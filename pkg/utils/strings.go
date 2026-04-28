package utils

import (
	"bytes"
	"io"
	"regexp"
	"strings"

	"golang.org/x/text/encoding/simplifiedchinese"
	"golang.org/x/text/transform"
)

var (
	reInvisible = regexp.MustCompile(`[\x00-\x1F\x7F-\x9F　 ]`)
)

// NormalizeString applies a series of cleaning operations:
// 1. Replaces NBSP with a regular space.
// 2. Collapses all consecutive whitespace (including newlines, tabs, and ideographic spaces) into a single space.
// 3. Trims leading and trailing whitespace.
func NormalizeString(s string) string {
	if s == "" {
		return ""
	}
	// Replace NBSP with a regular space
	s = strings.ReplaceAll(s, "\u00A0", " ")
	// Remove invisible characters
	s = reInvisible.ReplaceAllString(s, " ")
	// Collapse multiple spaces into a single space
	s = strings.Join(strings.Fields(s), " ")
	return strings.TrimSpace(s)
}

// GbkToUtf8 converts GBK/GB18030 encoded strings to UTF-8.
func GbkToUtf8(s string) (string, error) {
	decoder := simplifiedchinese.GB18030.NewDecoder()
	reader := transform.NewReader(
		bytes.NewReader([]byte(s)), decoder)
	d, err := io.ReadAll(reader)
	if err != nil {
		return "", err
	}
	return string(d), nil
}
