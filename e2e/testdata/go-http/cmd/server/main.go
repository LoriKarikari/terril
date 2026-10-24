package main

import (
	"fmt"
	"net/http"
	"os"
)

func main() {
	http.HandleFunc("/", func(w http.ResponseWriter, _ *http.Request) { fmt.Fprint(w, "hello from go") })
	http.ListenAndServe(":"+os.Getenv("PORT"), nil)
}
