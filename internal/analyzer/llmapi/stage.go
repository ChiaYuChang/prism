package llmapi

import "context"

// Stage is one reusable Analyzer LLM round trip.
type Stage[I, S, O any] interface {
	Name() string
	PreProcess(context.Context, *Packet[I, S, O]) error
	APICall(context.Context, *Packet[I, S, O]) error
	PostProcess(context.Context, *Packet[I, S, O]) error
}

// Step identifies one lifecycle operation executed by Runner.
type Step string

const (
	StepPreProcess  Step = "pre_process"
	StepAPICall     Step = "api_call"
	StepPostProcess Step = "post_process"
)
