package extraction

import (
	"context"

	"github.com/ChiaYuChang/prism/internal/llm"
	"github.com/ChiaYuChang/prism/internal/prompt"
	"github.com/google/uuid"
)

// PromptLoader retrieves an immutable prompt by its durable PromptID.
type PromptLoader interface {
	Load(ctx context.Context, id uuid.UUID) (prompt.Loaded, error)
}

type Dependencies struct {
	Generator    llm.Generator
	PromptLoader PromptLoader
}
