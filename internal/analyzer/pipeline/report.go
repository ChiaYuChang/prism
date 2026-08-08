package pipeline

import (
	"bytes"
	"context"
	"crypto/sha256"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/ChiaYuChang/prism/internal/repo"
	"github.com/ChiaYuChang/prism/internal/storage"
	"github.com/google/uuid"
)

// ReportWriter owns deterministic artifact delivery for the final pipeline
// stage. Storage is written before the repository transaction links the report
// and completes the control task.
type ReportWriter interface {
	Deliver(context.Context, repo.Task, repo.Batch, WorkSetInput) error
}

// MarkdownReportWriter emits a bounded deterministic Markdown artifact from
// the immutable Root input snapshot. A richer renderer can replace this type
// without changing the delivery transaction or storage key contract.
type MarkdownReportWriter struct {
	store   storage.ImmutableStore
	runtime repo.PipelineRuntime
	ttl     time.Duration
}

// NewMarkdownReportWriter creates the default report artifact writer.
func NewMarkdownReportWriter(store storage.ImmutableStore, runtime repo.PipelineRuntime, ttl time.Duration) (*MarkdownReportWriter, error) {
	if store == nil {
		return nil, fmt.Errorf("report storage is required")
	}
	if runtime == nil {
		return nil, fmt.Errorf("pipeline runtime is required")
	}
	if ttl <= 0 {
		return nil, fmt.Errorf("report cache ttl must be positive")
	}
	return &MarkdownReportWriter{store: store, runtime: runtime, ttl: ttl}, nil
}

// Deliver writes or verifies the deterministic report artifact and atomically
// links it to the execution while completing the final control task.
func (w *MarkdownReportWriter) Deliver(ctx context.Context, task repo.Task, root repo.Batch, input WorkSetInput) error {
	if root.AnalysisExecutionID == nil || *root.AnalysisExecutionID == uuid.Nil {
		return fmt.Errorf("analysis execution is missing from pipeline Root")
	}
	body, err := renderMarkdownReport(input)
	if err != nil {
		return err
	}
	executionID := *root.AnalysisExecutionID
	key := "reports/" + executionID.String() + ".md"
	result, err := w.store.PutIfAbsent(ctx, key, bytes.NewReader(body), storage.PutOptions{ContentType: "text/markdown; charset=utf-8"})
	if err != nil {
		return fmt.Errorf("write report artifact: %w", err)
	}
	if result.Checksum.SHA256 == "" {
		return fmt.Errorf("report storage returned no verified hash")
	}
	expectedDigest := sha256.Sum256(body)
	expectedHash := fmt.Sprintf("%x", expectedDigest)
	if int64(len(body)) != result.Checksum.Size {
		return fmt.Errorf("report storage size mismatch: wrote %d, stored %d", len(body), result.Checksum.Size)
	}
	if result.Checksum.SHA256 != expectedHash {
		return fmt.Errorf("report storage hash mismatch")
	}
	expiresAt := time.Now().Add(w.ttl)
	_, err = w.runtime.CompleteAnalysisReport(ctx, repo.CompleteAnalysisReportParams{
		TaskID:              task.ID,
		RootBatchID:         root.ID,
		AnalysisExecutionID: executionID,
		Report: repo.CreateReportParams{
			AnalysisExecutionID: executionID,
			RootBatchID:         root.ID,
			StorageURI:          key,
			ByteSize:            int64(len(body)),
			SHA256:              result.Checksum.SHA256,
			ExpiresAt:           expiresAt,
		},
	})
	if err != nil {
		return fmt.Errorf("link report and complete task: %w", err)
	}
	return nil
}

func renderMarkdownReport(input WorkSetInput) ([]byte, error) {
	ids := append([]uuid.UUID(nil), input.ContentIDs...)
	sort.Slice(ids, func(i, j int) bool { return ids[i].String() < ids[j].String() })
	var out strings.Builder
	out.WriteString("# Analysis Report\n\n")
	out.WriteString("Generated from the immutable analysis input snapshot.\n\n")
	out.WriteString("## Contents\n\n")
	for _, id := range ids {
		content, ok := input.ContentSnapshots[id]
		if !ok {
			return nil, fmt.Errorf("content snapshot %s is missing", id)
		}
		title := strings.ReplaceAll(strings.TrimSpace(content.Title), "\n", " ")
		url := strings.TrimSpace(content.URL)
		out.WriteString("- ")
		out.WriteString(title)
		if url != "" {
			out.WriteString(" ([source](")
			out.WriteString(url)
			out.WriteString("))")
		}
		out.WriteString("\n")
	}
	if len(ids) == 0 {
		out.WriteString("No content records were available in the snapshot.\n")
	}
	body := []byte(out.String())
	if int64(len(body)) > storage.MaxObjectSize {
		return nil, storage.ErrObjectTooLarge
	}
	return body, nil
}
