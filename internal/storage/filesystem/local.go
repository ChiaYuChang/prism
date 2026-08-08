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

var _ storage.ImmutableStore = (*LocalStore)(nil)

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
		return fmt.Errorf("%w: create storage directory: %w", storage.ErrProvider, err)
	}
	tmp, err := os.CreateTemp(filepath.Dir(path), ".storage-*.tmp")
	if err != nil {
		return fmt.Errorf("%w: create temporary storage object: %w", storage.ErrProvider, err)
	}
	tmpName := tmp.Name()
	defer func() { _ = os.Remove(tmpName) }()
	if _, err := io.Copy(tmp, body); err != nil {
		_ = tmp.Close()
		return fmt.Errorf("%w: write storage object %s: %w", storage.ErrProvider, key, err)
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("%w: close temporary storage object: %w", storage.ErrProvider, err)
	}
	if err := os.Rename(tmpName, path); err != nil {
		return fmt.Errorf("%w: commit storage object %s: %w", storage.ErrProvider, key, err)
	}
	return nil
}

// PutIfAbsent writes an object only when key does not already exist. An
// existing object is accepted only when its complete content matches body.
func (s *LocalStore) PutIfAbsent(ctx context.Context, key string, body io.Reader, _ storage.PutOptions) (storage.PutIfAbsentResult, error) {
	cleanKey, err := storage.NormalizeKey(key, false)
	if err != nil {
		return storage.PutIfAbsentResult{}, err
	}
	if body == nil {
		return storage.PutIfAbsentResult{}, fmt.Errorf("storage object body is nil")
	}
	data, err := readBounded(body)
	if err != nil {
		return storage.PutIfAbsentResult{}, err
	}
	size := int64(len(data))
	digest := fmt.Sprintf("%x", storage.Hash(data))
	wantedMeta := storage.ObjectMetadata{Key: cleanKey, Size: size}
	wantedChecksum := storage.ObjectChecksum{Size: size, SHA256: digest}

	objectPath := filepath.Join(s.root, filepath.FromSlash(cleanKey))
	if err := os.MkdirAll(filepath.Dir(objectPath), 0o755); err != nil {
		return storage.PutIfAbsentResult{}, fmt.Errorf("%w: create storage directory: %w", storage.ErrProvider, err)
	}

	tmp, err := os.CreateTemp(filepath.Dir(objectPath), ".storage-*.tmp")
	if err != nil {
		return storage.PutIfAbsentResult{}, fmt.Errorf("%w: create temporary storage object: %w", storage.ErrProvider, err)
	}
	tmpName := tmp.Name()
	defer func() { _ = os.Remove(tmpName) }()
	if n, writeErr := tmp.Write(data); writeErr != nil {
		_ = tmp.Close()
		return storage.PutIfAbsentResult{}, fmt.Errorf("%w: write storage object %s: %w", storage.ErrProvider, cleanKey, writeErr)
	} else if n != len(data) {
		_ = tmp.Close()
		return storage.PutIfAbsentResult{}, fmt.Errorf("%w: write storage object %s: %w", storage.ErrProvider, cleanKey, io.ErrShortWrite)
	}
	if err := tmp.Close(); err != nil {
		return storage.PutIfAbsentResult{}, fmt.Errorf("%w: close temporary storage object: %w", storage.ErrProvider, err)
	}

	// Linking a temporary file is atomic and refuses to replace an existing
	// directory entry, unlike os.Rename on Unix.
	if err := os.Link(tmpName, objectPath); err == nil {
		return storage.PutIfAbsentResult{Created: true, Metadata: wantedMeta, Checksum: wantedChecksum}, nil
	} else if !os.IsExist(err) {
		return storage.PutIfAbsentResult{}, fmt.Errorf("%w: commit storage object %s: %w", storage.ErrProvider, cleanKey, err)
	}

	existingMeta, err := s.Stat(ctx, cleanKey)
	if err != nil {
		return storage.PutIfAbsentResult{}, err
	}
	existingChecksum, err := s.Checksum(ctx, cleanKey)
	if err != nil {
		return storage.PutIfAbsentResult{}, err
	}

	if existingChecksum.Size == wantedChecksum.Size && existingChecksum.SHA256 == wantedChecksum.SHA256 {
		return storage.PutIfAbsentResult{Metadata: existingMeta, Checksum: existingChecksum}, nil
	}
	return storage.PutIfAbsentResult{Metadata: existingMeta, Checksum: existingChecksum}, fmt.Errorf("%w: %s", storage.ErrContentMismatch, cleanKey)
}

// Stat returns cheap metadata for key without reading the complete object.
func (s *LocalStore) Stat(_ context.Context, key string) (storage.ObjectMetadata, error) {
	cleanKey, err := storage.NormalizeKey(key, false)
	if err != nil {
		return storage.ObjectMetadata{}, err
	}
	objectPath := filepath.Join(s.root, filepath.FromSlash(cleanKey))
	info, err := os.Stat(objectPath)
	if err != nil {
		if os.IsNotExist(err) {
			return storage.ObjectMetadata{}, fmt.Errorf("%w: %s", storage.ErrNotFound, cleanKey)
		}
		return storage.ObjectMetadata{}, fmt.Errorf("%w: stat storage object %s: %w", storage.ErrProvider, cleanKey, err)
	}
	if !info.Mode().IsRegular() {
		return storage.ObjectMetadata{}, fmt.Errorf("%w: storage object %s is not a regular file", storage.ErrProvider, cleanKey)
	}
	return storage.ObjectMetadata{Key: cleanKey, Size: info.Size()}, nil
}

// Checksum returns bounded, content-verified metadata for key.
func (s *LocalStore) Checksum(_ context.Context, key string) (storage.ObjectChecksum, error) {
	cleanKey, err := storage.NormalizeKey(key, false)
	if err != nil {
		return storage.ObjectChecksum{}, err
	}
	objectPath := filepath.Join(s.root, filepath.FromSlash(cleanKey))
	info, err := os.Stat(objectPath)
	if err != nil {
		if os.IsNotExist(err) {
			return storage.ObjectChecksum{}, fmt.Errorf("%w: %s", storage.ErrNotFound, cleanKey)
		}
		return storage.ObjectChecksum{}, fmt.Errorf("%w: stat storage object %s: %w", storage.ErrProvider, cleanKey, err)
	}
	if !info.Mode().IsRegular() {
		return storage.ObjectChecksum{}, fmt.Errorf("%w: storage object %s is not a regular file", storage.ErrProvider, cleanKey)
	}
	if info.Size() > storage.MaxObjectSize {
		return storage.ObjectChecksum{Size: info.Size()}, fmt.Errorf("%w: %s", storage.ErrObjectTooLarge, cleanKey)
	}
	f, err := os.Open(objectPath)
	if err != nil {
		if os.IsNotExist(err) {
			return storage.ObjectChecksum{}, fmt.Errorf("%w: %s", storage.ErrNotFound, cleanKey)
		}
		return storage.ObjectChecksum{}, fmt.Errorf("%w: open storage object %s: %w", storage.ErrProvider, cleanKey, err)
	}
	expectedSize := info.Size()
	digest, size, hashErr := storage.HashReader(f, storage.MaxObjectSize)
	closeErr := f.Close()
	if hashErr != nil {
		return storage.ObjectChecksum{}, fmt.Errorf("%w: hash storage object %s: %w", storage.ErrProvider, cleanKey, hashErr)
	}
	if size != expectedSize {
		return storage.ObjectChecksum{}, fmt.Errorf("%w: object %s changed during checksum", storage.ErrProvider, cleanKey)
	}
	if closeErr != nil {
		return storage.ObjectChecksum{}, fmt.Errorf("%w: close storage object %s: %w", storage.ErrProvider, cleanKey, closeErr)
	}
	return storage.ObjectChecksum{Size: size, SHA256: fmt.Sprintf("%x", digest)}, nil
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
		return nil, fmt.Errorf("%w: open storage object %s: %w", storage.ErrProvider, key, err)
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
		return nil, fmt.Errorf("%w: stat storage prefix %s: %w", storage.ErrProvider, prefix, err)
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
		return nil, fmt.Errorf("%w: list storage prefix %s: %w", storage.ErrProvider, prefix, err)
	}
	sort.Slice(objects, func(i, j int) bool { return objects[i].Key < objects[j].Key })
	return objects, nil
}

// Delete removes an object. It returns nil if the object is already missing.
func (s *LocalStore) Delete(_ context.Context, key string) error {
	path, err := s.path(key)
	if err != nil {
		return err
	}
	if err := os.Remove(path); err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return fmt.Errorf("%w: delete storage object %s: %w", storage.ErrProvider, key, err)
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

func readBounded(body io.Reader) ([]byte, error) {
	data, err := io.ReadAll(io.LimitReader(body, storage.MaxObjectSize+1))
	if err != nil {
		return nil, fmt.Errorf("%w: read storage object body: %w", storage.ErrProvider, err)
	}
	if int64(len(data)) > storage.MaxObjectSize {
		return nil, storage.ErrObjectTooLarge
	}
	return data, nil
}
