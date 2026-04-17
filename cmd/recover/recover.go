package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"time"

	"github.com/ChiaYuChang/prism/internal/collector/archiver"
	"github.com/ChiaYuChang/prism/internal/collector/minifier"
	"github.com/ChiaYuChang/prism/internal/collector/parser"
	"github.com/ChiaYuChang/prism/internal/collector/transformer"
	"github.com/ChiaYuChang/prism/internal/repo"
	"github.com/google/uuid"
)

func runRecover(ctx context.Context, arch archiver.Archiver, pipeline repo.Pipeline, logger *slog.Logger, opts cliOptions) error {
	scanOpts := archiver.ScanOptions{
		Since:   opts.since,
		Until:   opts.until,
		Limit:   opts.limit,
		TraceID: opts.traceID,
		Stage:   archiver.MetaStageRaw,
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
	prs := parser.NewArticleParser()

	var succeeded, skipped, failed int

	for _, m := range metas {
		log := logger.With(slog.String("trace_id", m.TraceID), slog.String("url", m.URL))

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

		raw, err := arch.Load(ctx, m.TraceID)
		if err != nil {
			log.Error("failed to load archive", "error", err)
			failed++
			continue
		}

		if opts.dryRun {
			fmt.Printf("[dry-run] would recover trace_id=%s url=%s source=%s\n", m.TraceID, m.URL, m.SourceAbbr)
			succeeded++
			continue
		}

		minified, err := min.Minify(ctx, raw)
		if err != nil {
			log.Error("minify still fails", "error", err)
			failed++
			continue
		}

		canonical, err := tfm.Transform(ctx, minified)
		if err != nil {
			log.Error("transform failed", "error", err)
			failed++
			continue
		}

		art, err := prs.Parse(ctx, m.URL, canonical)
		if err != nil {
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
