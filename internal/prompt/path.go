package prompt

import (
	"strings"
)

// ObjectKey returns the hash-addressed key for a prompt body.
func ObjectKey(hash string) string {
	return strings.TrimPrefix(hash, "sha256:") + ".md"
}
