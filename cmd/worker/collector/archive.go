package main

import (
	"context"
	"fmt"
	"log/slog"
	"net/url"
	"strings"

	"github.com/ChiaYuChang/prism/internal/appconfig"
	"github.com/ChiaYuChang/prism/internal/collector/archiver"
)

// openArchiver constructs an Archiver from an archive URI. For "s3://bucket/prefix"
// it builds an S3 client from s3cfg and injects it. For "file://..." (or a bare
// path) it delegates to archiver.ParseURI.
func openArchiver(ctx context.Context, uri string, s3cfg appconfig.S3Config, logger *slog.Logger) (archiver.Archiver, error) {
	if strings.HasPrefix(uri, "s3://") {
		u, err := url.Parse(uri)
		if err != nil {
			return nil, fmt.Errorf("parse archive URI %q: %w", uri, err)
		}
		bucket := u.Host
		if bucket == "" {
			return nil, fmt.Errorf("s3 URI %q missing bucket", uri)
		}
		prefix := strings.TrimPrefix(u.Path, "/")
		client, err := s3cfg.NewClient(ctx)
		if err != nil {
			return nil, fmt.Errorf("build s3 client: %w", err)
		}
		return archiver.NewS3Archiver(client, bucket, prefix, logger)
	}
	return archiver.ParseURI(uri, logger)
}
