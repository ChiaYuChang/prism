package llm

type BatchJobRequest struct {
	DisplayName string
}

type BatchJobResponse struct {
	Name        string
	DisplayName string
	State       string
	OutFileName string
	Raw         any
}
