// Package filesystem implements storage.Store using a local filesystem root.
package filesystem

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/ChiaYuChang/prism/internal/storage"
)

// LocalStore stores objects below a local directory. A mounted filesystem such
// as EFS can be used by supplying its mount point as root.
type LocalStore struct {
	root string
}

var _ storage.Store = (*LocalStore)(nil)

// NewLocalStore creates a filesystem-backed store rooted at root.
func NewLocalStore(root string) (*LocalStore, error) {
	if strings.TrimSpace(root) == "" {
		return nil, fmt.Errorf("storage root is required")
	}
	root = filepath.Clean(root)
	if err := os.MkdirAll(root, 0o755); err != nil {
		return nil, fmt.Errorf("create storage root %s: %w", root, err)
	}
	return &LocalStore{root: root}, nil
}

// Put writes an object atomically below the store root.
func (s *LocalStore) Put(_ context.Context, key string, body io.Reader, _ storage.PutOptions) error {
	path, err := s.path(key)
	if err != nil {
		return err
	}
	if body == nil {
		return fmt.Errorf("storage object body is nil")
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return fmt.Errorf("create storage directory: %w", err)
	}
	tmp, err := os.CreateTemp(filepath.Dir(path), ".storage-*.tmp")
	if err != nil {
		return fmt.Errorf("create temporary storage object: %w", err)
	}
	tmpName := tmp.Name()
	defer func() { _ = os.Remove(tmpName) }()
	if _, err := io.Copy(tmp, body); err != nil {
		_ = tmp.Close()
		return fmt.Errorf("write storage object %s: %w", key, err)
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("close temporary storage object: %w", err)
	}
	if err := os.Rename(tmpName, path); err != nil {
		return fmt.Errorf("commit storage object %s: %w", key, err)
	}
	return nil
}

// Get opens an object for reading.
func (s *LocalStore) Get(_ context.Context, key string) (io.ReadCloser, error) {
	path, err := s.path(key)
	if err != nil {
		return nil, err
	}
	f, err := os.Open(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, fmt.Errorf("%w: %s", storage.ErrNotFound, key)
		}
		return nil, fmt.Errorf("open storage object %s: %w", key, err)
	}
	return f, nil
}

// List returns objects below prefix in lexical key order.
func (s *LocalStore) List(_ context.Context, prefix string) ([]storage.Object, error) {
	cleanPrefix, err := storage.NormalizeKey(prefix, true)
	if err != nil {
		return nil, err
	}
	root := s.root
	if cleanPrefix != "" {
		root, err = s.path(cleanPrefix)
		if err != nil {
			return nil, err
		}
	}
	if _, err := os.Stat(root); err != nil {
		if os.IsNotExist(err) {
			return []storage.Object{}, nil
		}
		return nil, fmt.Errorf("stat storage prefix %s: %w", prefix, err)
	}
	objects := make([]storage.Object, 0)
	err = filepath.WalkDir(root, func(path string, entry os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if entry.IsDir() {
			return nil
		}
		info, err := entry.Info()
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(s.root, path)
		if err != nil {
			return err
		}
		objects = append(objects, storage.Object{Key: filepath.ToSlash(rel), Size: info.Size()})
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("list storage prefix %s: %w", prefix, err)
	}
	sort.Slice(objects, func(i, j int) bool { return objects[i].Key < objects[j].Key })
	return objects, nil
}

// Delete removes an object.
func (s *LocalStore) Delete(_ context.Context, key string) error {
	path, err := s.path(key)
	if err != nil {
		return err
	}
	if err := os.Remove(path); err != nil {
		if os.IsNotExist(err) {
			return fmt.Errorf("%w: %s", storage.ErrNotFound, key)
		}
		return fmt.Errorf("delete storage object %s: %w", key, err)
	}
	return nil
}

func (s *LocalStore) path(key string) (string, error) {
	clean, err := storage.NormalizeKey(key, false)
	if err != nil {
		return "", err
	}
	return filepath.Join(s.root, filepath.FromSlash(clean)), nil
}
