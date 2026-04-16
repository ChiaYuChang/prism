package message

import (
	"time"

	"github.com/ChiaYuChang/prism/pkg/utils"
	"github.com/google/uuid"
)

const (
	ArchiveTopic     = "prism.archive"
	ArchiveDeadTopic = "prism.archive.dead"
)

// ArchiveSignal is published by the Collector Worker after a content row is created.
// The Archiver Worker consumes it and persists the canonical HTML to object storage.
// Page uses a self-describing CompressedBlob so the archiver can decompress without
// coupling to a specific algorithm.
type ArchiveSignal struct {
	ContentID uuid.UUID          `json:"content_id"`
	URL       string             `json:"url"`
	TraceID   string             `json:"trace_id"`
	FetchedAt time.Time          `json:"fetched_at"`
	Page      utils.CompressedBlob `json:"page"`
}
