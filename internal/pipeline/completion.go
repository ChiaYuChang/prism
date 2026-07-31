package pipeline

// Completion describes the direct-task state of a declared batch.
type Completion struct {
	Expected  *int32
	Actual    int
	Terminal  int
	Failed    int
	Cancelled int
}

// Finished reports whether expansion is finalized and every declared direct
// task has reached a terminal state.
func (c Completion) Finished() bool {
	return c.Expected != nil && c.Actual == int(*c.Expected) && c.Terminal == int(*c.Expected)
}

// Succeeded reports whether a finished batch has no failed or cancelled task.
func (c Completion) Succeeded() bool {
	return c.Failed == 0 && c.Cancelled == 0
}
