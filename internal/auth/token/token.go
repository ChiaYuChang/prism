package token

import (
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"encoding/hex"
	"fmt"
	"strings"

	"github.com/google/uuid"
)

type Type string

const (
	TypeRoot   Type = "root"
	TypeAdmin  Type = "admin"
	TypeUser   Type = "user"
	TypeWorker Type = "worker"
)

var typePrefixes = map[Type]string{
	TypeAdmin:  "padm",
	TypeUser:   "pusr",
	TypeWorker: "pwrk",
}

var prefixTypes = map[string]Type{
	"padm": TypeAdmin,
	"pusr": TypeUser,
	"pwrk": TypeWorker,
}

type Parsed struct {
	Type   Type
	ID     uuid.UUID
	Secret []byte
}

type Hasher interface {
	Algorithm() string
	Hash(secret []byte) string
	Verify(secret []byte, encoded string) bool
}

type SHA256Hasher struct{}

func (SHA256Hasher) Algorithm() string { return "sha256" }

func (h SHA256Hasher) Hash(secret []byte) string {
	sum := sha256.Sum256(secret)
	return hex.EncodeToString(sum[:])
}

func (h SHA256Hasher) Verify(secret []byte, encoded string) bool {
	got := h.Hash(secret)
	return subtle.ConstantTimeCompare([]byte(got), []byte(encoded)) == 1
}

func NewHasher(algorithm string) (Hasher, error) {
	switch strings.TrimSpace(strings.ToLower(algorithm)) {
	case "", "sha256":
		return SHA256Hasher{}, nil
	default:
		return nil, fmt.Errorf("unsupported token hash algorithm %q", algorithm)
	}
}

func Generate(tokenType Type, id uuid.UUID) (string, []byte, error) {
	prefix, ok := typePrefixes[tokenType]
	if !ok {
		return "", nil, fmt.Errorf("unsupported token type %q", tokenType)
	}
	secret := make([]byte, 32)
	if _, err := rand.Read(secret); err != nil {
		return "", nil, fmt.Errorf("generate token secret: %w", err)
	}
	encoded := base64.RawURLEncoding.EncodeToString(secret)
	return prefix + "_" + id.String() + "_" + encoded, secret, nil
}

func Parse(raw string) (Parsed, error) {
	parts := strings.Split(strings.TrimSpace(raw), "_")
	if len(parts) != 3 {
		return Parsed{}, fmt.Errorf("invalid token format")
	}
	tokenType, ok := prefixTypes[parts[0]]
	if !ok {
		return Parsed{}, fmt.Errorf("invalid token prefix")
	}
	id, err := uuid.Parse(parts[1])
	if err != nil {
		return Parsed{}, fmt.Errorf("invalid token id")
	}
	secret, err := base64.RawURLEncoding.DecodeString(parts[2])
	if err != nil || len(secret) != 32 {
		return Parsed{}, fmt.Errorf("invalid token secret")
	}
	return Parsed{Type: tokenType, ID: id, Secret: secret}, nil
}

func Prefix(tokenType Type) (string, bool) {
	prefix, ok := typePrefixes[tokenType]
	return prefix, ok
}
