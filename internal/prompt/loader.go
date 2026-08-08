package prompt

import (
	"context"

	"github.com/google/uuid"
)

// MetadataGetter retrieves basic version metadata for a prompt.
type MetadataGetter interface {
	GetByID(ctx context.Context, id uuid.UUID) (Version, error)
}

// Version represents the metadata of a prompt version.
type Version struct {
	ID      uuid.UUID
	Hash    string
	Name    string
	Version int
}

// Loaded represents a resolved prompt with its content bytes.
type Loaded struct {
	ID   uuid.UUID
	Hash string
	Body []byte
}

// Loader orchestrates resolving a durable PromptID into its immutable content.
type Loader struct {
	metadata MetadataGetter
	resolver *Resolver
}

// NewLoader creates a new prompt loader.
func NewLoader(metadata MetadataGetter, resolver *Resolver) *Loader {
	return &Loader{
		metadata: metadata,
		resolver: resolver,
	}
}

// Load retrieves a prompt's metadata and resolves its content.
func (l *Loader) Load(ctx context.Context, id uuid.UUID) (Loaded, error) {
	meta, err := l.metadata.GetByID(ctx, id)
	if err != nil {
		return Loaded{}, err
	}

	body, err := l.resolver.Resolve(ctx, meta.Hash)
	if err != nil {
		return Loaded{}, err
	}

	return Loaded{
		ID:   meta.ID,
		Hash: meta.Hash,
		Body: body,
	}, nil
}
