package main

import (
	"context"
	"database/sql"
	"flag"
	"fmt"
	"os"

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
)

func init() {
	flag.Parse()
}

func main() {
	switch {
	case *project == "", *bucket == "", *dbHost == "", *dbName == "", *dbUser == "", *dbPass == "", *dbPrefix == "":
		fmt.Println(chalk.Red.Color("All command line arguments must be set."))
		flag.PrintDefaults()
		return
	}

	db := makeConn(*dbHost, *dbName, *dbUser, *dbPass)

	attachments := getAttachments(db)
	if len(attachments) == 0 {
		fmt.Println("There aren't any attachments to sync up.")
		return
	}

	ctx := context.Background()
	client, err := storage.NewClient(ctx)
	if err != nil {
		printExit("creating a storage client", err)
	}

	bucketHandle := client.Bucket(*bucket)
	_ = bucketHandle

}

func printExit(msg string, err error) {
	fmt.Println(chalk.Red.Color(fmt.Sprintf("ERROR %v: %v", msg, err)))
	os.Exit(1)
}

// An attachmentPost is contains the columns retrieved for our purposes for each post representing an attachment.
type attachmentPost struct {
	ID       int64
	postName string
	guid     string
}

func getAttachments(db *sql.DB) []attachmentPost {
	var attachmentsCount int64
	if err := db.QueryRow(fmt.Sprintf("SELECT COUNT(*) FROM `%s` WHERE post_type = 'attachment'", tableName())).
		Scan(&attachmentsCount); err != nil {
		printExit("counting attachment rows", err)
	}
	if attachmentsCount == 0 {
		return nil
	}

	attachments := make([]attachmentPost, 0, attachmentsCount)
	i := 0
	attachmentsRows, err := db.Query(
		fmt.Sprintf("SELECT ID, post_name, guid from `%s` WHERE post_type = 'attachment'", tableName()))
	if err != nil {
		printExit("getting attachment rows", err)
	}
	for attachmentsRows.Next() {
		attachments = attachments[:i+1]
		att := &attachments[i]
		if err := attachmentsRows.Scan(&att.ID, &att.postName, &att.guid); err != nil {
			attachmentsRows.Close()
			printExit("scanning an attachment row", err)
		}
		i++
	}
	if err := attachmentsRows.Close(); err != nil {
		printExit("closing query rows", err)
	}

	return attachments
}

func makeConn(host, dbName, user, pass string) *sql.DB {
	config := mysql.NewConfig()
	config.Addr = host
	config.DBName = dbName
	config.User = user
	config.Passwd = pass
	db, err := sql.Open("mysql", config.FormatDSN())
	if err != nil {
		printExit("connecting to database", err)
	}
	return db
}

// tableName returns the name of the "wp_posts" database table.
func tableName() string {
	return *dbPrefix + "posts"
}
