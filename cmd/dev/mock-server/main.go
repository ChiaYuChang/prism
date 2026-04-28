package main

import (
	"flag"
	"fmt"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"strings"
)

func main() {
	port := flag.Int("port", 8080, "Port to listen on")
	baseDir := flag.String("base", "tmp/mirror", "Base directory for mirrored data")
	flag.Parse()

	// Ensure the directory exists
	if _, err := os.Stat(*baseDir); os.IsNotExist(err) {
		log.Fatalf("Directory %s does not exist. Run cmd/downloader first.", *baseDir)
	}

	mux := http.NewServeMux()

	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// 1. Get host from request (remove port if present)
		host := r.Host
		if i := strings.Index(host, ":"); i != -1 {
			host = host[:i]
		}

		// 2. Build relative path within the host directory
		relPath := strings.TrimPrefix(r.URL.Path, "/")
		if relPath == "" {
			relPath = "index.html"
		}

		// 3. Rewrite query params to match downloader's filename logic
		if r.URL.RawQuery != "" {
			querySafe := strings.NewReplacer("?", "_", "&", "_", "=", "_", "/", "_").Replace(r.URL.RawQuery)
			relPath = relPath + "_" + querySafe
		}

		// 4. Construct full path: base/host/relPath
		fullPath := filepath.Join(*baseDir, host, relPath)

		log.Printf("[Mock] %s (Host: %s) -> %s", r.URL.String(), host, fullPath)

		// Check if file exists
		if _, err := os.Stat(fullPath); os.IsNotExist(err) {
			log.Printf("[Mock] NOT FOUND: %s", fullPath)
			http.Error(w, "Not Found", http.StatusNotFound)
			return
		}

		// Serve the file
		http.ServeFile(w, r, fullPath)
	})

	mux.Handle("/", handler)

	fmt.Printf(">> Mock Multi-Host Server started at :%d\n", *port)
	fmt.Printf(">> Serving multi-host data from: %s\n", *baseDir)
	fmt.Printf(">> Example usage: curl -H \"Host: www.dpp.org.tw\" http://localhost:%d/media/00\n", *port)

	if err := http.ListenAndServe(fmt.Sprintf(":%d", *port), mux); err != nil {
		log.Fatal(err)
	}
}
