package archiver

import (
	"fmt"
	"log/slog"

	"github.com/ChiaYuChang/prism/internal/storage/filesystem"
)

// LocalArchiver stores archives using a local filesystem-backed storage store.
type LocalArchiver struct {
	*archiveStore
}

var _ Archiver = (*LocalArchiver)(nil)

// NewLocalArchiver creates a LocalArchiver rooted at baseDir.
func NewLocalArchiver(baseDir string, logger *slog.Logger) (*LocalArchiver, error) {
	store, err := filesystem.NewLocalStore(baseDir)
	if err != nil {
		return nil, fmt.Errorf("create local archive store: %w", err)
	}
	archive, err := newArchiveStore(store, logger)
	if err != nil {
		return nil, err
	}
	return &LocalArchiver{archiveStore: archive}, nil
}
