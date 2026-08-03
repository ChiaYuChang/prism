package api_test

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/ChiaYuChang/prism/internal/http/api"
	"github.com/ChiaYuChang/prism/internal/infra/natsadmin"
	"github.com/stretchr/testify/require"
)

type fakeNATSAdmin struct {
	manifest natsadmin.Manifest
	options  natsadmin.ApplyOptions
	paused   natsadmin.ConsumerRef
}

func (f *fakeNATSAdmin) Apply(_ context.Context, manifest natsadmin.Manifest, opts natsadmin.ApplyOptions) (natsadmin.ApplyReport, error) {
	f.manifest, f.options = manifest, opts
	return natsadmin.ApplyReport{Created: []string{"stream/prism_task"}}, nil
}

func (f *fakeNATSAdmin) PauseConsumer(_ context.Context, ref natsadmin.ConsumerRef, req natsadmin.PauseRequest) (natsadmin.PauseResult, error) {
	f.paused = ref
	return natsadmin.PauseResult{Stream: ref.Stream, Consumer: ref.Consumer, Paused: true, PauseUntil: &req.Until}, nil
}

func (*fakeNATSAdmin) ResumeConsumer(_ context.Context, ref natsadmin.ConsumerRef) (natsadmin.PauseResult, error) {
	return natsadmin.PauseResult{Stream: ref.Stream, Consumer: ref.Consumer}, nil
}
func (*fakeNATSAdmin) DeleteConsumer(context.Context, natsadmin.ConsumerRef) error { return nil }
func (*fakeNATSAdmin) DeleteStream(context.Context, string) error                  { return nil }
func (*fakeNATSAdmin) PurgeStream(context.Context, string) error                   { return nil }
func (*fakeNATSAdmin) PublishTestMessage(_ context.Context, subject string) (natsadmin.TestMessageResult, error) {
	return natsadmin.TestMessageResult{Stream: "prism_test", Subject: subject, Sequence: 1}, nil
}

func TestApplyAdminNATS(t *testing.T) {
	srv, _ := newTestServer(t)
	admin := &fakeNATSAdmin{}
	srv.NATSAdmin = admin
	body := map[string]any{
		"dry_run": true,
		"manifest": map[string]any{
			"version":   1,
			"streams":   []map[string]any{{"name": "prism_task", "subjects": []string{"prism_task"}, "storage": "file", "retention": "limits", "replicas": 1}},
			"consumers": []any{},
		},
	}
	data, err := json.Marshal(body)
	require.NoError(t, err)
	rec := httptest.NewRecorder()
	srv.ApplyAdminNATS(rec, httptest.NewRequest(http.MethodPost, "/api/v1/admin/nats/apply", bytes.NewReader(data)))
	require.Equal(t, http.StatusOK, rec.Code)
	require.True(t, admin.options.DryRun)
	require.Len(t, admin.manifest.Streams, 1)
}

func TestPauseAdminNATSConsumer(t *testing.T) {
	srv, _ := newTestServer(t)
	admin := &fakeNATSAdmin{}
	srv.NATSAdmin = admin
	until := time.Now().Add(time.Hour).UTC()
	data, err := json.Marshal(map[string]any{"until": until, "reason": "quota"})
	require.NoError(t, err)
	req := httptest.NewRequest(http.MethodPost, "/api/v1/admin/nats/streams/prism_batch_completed/consumers/planner-worker/pause", bytes.NewReader(data))
	req.SetPathValue("stream", "prism_batch_completed")
	req.SetPathValue("consumer", "planner-worker")
	rec := httptest.NewRecorder()
	srv.PauseAdminNATSConsumer(rec, req)
	require.Equal(t, http.StatusOK, rec.Code)
	require.Equal(t, "planner-worker", admin.paused.Consumer)
}

func TestDeleteAdminNATSStreamRequiresConfirmation(t *testing.T) {
	srv, _ := newTestServer(t)
	srv.NATSAdmin = &fakeNATSAdmin{}
	req := httptest.NewRequest(http.MethodDelete, "/api/v1/admin/nats/streams/prism_task?confirm=wrong", nil)
	req.SetPathValue("stream", "prism_task")
	rec := httptest.NewRecorder()
	srv.DeleteAdminNATSStream(rec, req)
	require.Equal(t, http.StatusBadRequest, rec.Code)
}

var _ api.NATSAdmin = (*fakeNATSAdmin)(nil)
