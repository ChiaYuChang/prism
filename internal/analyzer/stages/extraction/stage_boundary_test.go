package extraction_test

import (
	"context"
	"testing"

	"github.com/ChiaYuChang/prism/internal/analyzer/llmapi"
	"github.com/ChiaYuChang/prism/internal/analyzer/stages/extraction"
	"github.com/ChiaYuChang/prism/internal/prompt"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
)

type pinnedLoader struct{}

func (p *pinnedLoader) Load(ctx context.Context, id uuid.UUID) (prompt.Loaded, error) {
	return prompt.Loaded{
		ID:   id,
		Hash: "sha256:0123",
		Body: []byte("PINNED-SYSTEM-INSTRUCTION"),
	}, nil
}

func TestStage_SystemInstructionBoundary(t *testing.T) {
	fake := &FakeLLM{
		Results: []FakeResult{
			JSONResult(invalidSourceOutput()), // first attempt fails semantic validation
			JSONResult(validOutput()),         // second attempt passes
		},
	}

	deps := extraction.Dependencies{
		Generator:    fake,
		PromptLoader: &pinnedLoader{},
	}
	params := extraction.V1Parameters{
		Model:    "fake-model",
		PromptID: uuid.New(),
	}

	stage, err := extraction.NewV1(context.Background(), deps, params)
	require.NoError(t, err)

	p := llmapi.NewPacket[extraction.Input, any, extraction.Output](validInput(), nil)

	// Manual simulation of the run loop to check requests
	// Attempt 1: First try
	err = stage.PreProcess(context.Background(), p)
	require.NoError(t, err)

	err = stage.APICall(context.Background(), p)
	require.NoError(t, err)

	err = stage.PostProcess(context.Background(), p)
	require.Error(t, err)

	// Verify boundary for Attempt 1
	require.Equal(t, "PINNED-SYSTEM-INSTRUCTION", fake.Calls[0].SystemInstruction)
	require.Contains(t, fake.Calls[0].Prompt, validArticleContent)
	require.NotContains(t, fake.Calls[0].Prompt, "previous_output")
	require.NotContains(t, fake.Calls[0].Prompt, "validation_errors")

	// Attempt 2: Repair
	var reattempt *llmapi.ReAttemptError
	require.ErrorAs(t, err, &reattempt)
	p.RepairHint = reattempt.Hint

	err = stage.PreProcess(context.Background(), p)
	require.NoError(t, err)

	err = stage.APICall(context.Background(), p)
	require.NoError(t, err)

	err = stage.PostProcess(context.Background(), p)
	require.NoError(t, err)

	// Verify boundary for Attempt 2
	require.Equal(t, "PINNED-SYSTEM-INSTRUCTION", fake.Calls[1].SystemInstruction)
	require.Contains(t, fake.Calls[1].Prompt, validArticleContent)
	require.Contains(t, fake.Calls[1].Prompt, "previous_output")
	require.Contains(t, fake.Calls[1].Prompt, "validation_errors")
}
