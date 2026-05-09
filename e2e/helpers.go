//go:build e2e

package e2e

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"testing"
	"time"

	"github.com/ChiaYuChang/prism/internal/http/api"
	"github.com/ChiaYuChang/prism/internal/model"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
)

type e2eEnv struct {
	apiBase string
	dsn     string
}

func loadEnv(t *testing.T) e2eEnv {
	t.Helper()
	dsn := os.Getenv("PRISM_E2E_DSN")
	if dsn == "" {
		t.Skip("PRISM_E2E_DSN unset; skipping e2e")
	}
	base := os.Getenv("PRISM_E2E_API_BASE")
	if base == "" {
		base = "http://localhost:8090"
	}
	return e2eEnv{apiBase: base, dsn: dsn}
}

func openPool(ctx context.Context, t *testing.T, dsn string) *pgxpool.Pool {
	t.Helper()
	pool, err := pgxpool.New(ctx, dsn)
	if err != nil {
		t.Fatalf("pgxpool.New: %v", err)
	}
	if err := pool.Ping(ctx); err != nil {
		pool.Close()
		t.Fatalf("pgxpool.Ping: %v", err)
	}
	return pool
}

// seedCandidate inserts a candidate row directly so the driver isolates the
// user-line (api-server → scheduler PAGE_FETCH → collector) without
// involving discovery. Returns the candidate id (server-side uuidv7 default
// is preserved by RETURNING).
func seedCandidate(
	ctx context.Context, t *testing.T, pool *pgxpool.Pool,
	sourceAbbr, url, title string, publishedAt time.Time, traceID string,
) uuid.UUID {
	t.Helper()
	cand := model.Candidates{
		SourceAbbr:  sourceAbbr,
		URL:         url,
		Title:       title,
		PublishedAt: publishedAt,
	}
	fp := cand.Fingerprint()
	const q = `
INSERT INTO candidates (source_abbr, trace_id, fingerprint, url, title, ingestion_method, published_at)
VALUES ($1, $2, $3, $4, $5, 'MANUAL', $6)
ON CONFLICT (fingerprint) DO UPDATE SET title = EXCLUDED.title
RETURNING id`
	var id uuid.UUID
	if err := pool.QueryRow(ctx, q, sourceAbbr, traceID, fp, url, title, publishedAt).Scan(&id); err != nil {
		t.Fatalf("seed candidate: %v", err)
	}
	return id
}

func postPageFetch(ctx context.Context, t *testing.T, base string, ids []uuid.UUID) api.PageFetchResponse {
	t.Helper()
	body, err := json.Marshal(api.PageFetchRequest{CandidateIDs: ids})
	if err != nil {
		t.Fatalf("marshal page_fetch req: %v", err)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, base+"/api/v1/page_fetch", bytes.NewReader(body))
	if err != nil {
		t.Fatalf("build POST req: %v", err)
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("POST /page_fetch: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusAccepted {
		raw, _ := io.ReadAll(resp.Body)
		t.Fatalf("POST /page_fetch: status=%d body=%s", resp.StatusCode, raw)
	}
	var out api.PageFetchResponse
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		t.Fatalf("decode page_fetch resp: %v", err)
	}
	return out
}

// pollFetch hits GET /fetches/{id} until terminal=true or timeout.
func pollFetch(
	ctx context.Context, t *testing.T, base string, fetchID uuid.UUID,
	timeout, interval time.Duration,
) api.FetchProgressResponse {
	t.Helper()
	deadline := time.Now().Add(timeout)
	url := fmt.Sprintf("%s/api/v1/fetches/%s", base, fetchID)
	var last api.FetchProgressResponse
	for {
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
		if err != nil {
			t.Fatalf("build GET req: %v", err)
		}
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatalf("GET /fetches/{id}: %v", err)
		}
		if resp.StatusCode != http.StatusOK {
			raw, _ := io.ReadAll(resp.Body)
			resp.Body.Close()
			t.Fatalf("GET /fetches/{id}: status=%d body=%s", resp.StatusCode, raw)
		}
		if err := json.NewDecoder(resp.Body).Decode(&last); err != nil {
			resp.Body.Close()
			t.Fatalf("decode fetch progress: %v", err)
		}
		resp.Body.Close()
		if last.Terminal {
			return last
		}
		if time.Now().After(deadline) {
			t.Fatalf("fetch %s not terminal within %s; last=%+v", fetchID, timeout, last)
		}
		select {
		case <-time.After(interval):
		case <-ctx.Done():
			t.Fatalf("ctx cancelled while polling: %v", ctx.Err())
		}
	}
}

func assertContent(ctx context.Context, t *testing.T, pool *pgxpool.Pool, candidateID uuid.UUID) {
	t.Helper()
	const q = `SELECT title, content FROM contents WHERE candidate_id = $1`
	var title, content string
	if err := pool.QueryRow(ctx, q, candidateID).Scan(&title, &content); err != nil {
		t.Fatalf("contents row missing for candidate %s: %v", candidateID, err)
	}
	if title == "" {
		t.Errorf("contents.title empty for candidate %s", candidateID)
	}
	if content == "" {
		t.Errorf("contents.content empty for candidate %s", candidateID)
	}
}
