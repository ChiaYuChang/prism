package main

import (
	"flag"
	"net/http"
	"os"
	"time"
)

func main() {
	url := flag.String("url", "http://127.0.0.1:8080/readyz", "health endpoint URL")
	timeout := flag.Duration("timeout", 5*time.Second, "request timeout")
	flag.Parse()

	client := http.Client{Timeout: *timeout}
	response, err := client.Get(*url)
	if err != nil {
		os.Exit(1)
	}
	defer func() { _ = response.Body.Close() }()

	if response.StatusCode < http.StatusOK || response.StatusCode >= http.StatusMultipleChoices {
		os.Exit(1)
	}
}
