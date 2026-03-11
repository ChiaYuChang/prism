package utils

import (
	"bytes"
	"compress/gzip"
	"encoding/base64"
	"fmt"
	"io"
	"log/slog"
)

// CompressGzipBase64 takes a raw string, compresses it with Gzip,
// and returns the Base64-encoded representation.
// This is used for archival storage (e.g., S3 ArchiveRecord).
func CompressGzipBase64(data string) (string, error) {
	var buf bytes.Buffer
	gz := gzip.NewWriter(&buf)

	if _, err := gz.Write([]byte(data)); err != nil {
		return "", fmt.Errorf("gzip write error: %w", err)
	}

	if err := gz.Close(); err != nil {
		return "", fmt.Errorf("gzip close error: %w", err)
	}

	return base64.StdEncoding.EncodeToString(buf.Bytes()), nil
}

// DecompressGzipBase64 reverses the process:
// Base64-decodes the string and then decompresses it using Gzip.
func DecompressGzipBase64(encoded string) (string, error) {
	// 1. Base64 Decode
	decoded, err := base64.StdEncoding.DecodeString(encoded)
	if err != nil {
		return "", fmt.Errorf("base64 decode error: %w", err)
	}

	// 2. Gzip Decompress
	gz, err := gzip.NewReader(bytes.NewReader(decoded))
	if err != nil {
		return "", fmt.Errorf("gzip reader error: %w", err)
	}
	defer func() {
		if err := gz.Close(); err != nil {
			slog.Error("gzip close error", "error", err)
		}
	}()

	result, err := io.ReadAll(gz)
	if err != nil {
		return "", fmt.Errorf("gzip readall error: %w", err)
	}

	return string(result), nil
}
