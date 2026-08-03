package storage_test

import (
	"testing"

	"github.com/ChiaYuChang/prism/internal/storage"
	"github.com/stretchr/testify/require"
)

func TestParseURI(t *testing.T) {
	tests := []struct {
		name string
		raw  string
		want storage.URI
	}{
		{name: "bare file path", raw: "runtime/prompts", want: storage.URI{Scheme: "file", Root: "runtime/prompts"}},
		{name: "file", raw: "file:///runtime/prompts", want: storage.URI{Scheme: "file", Root: "/runtime/prompts"}},
		{name: "s3", raw: "s3://bucket/prefix", want: storage.URI{Scheme: "s3", Bucket: "bucket", Prefix: "prefix"}},
		{name: "future sftp", raw: "sftp://host/path", want: storage.URI{Scheme: "sftp", Host: "host", Prefix: "path"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := storage.ParseURI(tt.raw)
			require.NoError(t, err)
			require.Equal(t, tt.want, got)
		})
	}
}

func TestParseURIRejectsUnknownScheme(t *testing.T) {
	_, err := storage.ParseURI("gcs://bucket/path")
	require.Error(t, err)
}
