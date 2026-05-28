//go:build e2e

// Phase 6 / Phase 2.7 Stage 4 driver: prove the user-facing path
// (POST /page_fetch → GET /fetches/{id} terminal) end-to-end against a
// live stack. Discovery is not exercised; the candidate row is seeded
// directly so a failure isolates to the user line (api-server,
// fetches/fetch_items, scheduler PAGE_FETCH, collector, archiver).
//
// Required env:
//
//	PRISM_E2E_DSN       — Postgres DSN reaching the running e2e stack.
//	PRISM_E2E_API_BASE  — api-server base URL (default http://localhost:8090).
//
// Run:
//
//	go test -tags=e2e -count=1 -v ./e2e/...
package e2e

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
)

func TestPageFetch_HappyPath_e2e(t *testing.T) {
	env := loadEnv(t)
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	pool := openPool(ctx, t, env.dsn)
	defer pool.Close()

	// Reuse a fixture captured during Phase 1; the seeded URL is rewritten
	// to the in-stack fixture-server by the collector's --fixture-base flag,
	// so this test never touches real sites.
	const (
		sourceAbbr = "dpp"
		url        = "https://www.dpp.org.tw/media/contents/11553"
		title      = "e2e seed (Phase 6 driver)"
	)
	traceID := "e2e-page-fetch-" + uuid.NewString()
	publishedAt := time.Date(2026, 4, 1, 0, 0, 0, 0, time.UTC)

	candidateID := seedCandidate(ctx, t, pool, sourceAbbr, url, title, publishedAt, traceID)
	t.Logf("seeded candidate %s", candidateID)

	resp := postPageFetch(ctx, t, env.apiBase, []uuid.UUID{candidateID})
	t.Logf("POST /page_fetch → fetch_id=%s items=%+v", resp.FetchID, resp.Items)
	if len(resp.Items) != 1 {
		t.Fatalf("expected 1 item, got %d", len(resp.Items))
	}
	if resp.Items[0].CandidateID != candidateID {
		t.Errorf("item candidate_id mismatch: got=%s want=%s", resp.Items[0].CandidateID, candidateID)
	}
	// Status may be "created" on a fresh DB or "already_complete" on a
	// repeat run against the same fixture; both are valid terminal paths.
	switch resp.Items[0].Status {
	case "created", "already_complete":
	default:
		t.Errorf("unexpected status %q", resp.Items[0].Status)
	}

	final := pollFetch(ctx, t, env.apiBase, resp.FetchID, 30*time.Second, 1*time.Second)
	t.Logf("terminal progress=%+v", final)

	if final.Total != 1 {
		t.Errorf("total: got=%d want=1", final.Total)
	}
	if final.Failed.Count != 0 {
		t.Errorf("failed: got=%d want=0", final.Failed.Count)
	}
	if final.Completed.Count+final.AlreadyComplete.Count != 1 {
		t.Errorf("completed+already_complete: got=%d want=1", final.Completed.Count+final.AlreadyComplete.Count)
	}

	assertContent(ctx, t, pool, candidateID)
}
