package rotatingfile

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/ChiaYuChang/prism/pkg/units"
)

// ErrParamMissing is returned when a required constructor parameter is missing.
var ErrParamMissing = errors.New("param missing")

// Config configures the FilePool.
type Config struct {
	Path     string
	MaxSize  units.Bytes
	MaxFiles int
}

// FilePool implements io.WriteCloser and a Sync method for concurrent,
// slot-based log rotation.
type FilePool struct {
	mu         sync.Mutex
	path       string
	maxSize    int64
	maxFiles   int
	index      int
	file       *os.File
	size       int64
	created    int
	closed     bool
	noRotation bool
}

// New creates and returns a new FilePool. It validates the configuration
// and opens the initial file handle.
func New(cfg Config) (*FilePool, error) {
	if cfg.Path == "" {
		return nil, fmt.Errorf("%w: path", ErrParamMissing)
	}

	maxSize, err := cfg.MaxSize.Int64()
	if err != nil {
		return nil, fmt.Errorf("invalid max-size: %w", err)
	}

	p := &FilePool{
		path:     cfg.Path,
		maxSize:  maxSize,
		maxFiles: cfg.MaxFiles,
	}

	if maxSize <= 0 {
		p.noRotation = true
		if err := os.MkdirAll(filepath.Dir(p.path), 0o755); err != nil {
			return nil, fmt.Errorf("create log directory: %w", err)
		}
		f, err := os.OpenFile(p.path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
		if err != nil {
			return nil, fmt.Errorf("open log file: %w", err)
		}
		p.file = f
		info, err := f.Stat()
		if err != nil {
			_ = f.Close()
			return nil, fmt.Errorf("stat log file: %w", err)
		}
		p.size = info.Size()
		return p, nil
	}

	if cfg.MaxFiles < 1 {
		return nil, fmt.Errorf("max-files must be >= 1")
	}

	if err := os.MkdirAll(filepath.Dir(p.slotPath(0)), 0o755); err != nil {
		return nil, fmt.Errorf("create log directory: %w", err)
	}
	if err := p.rejectStaleSlots(); err != nil {
		return nil, err
	}
	index, size, created, err := p.prepareSlots()
	if err != nil {
		return nil, err
	}
	p.created = created
	flags := os.O_CREATE | os.O_WRONLY | os.O_APPEND
	if size >= maxSize {
		index = (index + 1) % cfg.MaxFiles
		size = 0
		flags = os.O_CREATE | os.O_WRONLY | os.O_TRUNC
	}

	f, err := os.OpenFile(p.slotPath(index), flags, 0o644)
	if err != nil {
		return nil, fmt.Errorf("open initial log slot: %w", err)
	}
	p.file = f
	p.index = index
	p.size = size
	return p, nil
}

// slotPath returns the file path for the given slot index.
func (p *FilePool) slotPath(idx int) string {
	return fmt.Sprintf("%s.%d", p.path, idx)
}

// CreatedSlots returns how many missing slots were created during New.
func (p *FilePool) CreatedSlots() int {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.created
}

func (p *FilePool) prepareSlots() (int, int64, int, error) {
	newestIndex := 0
	newestSize := int64(0)
	var newestMod time.Time
	foundNonEmpty := false
	created := 0

	for i := range p.maxFiles {
		path := p.slotPath(i)
		if _, err := os.Stat(path); err != nil {
			if !os.IsNotExist(err) {
				return 0, 0, 0, fmt.Errorf("stat log slot %d: %w", i, err)
			}
			created++
		}
		f, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
		if err != nil {
			return 0, 0, 0, fmt.Errorf("create log slot %d: %w", i, err)
		}
		info, err := f.Stat()
		closeErr := f.Close()
		if err != nil {
			return 0, 0, 0, fmt.Errorf("stat log slot %d: %w", i, err)
		}
		if closeErr != nil {
			return 0, 0, 0, fmt.Errorf("close log slot %d: %w", i, closeErr)
		}
		if info.Size() == 0 {
			continue
		}
		if !foundNonEmpty || info.ModTime().After(newestMod) {
			foundNonEmpty = true
			newestIndex = i
			newestSize = info.Size()
			newestMod = info.ModTime()
		}
	}
	return newestIndex, newestSize, created, nil
}

func (p *FilePool) rejectStaleSlots() error {
	matches, err := filepath.Glob(p.path + ".*")
	if err != nil {
		return fmt.Errorf("list log slots: %w", err)
	}
	for _, match := range matches {
		idx, ok := p.slotIndex(match)
		if !ok || idx < p.maxFiles {
			continue
		}
		return fmt.Errorf("max-files shrink leaves existing log slot %q outside configured pool; archive or remove it manually", match)
	}
	return nil
}

func (p *FilePool) slotIndex(path string) (int, bool) {
	prefix := p.path + "."
	if !strings.HasPrefix(path, prefix) {
		return 0, false
	}
	idx, err := strconv.Atoi(strings.TrimPrefix(path, prefix))
	return idx, err == nil
}

// Write writes bytes to the current log file. It automatically rotates
// files when the size limit is exceeded.
func (p *FilePool) Write(b []byte) (int, error) {
	p.mu.Lock()
	defer p.mu.Unlock()

	if p.closed {
		return 0, os.ErrClosed
	}

	if p.noRotation {
		n, err := p.file.Write(b)
		p.size += int64(n)
		return n, err
	}

	payloadLen := int64(len(b))

	// Rotate if current size + len(payload) > MaxSize and current slot is non-empty.
	// Large write rule: if len(payload) > MaxSize, write it anyway to an empty slot.
	if p.size+payloadLen > p.maxSize && p.size > 0 {
		nextIndex := (p.index + 1) % p.maxFiles
		slotPath := p.slotPath(nextIndex)
		f, err := os.OpenFile(slotPath, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o644)
		if err != nil {
			return 0, fmt.Errorf("open log slot during rotation: %w", err)
		}
		if err := p.file.Close(); err != nil {
			_ = f.Close()
			return 0, fmt.Errorf("close log slot during rotation: %w", err)
		}
		p.file = f
		p.index = nextIndex
		p.size = 0
	}

	n, err := p.file.Write(b)
	p.size += int64(n)
	return n, err
}

// Close closes the current active log file.
func (p *FilePool) Close() error {
	p.mu.Lock()
	defer p.mu.Unlock()

	if p.closed {
		return nil
	}

	p.closed = true
	if p.file != nil {
		err := p.file.Close()
		p.file = nil
		return err
	}
	return nil
}

// Sync flushes the current active log file to stable storage.
func (p *FilePool) Sync() error {
	p.mu.Lock()
	defer p.mu.Unlock()

	if p.closed {
		return os.ErrClosed
	}

	if p.file != nil {
		return p.file.Sync()
	}
	return nil
}
