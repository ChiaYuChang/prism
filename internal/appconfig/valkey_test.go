package appconfig

import (
	"fmt"
	"log/slog"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
)

// TestValkeyConfig_NoSecretLeak guards both fmt verbs and slog.Any from
// emitting the raw password. Any new field that holds a secret must extend
// String() / LogValue() and add a check here.
func TestValkeyConfig_NoSecretLeak(t *testing.T) {
	const password = "pwd-abcdef-0123456789"
	cfg := ValkeyConfig{
		Host:     "localhost",
		Port:     6379,
		Username: "user",
		Password: password,
		DB:       0,
	}

	for _, verb := range []string{"%v", "%+v", "%s"} {
		out := fmt.Sprintf(verb, cfg)
		assert.NotContains(t, out, password, "verb %q leaked password", verb)
	}

	var buf strings.Builder
	h := slog.NewTextHandler(&buf, nil)
	slog.New(h).Info("valkey", slog.Any("config", cfg))
	logged := buf.String()
	assert.NotContains(t, logged, password, "slog.Any leaked password: %s", logged)
}

func TestValkeyConfig_Addr(t *testing.T) {
	cfg := ValkeyConfig{Host: "valkey", Port: 6379}
	assert.Equal(t, "valkey:6379", cfg.Addr())
}
