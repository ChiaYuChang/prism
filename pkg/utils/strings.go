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
	reSpaces    = regexp.MustCompile(`\s+`)
)

// NormalizeString applies a series of cleaning operations:
// replaces NBSP, removes extra spaces, and strips invisible characters.
func NormalizeString(s string) string {
	s = strings.ReplaceAll(s, "\u00A0", " ")
	s = reSpaces.ReplaceAllString(s, " ")
	s = reInvisible.ReplaceAllString(s, "")
	return strings.TrimSpace(s)
}

// Join concatenates multiple strings and returns the result along with an array of cut indices.
// This is useful for mapping cleaned content back to its original segment.
func Join(text []string) (string, []int) {
	var builder strings.Builder
	cuts := make([]int, 0, len(text))
	for _, t := range text {
		builder.WriteString(t)
		cuts = append(cuts, builder.Len())
	}
	return builder.String(), cuts
}

// Split divides a string based on a provided set of cut indices.
func Split(text string, cuts []int) []string {
	if len(cuts) == 0 {
		return []string{text}
	}
	result := make([]string, 0, len(cuts))
	head := 0
	for _, tail := range cuts {
		if tail > head {
			result = append(result, text[head:tail])
		}
		head = tail
	}
	return result
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

// SecretMask hides sensitive parts of a string (e.g., passwords or tokens).
func SecretMask(s string) string {
	if len(s) <= 10 {
		return strings.Repeat("●", len(s))
	}
	return s[:5] + strings.Repeat("●", 5) + s[len(s)-5:]
}
