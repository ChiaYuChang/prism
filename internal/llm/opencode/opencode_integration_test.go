//go:build integration

package opencode

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/ChiaYuChang/prism/internal/llm"
	"github.com/ChiaYuChang/prism/pkg/testutils"
	"github.com/go-playground/mold/v4"
	"github.com/go-playground/validator/v10"
	"github.com/stretchr/testify/require"
	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/wait"
	"go.opentelemetry.io/otel/trace/noop"
)

const (
	opencodeVersion        = "1.17.9"
	opencodeContainerImage = "ghcr.io/anomalyco/opencode:" + opencodeVersion
	opencodeContainerPort  = "4096/tcp"
	opencodeTestUsername   = "opencode"
	authVal                = "test-pass"
	opencodeFreeModel      = "opencode/deepseek-v4-flash-free"
)

type opencodeHealthResponse struct {
	Healthy bool   `json:"healthy"`
	Version string `json:"version"`
}

// TestOpencodeContainer_Generate exercises a real opencode server container.
// It requires Docker, image pull access, and internet access from the container.
//
// Run with:
//
//	rtk go test -tags=integration -count=1 -run TestOpencodeContainer_Generate ./internal/llm/opencode
func TestOpencodeContainer_Generate(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()

	container, err := testcontainers.GenericContainer(ctx, testcontainers.GenericContainerRequest{
		ContainerRequest: testcontainers.ContainerRequest{
			Image:        opencodeContainerImage,
			ExposedPorts: []string{opencodeContainerPort},
			Cmd:          []string{"serve", "--port", "4096", "--hostname", "0.0.0.0"},
			Env: map[string]string{
				"OPENCODE_SERVER_USERNAME": opencodeTestUsername,
				"OPENCODE_SERVER_PASSWORD": authVal,
			},
			WaitingFor: wait.ForListeningPort(opencodeContainerPort),
		},
		Started: true,
	})
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, container.Terminate(context.Background())) })

	host, err := container.Host(ctx)
	require.NoError(t, err)
	mappedPort, err := container.MappedPort(ctx, opencodeContainerPort)
	require.NoError(t, err)
	baseURL := fmt.Sprintf("http://%s:%s", host, mappedPort.Port())

	hc := &http.Client{Timeout: 120 * time.Second}
	require.NoError(t, waitForHealthyOpencode(ctx, hc, baseURL, opencodeTestUsername, authVal))

	provider, err := New(
		ctx,
		slog.New(slog.NewTextHandler(io.Discard, nil)),
		noop.NewTracerProvider().Tracer("opencode-integration"),
		validator.New(),
		mold.New(),
		hc,
		Config{
			BaseURL:  baseURL,
			Agent:    "general",
			Username: opencodeTestUsername,
			Password: authVal,
			Timeout:  120 * time.Second,
		},
	)
	require.NoError(t, err)

	resp, err := provider.Generate(ctx, &llm.GenerateRequest{
		Model:             opencodeFreeModel,
		SystemInstruction: "Reply with one short sentence. Do not use markdown.",
		Prompt:            "Say hello from Prism.",
		Format:            llm.ResponseFormatText,
	})
	require.NoError(t, err)
	require.NotEmpty(t, strings.TrimSpace(resp.Text))

	t.Logf("model=%s text_len=%d total_tokens=%d", resp.Model, len(resp.Text), resp.Usage.Total)
}

func waitForHealthyOpencode(ctx context.Context, hc *http.Client, baseURL, username, password string) error {
	healthURL, err := url.JoinPath(baseURL, "/global/health")
	if err != nil {
		return fmt.Errorf("build health URL: %w", err)
	}

	return testutils.WithExponentialBackoff(ctx, 8, time.Second, func() error {
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, healthURL, nil)
		if err != nil {
			return err
		}
		req.SetBasicAuth(username, password)

		resp, err := hc.Do(req)
		if err != nil {
			return err
		}
		defer func() { _ = resp.Body.Close() }()

		if resp.StatusCode != http.StatusOK {
			return fmt.Errorf("health status %d", resp.StatusCode)
		}

		var health opencodeHealthResponse
		if err := json.NewDecoder(resp.Body).Decode(&health); err != nil {
			return err
		}
		if !health.Healthy || health.Version != opencodeVersion {
			return fmt.Errorf("health healthy=%t version=%q, want healthy=true version=%q",
				health.Healthy, health.Version, opencodeVersion)
		}
		return nil
	})
}
