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
	"strings"

	"cloud.google.com/go/storage"
	"github.com/go-sql-driver/mysql"
	"github.com/ttacon/chalk"
)

var (
	project = flag.String("project", "", "the GCP project")
	bucket  = flag.String("bucket", "", "the bucket name")

	dbHost   = flag.String("dbhost", "", "the database host")
	dbName   = flag.String("dbname", "", "the database name")
	dbUser   = flag.String("dbuser", "", "the database user")
	dbPass   = flag.String("dbpass", "", "the database password")
	dbPrefix = flag.String("dbprefix", "", "the WP database table prefix")

	guidPrefix = flag.String("guidprefix", "", "the start of each 'guid' in the attachments")
)

func init() {
	flag.Parse()
}

func main() {
	switch {
	case *project == "", *bucket == "",
		*dbHost == "", *dbName == "", *dbUser == "", *dbPass == "", *dbPrefix == "",
		*guidPrefix == "":
		fmt.Println(chalk.Red.Color("All command line arguments must be set."))
		flag.PrintDefaults()
		return
	}

	if !strings.HasSuffix(*guidPrefix, "/") {
		printErr(fmt.Sprintf("The given guidprefix argument %q does not have a trailing slash, which indicates "+
			"that it might not be what it should be.", *guidPrefix), errors.New("invalid command line arguments"))
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

	ctx := context.Background()
	client, err := storage.NewClient(ctx)
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

// An attachmentPost contains the fields retrieved for our purposes for each post representing an attachment.
type attachmentPost struct {
	ID       int64
	fileName string
}

// getAttachments retrieves all of the attachment posts from the database table specified.
func getAttachments(db *sql.DB) []attachmentPost {
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

	attachments := make([]attachmentPost, 0, attachmentsCount)
	i := 0
	rows, err := db.Query(fmt.Sprintf("SELECT ID, guid from `%s` WHERE post_type = 'attachment'", tableName()))
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

// checkStorageObjects checks to make sure that all attachments have a corresponding file in the bucket.
func checkStorageObjects(handle *storage.BucketHandle, atts []attachmentPost) error {
	return nil
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
