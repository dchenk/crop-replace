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
	bucket = flag.String("bucket", "", "the bucket name")

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

	postType = flag.String("posttype", "post", "the post_type to transform")
)

func init() {
	flag.Parse()
}

func main() {
	switch {
	case *bucket == "",
		*dbHost == "", *dbName == "", *dbUser == "", *dbPass == "", *dbPrefix == "",
		*guidPrefix == "", *bucketPrefix == "" && !*noBucketPrefix:
		fmt.Println(chalk.Red.Color("All command line arguments must be set."))
		fmt.Println("Currently got:")
		for k, v := range map[string]*string{
			"bucket":       bucket,
			"dbhost":       dbHost,
			"dbname":       dbName,
			"dbuser":       dbUser,
			"dbpass":       dbPass,
			"dbprefix":     dbPrefix,
			"guidprefix":   guidPrefix,
			"bucketprefix": bucketPrefix,
		} {
			fmt.Printf("\t%v %q\n", k, *v)
		}
		fmt.Printf("\t%v %v\n", "nobucketprefix", *noBucketPrefix)
		fmt.Println("Flags defined:")
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

	switch *postType {
	case "post", "page":
	default:
		printErr("The posttype argument must be either post or page", errInvalidCommand)
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

	fmt.Println("Finished listing crop variants in bucket.")

	err = replaceImageCrops(db, *postType, attachments)
	if err != nil {
		printErr("replacing images", err)
	}

}

var errInvalidCommand = errors.New("invalid command line arguments")

// An attachment contains the fields retrieved for our purposes for each post representing an attachment
// along with a list of all of its cropped variants contained in the storage bucket.
type attachment struct {
	ID       int64
	fileName string
	ext      string
	crops    []crop
}

type crop struct {
	str           string // str contains the dimensions in the form "600x600" or "600x340"
	width, height uint64
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

	rows, err := db.Query(fmt.Sprintf("SELECT ID, guid from `%s` WHERE post_type = 'attachment' ORDER BY ID", tableName()))
	if err != nil {
		printErr("getting attachment rows", err)
		return nil
	}
	defer rows.Close()
	for rows.Next() {
		var att attachment
		var guid string
		if err := rows.Scan(&att.ID, &guid); err != nil {
			printErr("scanning an attachment row", err)
			return nil
		}

		// Extract the extension, including the leading dot.
		att.ext = filepath.Ext(guid)
		if att.ext == "" {
			// If there is no extension, it's not likely that we're dealing with an image.
			fmt.Println(chalk.Cyan.Color(fmt.Sprintf("Skipping file without extension: %v", att.fileName)))
			continue
		}

		if !strings.HasPrefix(guid, *guidPrefix) {
			printErr(fmt.Sprintf("The row with ID %d has the guid %q but all attachments must have the same prefix.", att.ID, guid),
				errors.New("unexpected value for the 'guid' column"))
			return nil
		}

		// fileName will have guidPrefix removed but will have a leading slash.
		att.fileName = strings.TrimPrefix(guid, guidPrefixTrimmed)

		attachments = append(attachments, att)
	}
	if err := rows.Err(); err != nil {
		printErr("looping over query rows", err)
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

		if att.ext == "" {
			continue // Must be checked already, so this is just in case.
		}

		fileName := *bucketPrefix + att.fileName

		// Trim out the extension.
		query.Prefix = fileName[:len(fileName)-len(att.ext)]

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

			if dimensions := getCropVariant(strings.TrimPrefix(obj.Name, query.Prefix), att.ext); dimensions != nil {
				att.crops = append(att.crops, *dimensions)
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
// If the file name gives a crop variant, this function returns the dimensions of the crop, but otherwise it
// returns nil.
func getCropVariant(fileNameEnd, ext string) *crop {
	if fileNameEnd == "" || fileNameEnd[0] != '-' {
		return nil
	}
	trimmed := strings.TrimSuffix(fileNameEnd[1:], ext)
	split := strings.Split(trimmed, "x")
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
	return &crop{str: trimmed, width: width, height: height}
}

// replaceImageCrops loops through each post with post_type = postType and replaces occurrences of usage of each
// non-existent image crop with an existing variant of the image.
func replaceImageCrops(db *sql.DB, postType string, files []attachment) error {
	var rows *sql.Rows
	var update *sql.Stmt
	rollback := func(tx *sql.Tx) {
		if err := update.Close(); err != nil {
			printErr("closing prepared statement before rollback", err)
		}
		if rows != nil {
			if err := rows.Close(); err != nil {
				printErr("closing rows before rollback", err)
			}
		}
		if err := tx.Rollback(); err != nil {
			printErr("rolling back after failure", err)
		}
	}
	tx, err := db.Begin()
	if err != nil {
		return fmt.Errorf("could not begin transaction; %v", err)
	}
	update, err = tx.Prepare(fmt.Sprintf("UPDATE `%s` SET post_content = ? WHERE ID = ?", tableName()))
	if err != nil {
		rollback(tx)
		return fmt.Errorf("could not prepare update statement; %v", err)
	}
	rows, err = tx.Query(fmt.Sprintf("SELECT ID, post_content FROM `%s` WHERE post_type = ? ORDER BY ID", tableName()),
		postType)
	if err != nil {
		rollback(tx)
		return fmt.Errorf("could not query for rows; %v", err)
	}
	for rows.Next() {
		var ID int64
		var content string
		if err := rows.Scan(&ID, &content); err != nil {
			rollback(tx)
			return err
		}
		got := replaceCrops(content, files)
		if got != content {
			fmt.Println("Updating", ID)
			res, err := update.Exec(got, ID)
			if err != nil {
				rollback(tx)
				return fmt.Errorf("could not update row %d; %v", ID, err)
			}
			affected, err := res.RowsAffected()
			if err != nil {
				rollback(tx)
				return fmt.Errorf("could not check for rows affected; %v", err)
			}
			if affected != 1 {
				rollback(tx)
				return fmt.Errorf("after update results say %d rows affected", affected)
			}
		}
	}
	if err := rows.Err(); err != nil {
		rollback(tx)
		return err
	}
	if err := rows.Close(); err != nil {
		printErr("closing rows before commit", err)
	}
	return tx.Commit()
}

func replaceCrops(content string, files []attachment) string {
	for i := range files {
		file := &files[i]
		lenName := len(file.fileName)
		trimmed := file.fileName[:lenName-len(file.ext)]
		//contentTemp := content
		indexes := stringIndexes(content, trimmed)
		for _, indx := range indexes {
			_ = indx
		}
		//for indx := strings.Index(contentTemp, trimmed); indx != -1; {
		//	fmt.Println("indx", indx)
		//	// content = strings.Replace(content, "", "", -1)
		//	contentTemp = contentTemp[indx+lenName:]
		//}
	}
	return content
}

// stringIndexes returns the indexes of s at which there is substr.
func stringIndexes(s, substr string) (indexes []int) {
	offset := 0
	for {
		i := strings.Index(s, substr)
		if i == -1 {
			return
		}
		indexes = append(indexes, offset+i)
		move := i + len(substr)
		offset += move
		s = s[move:]
	}
	return
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
