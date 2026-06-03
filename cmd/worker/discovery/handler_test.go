package main

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"testing"

	"github.com/ChiaYuChang/prism/internal/discovery"
	discoverymocks "github.com/ChiaYuChang/prism/internal/discovery/mocks"
	"github.com/ChiaYuChang/prism/internal/discovery/planner"
	discoverysink "github.com/ChiaYuChang/prism/internal/discovery/sink"
	"github.com/ChiaYuChang/prism/internal/message"
	"github.com/ChiaYuChang/prism/internal/model"
	"github.com/ChiaYuChang/prism/internal/repo"
	repomocks "github.com/ChiaYuChang/prism/internal/repo/mocks"
	wm "github.com/ThreeDotsLabs/watermill/message"
	"github.com/google/uuid"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
	"go.opentelemetry.io/otel/trace/noop"
)

func TestHandlerHandleMessageCompletesTask(t *testing.T) {
	taskID := uuid.Must(uuid.NewV7())
	batchID := uuid.Must(uuid.NewV7())

	scout := discoverymocks.NewMockScout(t)
	scoutRepo := repomocks.NewMockScout(t)
	scheduler := repomocks.NewMockScheduler(t)
	sink := &stubCandidateSink{}

	h, err := NewHandler(testLogger(), noop.NewTracerProvider().Tracer("test"), scout, nil, sink, scoutRepo, scheduler)
	require.NoError(t, err)

	source := repo.Source{
		Abbr:    "dpp",
		Type:    repo.SourceTypeParty,
		BaseURL: "https://www.dpp.org.tw",
	}
	candidates := []model.Candidates{{Title: "A", URL: "https://www.dpp.org.tw/article/1"}}

	scoutRepo.EXPECT().GetSourceByAbbr(mock.Anything, "dpp").Return(source, nil)
	scout.EXPECT().Discover(mock.Anything, "https://www.dpp.org.tw/media/00").Return(candidates, nil)
	scheduler.EXPECT().CompleteTask(mock.Anything, taskID).Return(nil)

	payload, err := (&message.TaskSignal{
		TaskID:     taskID,
		BatchID:    batchID,
		Kind:       repo.TaskKindDirectoryFetch,
		SourceType: repo.SourceTypeParty,
		SourceAbbr: "dpp",
		URL:        "https://www.dpp.org.tw/media/00",
		TraceID:    "trace-123",
	}).Marshal()
	require.NoError(t, err)

	ack, err := h.HandleMessage(context.Background(), wm.NewMessage("id", payload))
	require.NoError(t, err)
	require.True(t, ack)
	require.NotNil(t, sink.last)
	require.Equal(t, "dpp", sink.last.SourceAbbr)
	require.Equal(t, repo.SourceTypeParty, sink.last.SourceType)
	require.Equal(t, "DIRECTORY", sink.last.IngestionMethod)
	require.Equal(t, batchID, sink.last.BatchID)
}

func TestHandlerHandleMessageIgnoresUnsupportedTask(t *testing.T) {
	taskID := uuid.Must(uuid.NewV7())

	scout := discoverymocks.NewMockScout(t)
	scoutRepo := repomocks.NewMockScout(t)
	scheduler := repomocks.NewMockScheduler(t)
	sink := &stubCandidateSink{}

	h, err := NewHandler(testLogger(), noop.NewTracerProvider().Tracer("test"), scout, nil, sink, scoutRepo, scheduler)
	require.NoError(t, err)

	payload, err := (&message.TaskSignal{
		TaskID:     taskID,
		BatchID:    uuid.Must(uuid.NewV7()),
		Kind:       "PAGE_FETCH",
		SourceType: repo.SourceTypeParty,
		SourceAbbr: "dpp",
		URL:        "https://www.dpp.org.tw/media/00",
		TraceID:    "trace-123",
	}).Marshal()
	require.NoError(t, err)

	ack, err := h.HandleMessage(context.Background(), wm.NewMessage("id", payload))
	require.NoError(t, err)
	require.True(t, ack)
	require.Nil(t, sink.last)
}

func TestHandlerHandleMessageNacksWhenCompleteFails(t *testing.T) {
	taskID := uuid.Must(uuid.NewV7())
	batchID := uuid.Must(uuid.NewV7())

	scout := discoverymocks.NewMockScout(t)
	scoutRepo := repomocks.NewMockScout(t)
	scheduler := repomocks.NewMockScheduler(t)
	sink := &stubCandidateSink{}

	h, err := NewHandler(testLogger(), noop.NewTracerProvider().Tracer("test"), scout, nil, sink, scoutRepo, scheduler)
	require.NoError(t, err)

	source := repo.Source{
		Abbr:    "dpp",
		Type:    repo.SourceTypeParty,
		BaseURL: "https://www.dpp.org.tw",
	}
	scoutRepo.EXPECT().GetSourceByAbbr(mock.Anything, "dpp").Return(source, nil)
	scout.EXPECT().Discover(mock.Anything, "https://www.dpp.org.tw/media/00").Return([]model.Candidates{}, nil)
	scheduler.EXPECT().CompleteTask(mock.Anything, taskID).Return(errors.New("db down"))

	payload, err := (&message.TaskSignal{
		TaskID:     taskID,
		BatchID:    batchID,
		Kind:       repo.TaskKindDirectoryFetch,
		SourceType: repo.SourceTypeParty,
		SourceAbbr: "dpp",
		URL:        "https://www.dpp.org.tw/media/00",
		TraceID:    "trace-123",
	}).Marshal()
	require.NoError(t, err)

	ack, err := h.HandleMessage(context.Background(), wm.NewMessage("id", payload))
	require.Error(t, err)
	require.False(t, ack)
}

type stubCandidateSink struct {
	last *discoverysink.CandidateSinkRequest
	err  error
}

func (s *stubCandidateSink) Handle(_ context.Context, req discoverysink.CandidateSinkRequest) error {
	s.last = &req
	return s.err
}

func TestHandlerHandleMessageKeywordSearch(t *testing.T) {
	taskID := uuid.Must(uuid.NewV7())
	batchID := uuid.Must(uuid.NewV7())

	scout := discoverymocks.NewMockScout(t)
	searchClient := discoverymocks.NewMockSearchClient(t)
	scoutRepo := repomocks.NewMockScout(t)
	scheduler := repomocks.NewMockScheduler(t)
	sink := &stubCandidateSink{}

	searchProviders := map[string]discovery.SearchClient{
		"brave": searchClient,
	}

	h, err := NewHandler(testLogger(), noop.NewTracerProvider().Tracer("test"), scout, searchProviders, sink, scoutRepo, scheduler)
	require.NoError(t, err)

	searchClient.EXPECT().
		DiscoverNews(mock.Anything, "台灣半導體政策", "cna.com.tw").
		Return([]model.Candidates{
			{Title: "TSMC expands", URL: "https://example.com/tsmc"},
			{Title: "Chip policy", URL: "https://example.com/chip"},
		}, nil)
	scheduler.EXPECT().CompleteTask(mock.Anything, taskID).Return(nil)

	payloadBytes, err := json.Marshal(planner.MediaTaskPayload{
		Query: "台灣半導體政策",
		Site:  "cna.com.tw",
	})
	require.NoError(t, err)

	sigPayload, err := (&message.TaskSignal{
		TaskID:     taskID,
		BatchID:    batchID,
		Kind:       repo.TaskKindKeywordSearch,
		SourceType: repo.SourceTypeMedia,
		SourceAbbr: "yahoo",
		URL:        "https://tw.news.yahoo.com",
		Payload:    payloadBytes,
		TraceID:    "trace-kw-123",
	}).Marshal()
	require.NoError(t, err)

	ack, err := h.HandleMessage(context.Background(), wm.NewMessage("id", sigPayload))
	require.NoError(t, err)
	require.True(t, ack)
	require.NotNil(t, sink.last)
	require.Equal(t, "yahoo", sink.last.SourceAbbr)
	require.Equal(t, repo.SourceTypeMedia, sink.last.SourceType)
	require.Equal(t, "SEARCH", sink.last.IngestionMethod)
	require.Equal(t, batchID, sink.last.BatchID)
	require.Len(t, sink.last.Candidates, 2)
	require.Equal(t, "TSMC expands", sink.last.Candidates[0].Title)
	require.Equal(t, "brave", sink.last.Candidates[0].Metadata["search_provider"])
}

func TestHandlerHandleMessageKeywordSearchNoProviders(t *testing.T) {
	taskID := uuid.Must(uuid.NewV7())

	scout := discoverymocks.NewMockScout(t)
	scoutRepo := repomocks.NewMockScout(t)
	scheduler := repomocks.NewMockScheduler(t)
	sink := &stubCandidateSink{}

	h, err := NewHandler(testLogger(), noop.NewTracerProvider().Tracer("test"), scout, nil, sink, scoutRepo, scheduler)
	require.NoError(t, err)

	payloadBytes, err := json.Marshal(planner.MediaTaskPayload{Query: "test"})
	require.NoError(t, err)

	sigPayload, err := (&message.TaskSignal{
		TaskID:     taskID,
		BatchID:    uuid.Must(uuid.NewV7()),
		Kind:       repo.TaskKindKeywordSearch,
		SourceType: repo.SourceTypeMedia,
		SourceAbbr: "unknown-source",
		URL:        "https://example.com",
		Payload:    payloadBytes,
		TraceID:    "trace-kw-404",
	}).Marshal()
	require.NoError(t, err)

	scheduler.EXPECT().FailTask(mock.Anything, taskID).Return(nil)

	ack, err := h.HandleMessage(context.Background(), wm.NewMessage("id", sigPayload))
	require.Error(t, err)
	require.True(t, ack)
}

func TestHandlerHandleMessageKeywordSearchCompletesWhenOneProviderSucceeds(t *testing.T) {
	taskID := uuid.Must(uuid.NewV7())
	batchID := uuid.Must(uuid.NewV7())

	scout := discoverymocks.NewMockScout(t)
	braveClient := discoverymocks.NewMockSearchClient(t)
	googleClient := discoverymocks.NewMockSearchClient(t)
	scoutRepo := repomocks.NewMockScout(t)
	scheduler := repomocks.NewMockScheduler(t)
	sink := &stubCandidateSink{}

	h, err := NewHandler(testLogger(), noop.NewTracerProvider().Tracer("test"), scout, map[string]discovery.SearchClient{
		"brave":      braveClient,
		"google-cse": googleClient,
	}, sink, scoutRepo, scheduler)
	require.NoError(t, err)

	braveClient.EXPECT().DiscoverNews(mock.Anything, "台灣半導體政策", "tw.news.yahoo.com").Return(nil, errors.New("rate limited"))
	googleClient.EXPECT().DiscoverNews(mock.Anything, "台灣半導體政策", "tw.news.yahoo.com").Return([]model.Candidates{
		{Title: "Yahoo article", URL: "https://tw.news.yahoo.com/a"},
	}, nil)
	scheduler.EXPECT().CompleteTask(mock.Anything, taskID).Return(nil)

	payloadBytes, err := json.Marshal(planner.MediaTaskPayload{Query: "台灣半導體政策", Site: "tw.news.yahoo.com"})
	require.NoError(t, err)
	sigPayload, err := (&message.TaskSignal{
		TaskID:     taskID,
		BatchID:    batchID,
		Kind:       repo.TaskKindKeywordSearch,
		SourceType: repo.SourceTypeMedia,
		SourceAbbr: "yahoo",
		URL:        "https://tw.news.yahoo.com",
		Payload:    payloadBytes,
		TraceID:    "trace-kw-123",
	}).Marshal()
	require.NoError(t, err)

	ack, err := h.HandleMessage(context.Background(), wm.NewMessage("id", sigPayload))
	require.NoError(t, err)
	require.True(t, ack)
	require.NotNil(t, sink.last)
	require.Equal(t, "yahoo", sink.last.SourceAbbr)
	require.Len(t, sink.last.Candidates, 1)
	require.Equal(t, "google-cse", sink.last.Candidates[0].Metadata["search_provider"])
}

func TestHandlerHandleMessageDirectoryFetchMedia(t *testing.T) {
	taskID := uuid.Must(uuid.NewV7())
	batchID := uuid.Must(uuid.NewV7())

	scout := discoverymocks.NewMockScout(t)
	scoutRepo := repomocks.NewMockScout(t)
	scheduler := repomocks.NewMockScheduler(t)
	sink := &stubCandidateSink{}

	h, err := NewHandler(testLogger(), noop.NewTracerProvider().Tracer("test"), scout, nil, sink, scoutRepo, scheduler)
	require.NoError(t, err)

	source := repo.Source{Abbr: "cna", Type: repo.SourceTypeMedia, BaseURL: "https://www.cna.com.tw"}
	scoutRepo.EXPECT().GetSourceByAbbr(mock.Anything, "cna").Return(source, nil)
	scout.EXPECT().Discover(mock.Anything, "https://www.cna.com.tw/rss/aipl.xml").Return([]model.Candidates{
		{Title: "CNA article", URL: "https://www.cna.com.tw/news/aipl/1.aspx"},
	}, nil)
	scheduler.EXPECT().CompleteTask(mock.Anything, taskID).Return(nil)

	payload, err := (&message.TaskSignal{
		TaskID:     taskID,
		BatchID:    batchID,
		Kind:       repo.TaskKindDirectoryFetch,
		SourceType: repo.SourceTypeMedia,
		SourceAbbr: "cna",
		URL:        "https://www.cna.com.tw/rss/aipl.xml",
		TraceID:    "trace-media-dir",
	}).Marshal()
	require.NoError(t, err)

	ack, err := h.HandleMessage(context.Background(), wm.NewMessage("id", payload))
	require.NoError(t, err)
	require.True(t, ack)
	require.NotNil(t, sink.last)
	require.Equal(t, "cna", sink.last.SourceAbbr)
	require.Equal(t, repo.SourceTypeMedia, sink.last.SourceType)
	require.Equal(t, repo.IngestionMethodDirectory, sink.last.IngestionMethod)
}

func testLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}
