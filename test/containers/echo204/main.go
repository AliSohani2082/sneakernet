// Command echo204 answers every HTTP request with 204 No Content. The
// container tests use it as the "internet" behind a local Xray server, so
// the whole proxy path runs with networking disabled.
package main

import (
	"flag"
	"log"
	"net/http"
)

func main() {
	addr := flag.String("addr", "127.0.0.1:41080", "listen address")
	flag.Parse()
	log.Fatal(http.ListenAndServe(*addr, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	})))
}
