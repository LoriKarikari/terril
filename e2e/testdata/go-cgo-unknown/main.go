package main

import (
	"fmt"
	"net/http"
	"os"

	"example.com/cgolib"
)

func main() {
	http.HandleFunc("/", func(w http.ResponseWriter, _ *http.Request) { fmt.Fprintf(w, "answer %d", cgolib.Answer()) })
	http.ListenAndServe(":"+os.Getenv("PORT"), nil)
}
