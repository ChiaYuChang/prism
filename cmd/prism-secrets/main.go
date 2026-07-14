package main

import (
	"context"
	"flag"
	"fmt"
	"os"

	"github.com/ChiaYuChang/prism/internal/secrets"
)

func main() {
	dir := flag.String("dir", ".secrets", "directory for generated secret files")
	flag.Parse()

	if err := secrets.Sync(context.Background(), secrets.NewRBWStore(), *dir, secrets.DefaultMappings()); err != nil {
		fmt.Fprintf(os.Stderr, "sync secrets: %v\n", err)
		os.Exit(1)
	}
	fmt.Fprintf(os.Stderr, "synced %d secrets to %s\n", len(secrets.DefaultMappings()), *dir)
}
