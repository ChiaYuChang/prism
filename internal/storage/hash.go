package storage

import (
	"crypto/sha256"
	"io"
)

// Hash computes the canonical SHA-256 digest of data.
// It returns the raw bytes of the digest.
func Hash(data []byte) []byte {
	sum := sha256.Sum256(data)
	return sum[:]
}

// HashReader consumes r up to max+1 bytes. It computes the SHA-256 digest
// and returns it as raw bytes, along with the exact size read.
// If the reader yields more than max bytes, ErrObjectTooLarge is returned.
func HashReader(r io.Reader, max int64) (digest []byte, size int64, err error) {
	hasher := sha256.New()
	size, err = io.Copy(hasher, io.LimitReader(r, max+1))
	if err != nil {
		return nil, size, err
	}
	if size > max {
		return nil, size, ErrObjectTooLarge
	}
	return hasher.Sum(nil), size, nil
}
