package appconfig

import (
	"fmt"
	"log/slog"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestS3Config_NoSecretLeak(t *testing.T) {
	const access = "access-abcdef-0123456789"
	const private = "private-abcdef-0123456789"
	cfg := S3Config{
		Endpoint:     "http://seaweedfs:8333",
		Region:       "us-east-1",
		AccessKey:    access,
		SecretKey:    private,
		UsePathStyle: true,
	}

	for _, verb := range []string{"%v", "%+v", "%s"} {
		out := fmt.Sprintf(verb, cfg)
		assert.NotContains(t, out, access, "verb %q leaked access key", verb)
		assert.NotContains(t, out, private, "verb %q leaked secret key", verb)
	}

	var buf strings.Builder
	h := slog.NewTextHandler(&buf, nil)
	slog.New(h).Info("s3", slog.Any("config", cfg))
	logged := buf.String()
	assert.NotContains(t, logged, access, "slog.Any leaked access key: %s", logged)
	assert.NotContains(t, logged, private, "slog.Any leaked secret key: %s", logged)
}
