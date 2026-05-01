package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/ChiaYuChang/prism/internal/collector"
	"github.com/ChiaYuChang/prism/internal/collector/archiver"
	"github.com/ChiaYuChang/prism/internal/collector/minifier"
	"github.com/ChiaYuChang/prism/internal/collector/parser"
	"github.com/ChiaYuChang/prism/internal/collector/transformer"
	"github.com/ChiaYuChang/prism/internal/repo"
	"github.com/google/uuid"
)

func runRecover(ctx context.Context, arch archiver.Archiver, pipeline repo.Pipeline, prs collector.Parser, logger *slog.Logger, opts cliOptions) error {
	scanOpts := archiver.ScanOptions{
		Since:   opts.since,
		Until:   opts.until,
		Limit:   opts.limit,
		TraceID: opts.traceID,
		// PayloadKind unset = scan all kinds. Per-archive replay path is
		// chosen below based on m.PayloadKind: raw → M+T+P, minified → T+P,
		// canonical → P only.
	}
	metas, err := arch.Scan(ctx, scanOpts)
	if err != nil {
		return err
	}
	if len(metas) == 0 {
		fmt.Println("no recoverable archives found")
		return nil
	}

	min := minifier.New()
	tfm := transformer.NewNoOpTransformer()

	var succeeded, skipped, failed int

	for _, m := range metas {
		log := logger.With(
			slog.String("trace_id", m.TraceID),
			slog.String("url", m.URL),
			slog.String("kind", string(m.PayloadKind)),
		)

		if _, err := pipeline.GetContentByURL(ctx, m.URL); err == nil {
			log.Info("content already exists, skipping")
			skipped++
			continue
		}

		if m.SourceAbbr == "" {
			log.Warn("archive missing source_abbr metadata, skipping")
			skipped++
			continue
		}

		payload, err := arch.Load(ctx, m.TraceID)
		if err != nil {
			log.Error("failed to load archive", "error", err)
			failed++
			continue
		}

		if opts.dryRun {
			fmt.Printf("[dry-run] would recover trace_id=%s url=%s kind=%s source=%s\n",
				m.TraceID, m.URL, m.PayloadKind, m.SourceAbbr)
			succeeded++
			continue
		}

		canonical, ok := buildCanonical(ctx, m.PayloadKind, payload, min, tfm, log)
		if !ok {
			failed++
			continue
		}

		art, err := prs.Parse(ctx, m.URL, canonical)
		if err != nil {
			if errors.Is(err, parser.ErrNoMatchingParser) {
				log.Warn("no parser configured for host, skipping", "error", err)
				skipped++
				continue
			}
			log.Error("parse failed", "error", err)
			failed++
			continue
		}

		contentType := "ARTICLE"
		if m.SourceType == repo.SourceTypeParty {
			contentType = "PARTY_RELEASE"
		}

		fetchedAt := time.Now()
		publishedAt := art.PublishedAt
		metadata := map[string]any{
			"recovered":          true,
			"recovered_at":       fetchedAt.Format(time.RFC3339),
			"original_trace_id":  m.TraceID,
			"original_error":     m.Error,
		}
		if publishedAt.IsZero() {
			publishedAt = fetchedAt
			metadata["published_at_estimated"] = true
		}
		metaBytes, _ := json.Marshal(metadata)

		batchID := uuid.Nil
		if m.BatchID != "" {
			if id, err := uuid.Parse(m.BatchID); err == nil {
				batchID = id
			}
		}

		params := repo.CreateContentParams{
			BatchID:     batchID,
			Type:        contentType,
			SourceAbbr:  m.SourceAbbr,
			URL:         m.URL,
			Title:       art.Title,
			Content:     art.Content,
			TraceID:     m.TraceID,
			PublishedAt: publishedAt,
			FetchedAt:   fetchedAt,
			Metadata:    metaBytes,
		}
		if art.Author != "" {
			params.Author = &art.Author
		}

		content, err := pipeline.CreateContent(ctx, params)
		if err != nil {
			log.Error("create content failed", "error", err)
			failed++
			continue
		}

		log.Info("content recovered", "content_id", content.ID.String())
		succeeded++
	}

	fmt.Printf("\nRecovery complete: %d succeeded, %d skipped, %d failed (of %d total)\n",
		succeeded, skipped, failed, len(metas))
	return nil
}

// buildCanonical replays the pipeline subset implied by an archive's PayloadKind:
//   - raw (or empty for back-compat): Minify → Transform → canonical
//   - minified:                       Transform → canonical
//   - canonical:                      identity (already canonical)
//
// Returns (canonical, true) on success or ("", false) on stage error (logged).
func buildCanonical(
	ctx context.Context,
	kind archiver.PayloadKind,
	payload string,
	min collector.Transformer,
	tfm collector.Transformer,
	log *slog.Logger,
) (string, bool) {
	canonical := payload

	switch kind {
	case archiver.PayloadKindRaw, "":
		minified, err := min.Transform(ctx, canonical)
		if err != nil {
			log.Error("minify still fails", "error", err)
			return "", false
		}
		canonical = minified
		fallthrough
	case archiver.PayloadKindMinified:
		out, err := tfm.Transform(ctx, canonical)
		if err != nil {
			log.Error("transform failed", "error", err)
			return "", false
		}
		canonical = out
	case archiver.PayloadKindCanonical:
		// already canonical; nothing to do
	default:
		log.Warn("unknown payload kind, treating as canonical")
	}

	return canonical, true
}
