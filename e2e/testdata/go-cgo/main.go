package main

// int add(int a, int b) { return a + b; }
import "C"

import (
	"fmt"
	"net/http"
	"os"
)

func main() {
	sum := C.add(40, 2)
	http.HandleFunc("/", func(w http.ResponseWriter, _ *http.Request) { fmt.Fprintf(w, "C says %d", sum) })
	http.ListenAndServe(":"+os.Getenv("PORT"), nil)
}
