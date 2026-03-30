package utils_test

import (
	"testing"

	"github.com/ChiaYuChang/prism/pkg/utils"
)

func TestArchiveGzipBase64(t *testing.T) {
	original := "Project Prism: Master Implementation Plan for Taiwan Political Analysis"

	// Test Compression
	compressed, err := utils.CompressGzipBase64(original)
	if err != nil {
		t.Fatalf("failed to compress: %v", err)
	}

	if compressed == original {
		t.Error("compressed string should be different from original")
	}

	// Test Decompression
	decompressed, err := utils.DecompressGzipBase64(compressed)
	if err != nil {
		t.Fatalf("failed to decompress: %v", err)
	}

	if decompressed != original {
		t.Errorf("decompressed string mismatch: got %q, want %q", decompressed, original)
	}
}

func TestDecompressInvalidBase64(t *testing.T) {
	_, err := utils.DecompressGzipBase64("invalid base64 content!!!")
	if err == nil {
		t.Error("expected error for invalid base64, got nil")
	}
}

func TestDecompressInvalidGzip(t *testing.T) {
	// Base64 encoded but not Gzip compressed
	invalidGzip := "SGVsbG8gV29ybGQ=" // "Hello World" in Base64
	_, err := utils.DecompressGzipBase64(invalidGzip)
	if err == nil {
		t.Error("expected error for invalid gzip, got nil")
	}
}
