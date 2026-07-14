package secrets

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
)

// Mapping identifies an RBW item and the relative path it populates.
type Mapping struct {
	Item string
	Path string
}

// DefaultMappings returns the local development secret mappings.
func DefaultMappings() []Mapping {
	return []Mapping{
		{Item: "prism_brave-search-api", Path: "brave-search-api"},
		{Item: "prism_google-cse-api", Path: "google/cse-id"},
		{Item: "prism_google-gcloud", Path: "google/gcloud"},
		{Item: "prism_google-gemini", Path: "google/gemini"},
		{Item: "prism_grafana", Path: "grafana"},
		{Item: "prism_nats-auth-token", Path: "nats-auth-token"},
		{Item: "prism_opencode", Path: "opencode"},
		{Item: "prism_pg-admin", Path: "pg-admin"},
		{Item: "prism_pg-prism", Path: "pg-prism"},
		{Item: "prism_seaweedfs", Path: "seaweedfs"},
		{Item: "prism_serpapi", Path: "serpapi"},
		{Item: "prism_valkey-admin", Path: "valkey-admin"},
		{Item: "prism_valkey-prism", Path: "valkey-app"},
	}
}

// Sync retrieves mappings and writes them below dir with owner-only
// permissions. Existing files outside mappings are preserved.
func Sync(ctx context.Context, store SecretStore, dir string, mappings []Mapping) error {
	if store == nil {
		return fmt.Errorf("%w: store", ErrParamMissing)
	}
	if dir == "" {
		return fmt.Errorf("%w: directory", ErrParamMissing)
	}
	if len(mappings) == 0 {
		return fmt.Errorf("%w: mappings", ErrParamMissing)
	}
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return fmt.Errorf("create secret directory: %w", err)
	}
	if err := os.Chmod(dir, 0o700); err != nil {
		return fmt.Errorf("chmod secret directory: %w", err)
	}

	values := make([][]byte, len(mappings))
	for i, mapping := range mappings {
		if mapping.Item == "" || mapping.Path == "" {
			return fmt.Errorf("mapping %d: %w", i, ErrParamMissing)
		}
		cleanPath := filepath.Clean(mapping.Path)
		if filepath.IsAbs(mapping.Path) || cleanPath == ".." || len(cleanPath) > 3 && cleanPath[:3] == ".."+string(filepath.Separator) {
			return fmt.Errorf("mapping %s: path escapes secret directory", mapping.Item)
		}
		value, err := store.Get(ctx, mapping.Item)
		if err != nil {
			return fmt.Errorf("get %s: %w", mapping.Item, err)
		}
		values[i] = value
	}

	for i, mapping := range mappings {
		path := filepath.Join(dir, mapping.Path)
		if err := writeSecret(path, values[i]); err != nil {
			return fmt.Errorf("write %s: %w", mapping.Item, err)
		}
	}
	return nil
}

func writeSecret(path string, value []byte) error {
	if len(value) == 0 {
		return ErrEmptySecret
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(filepath.Dir(path), ".secret-*")
	if err != nil {
		return err
	}
	tmpPath := tmp.Name()
	defer func() { _ = os.Remove(tmpPath) }()
	if err := tmp.Chmod(0o600); err != nil {
		_ = tmp.Close()
		return err
	}
	if _, err := tmp.Write(value); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	if err := os.Rename(tmpPath, path); err != nil {
		return err
	}
	return nil
}
