package main

import (
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/spf13/cobra"
)

const (
	defaultAdminTokenFile = "/run/secrets/prism_admin_token"
)

type cliContext struct {
	output     string
	inputMode  string
	inputFile  string
	adminFile  string
	adminToken string
	apiURL     string
}

func newCLIContext() *cliContext {
	return &cliContext{}
}

func (c *cliContext) bindPersistentFlags(cmd *cobra.Command) {
	flags := cmd.PersistentFlags()
	flags.StringVar(&c.output, "output", "text", "Output format: text or json")
	flags.StringVar(&c.inputMode, "input", "", "Input mode: json")
	flags.StringVar(&c.inputFile, "input-file", "", "Read JSON command envelope from file")
	flags.StringVar(&c.adminFile, "admin-token-file", "", "Path to admin token file inside the container")
	flags.StringVar(&c.adminToken, "admin-token", "", "Raw admin token; prefer --admin-token-file")
	flags.StringVar(&c.apiURL, "api-url", "http://localhost:8091/api/v1", "Prism admin API base URL")
}

func readAll(path string) (string, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(b)), nil
}

func readStdin() ([]byte, error) {
	return io.ReadAll(os.Stdin)
}

func warnf(format string, args ...any) {
	fmt.Fprintf(os.Stderr, "WARNING: "+format+"\n", args...)
}
