package extraction

import (
	"fmt"

	"github.com/google/uuid"
)

type V1Parameters struct {
	Model string `json:"model" validate:"required"`
	// PromptID specifies the exact pinned version of the extraction semantic policy.
	PromptID uuid.UUID `json:"prompt_id" validate:"required"`
}

func (p *V1Parameters) Validate() error {
	if p.Model == "" {
		return fmt.Errorf("model is required")
	}
	if p.PromptID == uuid.Nil {
		return fmt.Errorf("prompt_id is required")
	}
	return nil
}
