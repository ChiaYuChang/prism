package utils

import (
	"crypto/md5"
	"crypto/sha256"
	"encoding/hex"
	"hash"
	"sync"
)

var (
	// DefaultHasher uses MD5 and Hex encoding.
	// Suitable for generating 32-character unique fingerprints for news articles.
	DefaultHasher = struct {
		sync.RWMutex
		Hasher
	}{
		Hasher: Hasher{
			algo:    md5.New,
			encoder: hex.EncodeToString,
		},
	}

	// SecureHasher uses SHA256 for tasks requiring higher security standards.
	SecureHasher = struct {
		sync.RWMutex
		Hasher
	}{
		Hasher: Hasher{
			algo:    sha256.New,
			encoder: hex.EncodeToString,
		},
	}
)

// Hasher defines a strategy for generating hashes.
// It uses a factory function (algo) to ensure thread-safety during concurrent operations.
type Hasher struct {
	algo    func() hash.Hash
	encoder func([]byte) string
}

// Sum calculates the hash value of multiple input strings.
// It calls Reset() after computation to clear internal buffers for memory safety.
func (h *Hasher) Sum(input ...string) string {
	hasher := h.algo()
	// Ensure internal buffers are cleared after use for security and potential object reuse.
	defer hasher.Reset()

	for _, s := range input {
		hasher.Write([]byte(s))
	}
	return h.encoder(hasher.Sum(nil))
}

// Hash is a convenience wrapper for DefaultHasher.Sum.
// It includes a Read-Lock to ensure configuration safety during concurrent access.
func Hash(input ...string) string {
	DefaultHasher.RLock()
	defer DefaultHasher.RUnlock()
	return DefaultHasher.Sum(input...)
}

// Ptr returns a pointer to the provided value v.
// Useful for handling optional database fields or API parameters that require pointers.
func Ptr[T any](v T) *T {
	return &v
}

// TruncateString truncate a string to the given length
func TruncateString(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen] + "..."
}
