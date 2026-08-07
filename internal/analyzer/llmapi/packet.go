package llmapi

import "github.com/ChiaYuChang/prism/internal/llm"

// Packet carries the input and mutable working data for one Stage execution.
// A new Packet must be created for every execution attempt.
type Packet[I, S, O any] struct {
	input I

	RepairHint *RepairHint

	State    S
	Request  *llm.GenerateRequest
	Response *llm.GenerateResponse
	Output   O
}

// NewPacket creates a Packet for input and optional repair hint.
func NewPacket[I, S, O any](input I, hint *RepairHint) *Packet[I, S, O] {
	return &Packet[I, S, O]{input: input, RepairHint: hint}
}

// Input returns the immutable input associated with the Packet.
func (p *Packet[I, S, O]) Input() I {
	return p.input
}
