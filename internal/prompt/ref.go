package prompt

import (
	"context"
	"crypto/sha256"
	"fmt"
	"os"

	"github.com/ChiaYuChang/prism/internal/repo"
	"github.com/google/uuid"
)

type Ref struct {
	ID      uuid.UUID `json:"id"      mapstructure:"id"      yaml:"id"`
	Key     string    `json:"key"     mapstructure:"key"     yaml:"key"`
	Version int32     `json:"version" mapstructure:"version" yaml:"version"`
	Hash    string    `json:"hash"    mapstructure:"hash"    yaml:"hash"`
}

func (r Ref) Enabled() bool {
	return r.ID != uuid.Nil
}

func Resolve(ctx context.Context, prompts repo.Prompts, ref Ref) ([]byte, repo.PromptVersion, error) {
	if prompts == nil {
		return nil, repo.PromptVersion{}, fmt.Errorf("prompt repository is nil")
	}
	if !ref.Enabled() {
		return nil, repo.PromptVersion{}, fmt.Errorf("prompt id is required")
	}
	if ref.Key == "" {
		return nil, repo.PromptVersion{}, fmt.Errorf("prompt key is required")
	}
	if ref.Version <= 0 {
		return nil, repo.PromptVersion{}, fmt.Errorf("prompt version is required")
	}
	if ref.Hash == "" {
		return nil, repo.PromptVersion{}, fmt.Errorf("prompt hash is required")
	}

	version, err := prompts.GetPromptVersionByID(ctx, ref.ID)
	if err != nil {
		return nil, repo.PromptVersion{}, fmt.Errorf("load prompt version: %w", err)
	}
	if version.Key != ref.Key {
		return nil, repo.PromptVersion{}, fmt.Errorf("prompt key mismatch: config=%s db=%s", ref.Key, version.Key)
	}
	if version.Version != ref.Version {
		return nil, repo.PromptVersion{}, fmt.Errorf("prompt version mismatch: config=%d db=%d", ref.Version, version.Version)
	}
	if version.Hash != ref.Hash {
		return nil, repo.PromptVersion{}, fmt.Errorf("prompt hash mismatch: config=%s db=%s", ref.Hash, version.Hash)
	}

	body, err := os.ReadFile(version.Path)
	if err != nil {
		return nil, repo.PromptVersion{}, fmt.Errorf("read prompt file: %w", err)
	}
	if hashBytes(body) != ref.Hash {
		return nil, repo.PromptVersion{}, fmt.Errorf("prompt file hash mismatch: path=%s", version.Path)
	}
	return body, version, nil
}

func hashBytes(body []byte) string {
	sum := sha256.Sum256(body)
	return fmt.Sprintf("sha256:%x", sum[:])
}
