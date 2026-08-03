package token

import (
	"bytes"
	"encoding/base64"
	"strings"
	"testing"

	"github.com/google/uuid"
)

func TestParseAllowsUnderscoresInSecret(t *testing.T) {
	secret := bytes.Repeat([]byte{0xff}, 32)
	raw := "padm_" + uuid.New().String() + "_" + base64.RawURLEncoding.EncodeToString(secret)
	if !strings.Contains(strings.SplitN(raw, "_", 3)[2], "_") {
		t.Fatal("test secret did not contain an underscore")
	}

	parsed, err := Parse(raw)
	if err != nil {
		t.Fatalf("Parse() error = %v", err)
	}
	if parsed.Type != TypeAdmin || !bytes.Equal(parsed.Secret, secret) {
		t.Fatalf("Parse() = type %q, secret length %d", parsed.Type, len(parsed.Secret))
	}
}
