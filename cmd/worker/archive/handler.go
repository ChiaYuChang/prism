package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/ChiaYuChang/prism/internal/collector"
	"github.com/ChiaYuChang/prism/internal/message"
	wm "github.com/ThreeDotsLabs/watermill/message"
	"github.com/google/uuid"
)

var (
	ErrParamMissing         = errors.New("param missing")
	ErrInvalidArchiveSignal = errors.New("invalid archive signal")
)

type Handler struct {
	saver collector.Saver
}

func NewHandler(saver collector.Saver) (*Handler, error) {
	if saver == nil {
		return nil, fmt.Errorf("%w: saver", ErrParamMissing)
	}
	return &Handler{saver: saver}, nil
}

func (h *Handler) HandleMessage(ctx context.Context, msg *wm.Message) (bool, error) {
	var sig message.ArchiveSignal
	if err := json.Unmarshal(msg.Payload, &sig); err != nil {
		return true, fmt.Errorf("%w: decode: %w", ErrInvalidArchiveSignal, err)
	}
	if sig.ContentID == uuid.Nil || sig.URL == "" || sig.FetchedAt.IsZero() {
		return true, fmt.Errorf("%w: content_id, url, and fetched_at are required", ErrInvalidArchiveSignal)
	}
	payload, err := sig.Page.UnpackString()
	if err != nil {
		return true, fmt.Errorf("%w: unpack page: %w", ErrInvalidArchiveSignal, err)
	}
	if err := h.saver.Save(ctx, collector.Archive{
		ID:        sig.ContentID.String(),
		URL:       sig.URL,
		Payload:   payload,
		TraceID:   sig.TraceID,
		Timestamp: sig.FetchedAt,
		Metadata:  map[string]any{"kind": "canonical"},
	}); err != nil {
		return false, fmt.Errorf("save archive: %w", err)
	}
	return true, nil
}
