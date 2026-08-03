package pipeline

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestCompileControlTasksLinksStageZeroToLinearPlan(t *testing.T) {
	spec := PipelineSpec{Pipeline: []StageSpec{
		{Name: "later", DependsOn: []string{"first"}},
		{Name: "first"},
	}}

	got, err := CompileControlTasks(spec, "pipeline:init")
	require.NoError(t, err)
	require.Equal(t, []ControlTask{
		{LogicalKey: "pipeline:stage:first", Stage: StageSpec{Name: "first"}, Previous: "pipeline:init"},
		{LogicalKey: "pipeline:stage:later", Stage: StageSpec{Name: "later", DependsOn: []string{"first"}}, Previous: "pipeline:stage:first"},
	}, got)
}
