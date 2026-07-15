package prompt

import (
	"context"
	"crypto/sha256"
	"fmt"
	"io"
	"strings"

	"github.com/ChiaYuChang/prism/internal/repo"
	"github.com/ChiaYuChang/prism/internal/storage"
	"github.com/google/uuid"
)

type Ref struct {
	ID      uuid.UUID `json:"id"      mapstructure:"id"      yaml:"id"`
	Key     string    `json:"key"     mapstructure:"key"     yaml:"key"`
	Version int32     `json:"version" mapstructure:"version" yaml:"version"`
	Hash    string    `json:"hash"    mapstructure:"hash"    yaml:"hash"`
}

func (r Ref) Enabled() bool {
	return r.ID != uuid.Nil || strings.TrimSpace(r.Key) != ""
}

func Resolve(ctx context.Context, prompts repo.Prompts, store storage.Store, ref Ref) ([]byte, repo.PromptVersion, error) {
	if prompts == nil {
		return nil, repo.PromptVersion{}, fmt.Errorf("prompt repository is nil")
	}
	if store == nil {
		return nil, repo.PromptVersion{}, fmt.Errorf("prompt storage is nil")
	}
	if !ref.Enabled() {
		return nil, repo.PromptVersion{}, fmt.Errorf("prompt name or id is required")
	}

	ref.Key = strings.ReplaceAll(strings.Trim(strings.TrimSpace(ref.Key), "."), "/", ".")
	var version repo.PromptVersion
	var err error
	if ref.ID != uuid.Nil {
		version, err = prompts.GetPromptVersionByID(ctx, ref.ID)
	} else {
		version, err = prompts.GetLatestPromptVersionByName(ctx, ref.Key)
	}
	if err != nil {
		return nil, repo.PromptVersion{}, fmt.Errorf("load prompt version: %w", err)
	}
	if ref.Key != "" && version.Name != ref.Key {
		return nil, repo.PromptVersion{}, fmt.Errorf("prompt key mismatch: config=%s db=%s", ref.Key, version.Name)
	}
	if ref.Version > 0 && version.Version != ref.Version {
		return nil, repo.PromptVersion{}, fmt.Errorf("prompt version mismatch: config=%d db=%d", ref.Version, version.Version)
	}
	if ref.Hash != "" && version.Hash != ref.Hash {
		return nil, repo.PromptVersion{}, fmt.Errorf("prompt hash mismatch: config=%s db=%s", ref.Hash, version.Hash)
	}

	key := ObjectKey(version.Hash)
	object, err := store.Get(ctx, key)
	if err != nil {
		return nil, repo.PromptVersion{}, fmt.Errorf("read prompt object: %w", err)
	}
	defer func() { _ = object.Close() }()
	body, err := io.ReadAll(object)
	if err != nil {
		return nil, repo.PromptVersion{}, fmt.Errorf("read prompt object: %w", err)
	}
	if hashBytes(body) != version.Hash {
		return nil, repo.PromptVersion{}, fmt.Errorf("prompt object hash mismatch: key=%s", key)
	}
	return body, version, nil
}

func hashBytes(body []byte) string {
	sum := sha256.Sum256(body)
	return fmt.Sprintf("sha256:%x", sum[:])
}
