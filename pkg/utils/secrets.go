package utils

import (
	"os"
	"strings"
)

// GetSecret retrieves a sensitive value from environment variables, prioritizing
// the "Secret-via-File" pattern (e.g., KEY_FILE) common in Docker and Kubernetes.
//
// Pattern Logic:
//  1. Checks for an environment variable named [key] + "_FILE" (e.g., DB_PASSWORD_FILE).
//  2. If found, reads the file content at that path and returns it (whitespace trimmed).
//  3. If the _FILE variable is empty or the file cannot be read, falls back to the
//     direct environment variable [key] (e.g., DB_PASSWORD).
func GetSecret(key string) string {
	// 1. Try to read from the file path variable
	fileKey := key + "_FILE"
	if filePath := os.Getenv(fileKey); filePath != "" {
		content, err := os.ReadFile(filePath)
		if err == nil {
			return strings.TrimSpace(string(content))
		}
	}

	// 2. Fallback to the plain environment variable
	return os.Getenv(key)
}
