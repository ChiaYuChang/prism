package archiver

import (
	"fmt"
	"log/slog"
	"net/url"
	"strings"
)

// ParseURI constructs an Archiver from a URI string.
//
// Supported schemes:
//
//	file:///absolute/path       — LocalArchiver rooted at /absolute/path
//	file://relative/path        — LocalArchiver rooted at relative/path
//	s3://bucket/optional/prefix — S3Archiver stub (client must be configured separately)
//
// The logger parameter is required for all backends.
//
// Note: for s3:// the returned Archiver is a stub — Save works but Load/Scan/Remove
// return errors. A real S3 client must be injected via NewS3Archiver directly.
func ParseURI(uri string, logger *slog.Logger) (Archiver, error) {
	if logger == nil {
		return nil, fmt.Errorf("%w: logger", ErrParamMissing)
	}

	// Allow bare paths without a scheme (treated as file://).
	if !strings.Contains(uri, "://") {
		return NewLocalArchiver(uri, logger)
	}

	u, err := url.Parse(uri)
	if err != nil {
		return nil, fmt.Errorf("parse archive URI %q: %w", uri, err)
	}

	switch u.Scheme {
	case "file":
		// file:///abs/path  → Path = "/abs/path"
		// file://rel/path   → Host = "rel", Path = "/path"  (non-standard but practical)
		path := u.Path
		if u.Host != "" {
			path = u.Host + path
		}
		if path == "" {
			return nil, fmt.Errorf("file URI %q has no path", uri)
		}
		return NewLocalArchiver(path, logger)

	case "s3":
		bucket := u.Host
		if bucket == "" {
			return nil, fmt.Errorf("s3 URI %q missing bucket", uri)
		}
		prefix := strings.TrimPrefix(u.Path, "/")
		// S3Archiver requires a real client; return nil with a descriptive error
		// so callers know they must use NewS3Archiver directly with a configured client.
		_ = prefix
		return nil, fmt.Errorf("s3 archiver requires an injected S3 client: use NewS3Archiver(client, %q, %q, logger) directly", bucket, prefix)

	default:
		return nil, fmt.Errorf("%w: %q (supported: file://, s3://)", ErrUnknownScheme, u.Scheme)
	}
}
