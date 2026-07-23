package llm

// EmbedRequest encapsulates parameters for an embedding task.
type EmbedRequest struct {
	Model      string
	Input      []string
	Dimensions int
	Meta       map[string]string
}

func NewEmbedRequest(model string, input ...string) *EmbedRequest {
	return &EmbedRequest{
		Model: model,
		Input: input,
	}
}

// EmbedResponse holds the resulting vectors.
type EmbedResponse struct {
	Model   string
	Vectors [][]float32
	Raw     any
}
