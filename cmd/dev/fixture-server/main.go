// Command fixture-server serves testdata/real over HTTP so worker
// binaries running with --fixture-base=http://localhost:<port> can replay
// the captured corpus end-to-end without touching real sites. Dev-only.
package main

import (
	"flag"
	"log"
	"net/http"
)

func main() {
	addr := flag.String("addr", ":9999", "listen address (host:port)")
	root := flag.String("root", "testdata/real", "fixture root directory")
	flag.Parse()

	log.Printf("fixture-server: serving %s on %s", *root, *addr)
	if err := http.ListenAndServe(*addr, http.FileServer(http.Dir(*root))); err != nil {
		log.Fatalf("fixture-server: %v", err)
	}
}
