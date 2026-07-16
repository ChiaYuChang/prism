package storage

import (
	"fmt"
	"net/url"
	"strings"
)

// URI identifies a storage backend and its namespace.
type URI struct {
	Scheme string
	Root   string
	Host   string
	Bucket string
	Prefix string
}

// ParseURI parses file:// and S3-compatible storage URIs. The sftp scheme is
// recognized for forward compatibility but has no backend implementation yet.
func ParseURI(raw string) (URI, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return URI{}, fmt.Errorf("storage URI is required")
	}
	if !strings.Contains(raw, "://") {
		return URI{Scheme: "file", Root: raw}, nil
	}
	u, err := url.Parse(raw)
	if err != nil {
		return URI{}, fmt.Errorf("parse storage URI %q: %w", raw, err)
	}
	switch strings.ToLower(u.Scheme) {
	case "file":
		root := u.Path
		if u.Host != "" {
			root = u.Host + root
		}
		if root == "" {
			return URI{}, fmt.Errorf("file storage URI %q has no root", raw)
		}
		return URI{Scheme: "file", Root: root}, nil
	case "s3", "sftp":
		if u.Host == "" {
			return URI{}, fmt.Errorf("%s storage URI %q has no host", u.Scheme, raw)
		}
		spec := URI{Scheme: strings.ToLower(u.Scheme), Prefix: strings.TrimPrefix(u.Path, "/")}
		if spec.Scheme == "s3" {
			spec.Bucket = u.Host
		} else {
			spec.Host = u.Host
		}
		return spec, nil
	default:
		return URI{}, fmt.Errorf("unknown storage URI scheme %q", u.Scheme)
	}
}
