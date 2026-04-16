package utils

import (
	"bytes"
	"compress/gzip"
	"encoding/base64"
	"encoding/json"
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

// CompressedBlob is a self-describing container for compressed content.
// The compression_method field tells the reader how to decompress,
// making it safe to change algorithms in the future without breaking existing archives.
type CompressedBlob struct {
	CompressionMethod string `json:"compression_method"` // e.g. "gzip"
	Encoding          string `json:"encoding"`           // e.g. "base64"
	OriginalSize      int    `json:"original_size"`      // bytes before compression
	Content           string `json:"content"`            // compressed + encoded payload
}

// CompressBlob compresses data with gzip and base64-encodes it into a CompressedBlob.
func CompressBlob(data string) (*CompressedBlob, error) {
	compressed, err := CompressGzipBase64(data)
	if err != nil {
		return nil, err
	}
	return &CompressedBlob{
		CompressionMethod: "gzip",
		Encoding:          "base64",
		OriginalSize:      len(data),
		Content:           compressed,
	}, nil
}

// Decompress reverses the compression based on compression_method.
func (b *CompressedBlob) Decompress() (string, error) {
	switch b.CompressionMethod {
	case "gzip":
		return DecompressGzipBase64(b.Content)
	default:
		return "", fmt.Errorf("unsupported compression method: %s", b.CompressionMethod)
	}
}

// CompressJSONBlob compresses a JSON-marshalable value into a CompressedBlob.
func CompressJSONBlob(js json.Marshaler) (*CompressedBlob, error) {
	data, err := js.MarshalJSON()
	if err != nil {
		return nil, fmt.Errorf("json marshal error: %w", err)
	}
	return CompressBlob(string(data))
}
