// Package filesystem implements storage.Store using a local filesystem root.
package filesystem

import (
	"context"
	"crypto/sha256"
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
	wanted := objectMetadata(cleanKey, data)
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
		return storage.PutIfAbsentResult{Created: true, Metadata: wanted}, nil
	} else if !os.IsExist(err) {
		return storage.PutIfAbsentResult{}, fmt.Errorf("%w: commit storage object %s: %w", storage.ErrProvider, cleanKey, err)
	}

	existing, err := s.Stat(ctx, cleanKey)
	if err != nil {
		return storage.PutIfAbsentResult{}, err
	}
	if existing.Size == wanted.Size && existing.SHA256 == wanted.SHA256 {
		return storage.PutIfAbsentResult{Metadata: existing}, nil
	}
	return storage.PutIfAbsentResult{Metadata: existing}, fmt.Errorf("%w: %s", storage.ErrContentMismatch, cleanKey)
}

// Stat returns bounded, content-verified metadata for key.
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
	if info.Size() > storage.MaxObjectSize {
		return storage.ObjectMetadata{Key: cleanKey, Size: info.Size()}, fmt.Errorf("%w: %s", storage.ErrObjectTooLarge, cleanKey)
	}
	f, err := os.Open(objectPath)
	if err != nil {
		if os.IsNotExist(err) {
			return storage.ObjectMetadata{}, fmt.Errorf("%w: %s", storage.ErrNotFound, cleanKey)
		}
		return storage.ObjectMetadata{}, fmt.Errorf("%w: open storage object %s: %w", storage.ErrProvider, cleanKey, err)
	}
	digest, size, hashErr := hashBounded(f)
	closeErr := f.Close()
	if hashErr != nil {
		return storage.ObjectMetadata{}, fmt.Errorf("%w: hash storage object %s: %w", storage.ErrProvider, cleanKey, hashErr)
	}
	if closeErr != nil {
		return storage.ObjectMetadata{}, fmt.Errorf("%w: close storage object %s: %w", storage.ErrProvider, cleanKey, closeErr)
	}
	if size > storage.MaxObjectSize {
		return storage.ObjectMetadata{Key: cleanKey, Size: size}, fmt.Errorf("%w: %s", storage.ErrObjectTooLarge, cleanKey)
	}
	return storage.ObjectMetadata{Key: cleanKey, Size: size, SHA256: digest}, nil
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

func readBounded(body io.Reader) ([]byte, error) {
	data, err := io.ReadAll(io.LimitReader(body, storage.MaxObjectSize+1))
	if err != nil {
		return nil, fmt.Errorf("read storage object body: %w", err)
	}
	if int64(len(data)) > storage.MaxObjectSize {
		return nil, storage.ErrObjectTooLarge
	}
	return data, nil
}

func hashBounded(body io.Reader) (string, int64, error) {
	hasher := sha256.New()
	size, err := io.Copy(hasher, io.LimitReader(body, storage.MaxObjectSize+1))
	if err != nil {
		return "", size, err
	}
	if size > storage.MaxObjectSize {
		return "", size, storage.ErrObjectTooLarge
	}
	return fmt.Sprintf("%x", hasher.Sum(nil)), size, nil
}

func objectMetadata(key string, data []byte) storage.ObjectMetadata {
	sum := sha256.Sum256(data)
	return storage.ObjectMetadata{
		Key:    key,
		Size:   int64(len(data)),
		SHA256: fmt.Sprintf("%x", sum),
	}
}
