// This program loops over each post and replaces occurrences of cropped media attachment URLs that do not
// exist with URLs of (similar) crops that exist in the GCS bucket being used.
//
// It is assumed that the post_content column for the transformed posts is simply text (or HTML) and not
// a data structure encoded as JSON or serialized by PHP.
//
// Before you run this tool, you must first make sure that the "guid" column for all "attachment" posts
// begins the same way--with a site address.
package main

import (
	"context"
	"database/sql"
	"errors"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"cloud.google.com/go/storage"
	"github.com/go-sql-driver/mysql"
	"github.com/ttacon/chalk"
	"google.golang.org/api/iterator"
	"google.golang.org/api/option"
)

var (
	project = flag.String("project", "", "the GCP project")
	bucket  = flag.String("bucket", "", "the bucket name")

	dbHost   = flag.String("dbhost", "", "the database host")
	dbName   = flag.String("dbname", "", "the database name")
	dbUser   = flag.String("dbuser", "", "the database user")
	dbPass   = flag.String("dbpass", "", "the database password")
	dbPrefix = flag.String("dbprefix", "", "the WP database table prefix")

	guidPrefix = flag.String("guidprefix", "",
		"the start of each 'guid' in the attachments, with a trailing slash")
	bucketPrefix = flag.String("bucketprefix", "",
		"the prefix that all objects in the bucket have, without a trailing slash")
	noBucketPrefix = flag.Bool("nobucketprefix", false, "if true, then no bucket prefix is expected")
)

func init() {
	flag.Parse()
}

func main() {
	switch {
	case *project == "", *bucket == "",
		*dbHost == "", *dbName == "", *dbUser == "", *dbPass == "", *dbPrefix == "",
		*guidPrefix == "", *bucketPrefix == "" && !*noBucketPrefix:
		fmt.Println(chalk.Red.Color("All command line arguments must be set."))
		flag.PrintDefaults()
		return
	}

	if !strings.HasSuffix(*guidPrefix, "/") {
		printErr(fmt.Sprintf("The given guidprefix argument %q does not have a trailing slash, which indicates "+
			"that it might not be what it should be", *guidPrefix), errInvalidCommand)
		return
	}

	if strings.HasSuffix(*bucketPrefix, "/") {
		printErr(fmt.Sprintf("The given bucketprefix argument %q has a trailing slash but it must not", *bucketPrefix),
			errInvalidCommand)
		return
	}

	db := makeConn(*dbHost, *dbName, *dbUser, *dbPass)
	defer db.Close()

	attachments := getAttachments(db)
	if len(attachments) == 0 {
		fmt.Println("There aren't any attachments to sync up.")
		return
	}
	fmt.Println("Retrieved", len(attachments), "attachment posts.")

	client, err := storage.NewClient(context.Background(),
		option.WithScopes(storage.ScopeReadOnly),
		option.WithoutAuthentication(), // All desired objects must be public.
	)
	if err != nil {
		printErr("creating a storage client", err)
		return
	}

	bucketHandle := client.Bucket(*bucket)
	if err := checkStorageObjects(bucketHandle, attachments); err != nil {
		printErr("could not check for storage objects", err)
		return
	}

}

var errInvalidCommand = errors.New("invalid command line arguments")

// An attachment contains the fields retrieved for our purposes for each post representing an attachment
// along with a list of all of its cropped variants contained in the storage bucket.
type attachment struct {
	ID       int64
	fileName string

	// crops has strings such as "600x600" and "600x340"
	crops []string
}

// getAttachments retrieves all of the attachment posts from the database table specified.
func getAttachments(db *sql.DB) []attachment {
	var attachmentsCount int64
	if err := db.QueryRow(fmt.Sprintf("SELECT COUNT(*) FROM `%s` WHERE post_type = 'attachment'", tableName())).
		Scan(&attachmentsCount); err != nil {
		printErr("counting attachment rows", err)
		return nil
	}
	if attachmentsCount == 0 {
		return nil
	}

	// guidPrefixTrimmed is the guid prefix without the trailing slash.
	guidPrefixTrimmed := (*guidPrefix)[:len(*guidPrefix)-1]

	attachments := make([]attachment, 0, attachmentsCount)
	i := 0
	rows, err := db.Query(fmt.Sprintf("SELECT ID, guid from `%s` WHERE post_type = 'attachment' ORDER BY ID", tableName()))
	if err != nil {
		printErr("getting attachment rows", err)
		return nil
	}
	for rows.Next() {
		attachments = attachments[:i+1]
		att := &attachments[i]
		var guid string
		if err := rows.Scan(&att.ID, &guid); err != nil {
			printErr("scanning an attachment row", err)
			return nil
		}
		if !strings.HasPrefix(guid, *guidPrefix) {
			printErr(fmt.Sprintf("The row with ID %d has the guid %q but all attachments must have the same prefix.", att.ID, guid),
				errors.New("unexpected value for the 'guid' column"))
			return nil
		}
		// fileName will have guidPrefix removed but with a leading slash.
		att.fileName = strings.TrimPrefix(guid, guidPrefixTrimmed)
		i++
	}
	if err := rows.Close(); err != nil {
		printErr("closing query rows", err)
	}

	return attachments
}

// checkStorageObjects checks to make sure that all attachments have a corresponding file in the bucket and
// populates the crops field of each attachment element.
func checkStorageObjects(handle *storage.BucketHandle, atts []attachment) error {
	var (
		err   error
		obj   *storage.ObjectAttrs
		query storage.Query
	)

	for i := range atts {
		att := &atts[i]

		fileName := *bucketPrefix + att.fileName

		// Extract the extension to query without it.
		ext := filepath.Ext(fileName)
		if ext == "" {
			// If there is no extension, it's not likely that we're dealing with an image.
			fmt.Println(chalk.Cyan.Color(fmt.Sprintf("Skipping file without extension: %v", att.fileName)))
			continue
		}

		query.Prefix = fileName[:len(att.fileName)-len(ext)-1]

		var exists bool

		it := handle.Objects(context.Background(), &query)
		for {
			obj, err = it.Next()
			if err == iterator.Done {
				break
			}
			if err != nil {
				return err
			}

			if fileName == obj.Name {
				exists = true
				continue
			}

			if dimensions := getCropVariant(strings.TrimPrefix(obj.Name, query.Prefix), ext); dimensions != nil {
				//att.crops = append(att.crops, obj.Name)
			}
		}

		if !exists {
			printErr(fmt.Sprintf("there is no file named %v", fileName), errMissingFile)
		}
	}
	return nil
}

var errMissingFile = errors.New("missing file for an attachment")

// getCropVariant says whether the object with the name ending in fileNameEnd is a variant crop of an object
// whose name without .ext has been trimmed out of fileNameEnd.
// If the file name gives a crop variant, this function returns the dimensions in the slice of length 2, but
// otherwise it returns nil.
func getCropVariant(fileNameEnd, ext string) []uint64 {
	if fileNameEnd == "" || fileNameEnd[0] != '-' {
		return nil
	}
	split := strings.Split(strings.TrimSuffix(fileNameEnd[1:], "."+ext), "x")
	if len(split) != 2 {
		return nil
	}
	width, err := strconv.ParseUint(split[0], 10, 64)
	if err != nil {
		return nil
	}
	height, err := strconv.ParseUint(split[1], 10, 64)
	if err != nil {
		return nil
	}
	return []uint64{width, height}
}

// printErr prints the message msg with the non-nil error.
func printErr(msg string, err error) {
	fmt.Println(chalk.Red.Color(fmt.Sprintf("ERROR %v: %v", msg, err)))
}

// makeConn creates a sql.DB object to use with connections to the database.
// The program will terminate if a connection cannot be established.
func makeConn(host, dbName, user, pass string) *sql.DB {
	config := mysql.NewConfig()
	config.Net = "tcp"
	config.Addr = host
	config.DBName = dbName
	config.User = user
	config.Passwd = pass
	db, err := sql.Open("mysql", config.FormatDSN())
	if err != nil {
		printErr("connecting to database", err)
		os.Exit(1)
	}
	return db
}

// tableName returns the name of the "wp_posts" database table.
func tableName() string {
	return *dbPrefix + "posts"
}
