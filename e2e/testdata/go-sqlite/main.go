package main

import (
	"database/sql"
	"fmt"
	"net/http"
	"os"

	_ "github.com/mattn/go-sqlite3"
)

func main() {
	db, err := sql.Open("sqlite3", ":memory:")
	if err != nil {
		panic(err)
	}
	var version string
	if err := db.QueryRow("select sqlite_version()").Scan(&version); err != nil {
		panic(err)
	}
	http.HandleFunc("/", func(w http.ResponseWriter, _ *http.Request) { fmt.Fprint(w, "go with sqlite "+version) })
	http.ListenAndServe(":"+os.Getenv("PORT"), nil)
}
