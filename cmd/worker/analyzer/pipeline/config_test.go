package main

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestLoadConfigRejectsNonDefaultPipelineFile(t *testing.T) {
	_, err := LoadConfig([]string{"--pipeline-file", "configs/other.yaml"})
	require.Error(t, err)
	require.True(t, strings.Contains(err.Error(), `pipeline-file must be "configs/llm_pipeline.yaml"`), err)
}
