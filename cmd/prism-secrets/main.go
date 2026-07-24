package main

import (
	"context"
	"flag"
	"fmt"
	"os"

	"github.com/ChiaYuChang/prism/internal/secrets"
)

func main() {
	prefix := os.Getenv("PREFIX")
	if prefix == "" {
		prefix = "prism_"
	}
	dir := flag.String("dir", ".secrets", "directory for generated secret files")
	prefixFlag := flag.String("prefix", prefix, "prefix for RBW item names")
	flag.Parse()

	mappings := secrets.WithPrefix(secrets.DefaultMappings(), *prefixFlag)
	if os.Getenv("PRISM_SECRETS_INCLUDE_ROOTCTL") == "1" {
		mappings = secrets.WithRootctl(mappings)
	}
	if err := secrets.Sync(context.Background(), secrets.NewRBWStore(), *dir, mappings); err != nil {
		fmt.Fprintf(os.Stderr, "sync secrets: %v\n", err)
		os.Exit(1)
	}
	fmt.Fprintf(os.Stderr, "synced %d secrets to %s\n", len(mappings), *dir)
}
