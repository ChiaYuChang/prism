package pipeline

import "fmt"

// ControlTask is the durable planning input for one stage-control task.
type ControlTask struct {
	LogicalKey string
	Stage      StageSpec
	Previous   string
}

// CompileControlTasks converts the declarative order into a linear control chain.
// The first control task follows previousTaskKey; each later task follows the
// preceding configured stage, regardless of the original DAG branching.
func CompileControlTasks(spec PipelineSpec, previousTaskKey string) ([]ControlTask, error) {
	ordered, err := Compile(spec)
	if err != nil {
		return nil, err
	}
	if previousTaskKey == "" {
		return nil, fmt.Errorf("previous task key is required")
	}

	control := make([]ControlTask, len(ordered))
	previous := previousTaskKey
	for i, stage := range ordered {
		key := "pipeline:stage:" + stage.Name
		control[i] = ControlTask{LogicalKey: key, Stage: stage, Previous: previous}
		previous = key
	}
	return control, nil
}
