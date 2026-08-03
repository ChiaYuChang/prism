package api

import (
	"bytes"
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"regexp"
	"strings"
	"time"

	"github.com/ChiaYuChang/prism/internal/prompt"
	"github.com/ChiaYuChang/prism/internal/repo"
	"github.com/ChiaYuChang/prism/internal/storage"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

const maxPromptUploadBytes = 1 << 20

var promptKeyPattern = regexp.MustCompile(`^[a-z0-9][a-z0-9_-]*(/[a-z0-9][a-z0-9_-]*){1,8}$`)

type AdminPromptVersion struct {
	ID        uuid.UUID `json:"id"`
	Name      string    `json:"name"`
	Version   int32     `json:"version"`
	Hash      string    `json:"hash"`
	SizeBytes int64     `json:"size_bytes"`
	CreatedAt time.Time `json:"created_at"`
}

type AdminListPromptVersionsResponse struct {
	Items []AdminPromptVersion `json:"items"`
	Limit int32                `json:"limit"`
	Next  int32                `json:"next"`
	Count int                  `json:"count"`
}

// CreatePromptVersion handles POST /api/v1/admin/prompts.
//
// @Summary   Upload operator prompt version
// @Tags      admin
// @Accept    multipart/form-data
// @Produce   json
// @Param     name formData string true  "Prompt name, e.g. worker/planner/analysis/extractor"
// @Param     hash formData string false "Expected SHA-256 hash, formatted as sha256:<hex>"
// @Param     file formData file   true  "Prompt markdown file"
// @Success   200 {object} AdminPromptVersion
// @Failure   400 {object} ErrorResponse
// @Failure   500 {object} ErrorResponse
func (s *Server) CreatePromptVersion(w http.ResponseWriter, r *http.Request) {
	prompts := s.promptsOrError(w)
	if prompts == nil {
		return
	}

	r.Body = http.MaxBytesReader(w, r.Body, maxPromptUploadBytes)
	if err := r.ParseMultipartForm(maxPromptUploadBytes); err != nil {
		writeError(w, http.StatusBadRequest, "invalid multipart prompt upload")
		return
	}

	name := normalizePromptName(r.FormValue("name"))
	if !validPromptName(name) {
		writeError(w, http.StatusBadRequest, "invalid prompt name")
		return
	}

	file, _, err := r.FormFile("file")
	if err != nil {
		writeError(w, http.StatusBadRequest, "prompt file is required")
		return
	}
	defer func() { _ = file.Close() }()

	body, err := io.ReadAll(file)
	if err != nil {
		writeError(w, http.StatusBadRequest, "failed to read prompt file")
		return
	}
	if len(strings.TrimSpace(string(body))) == 0 {
		writeError(w, http.StatusBadRequest, "prompt file is empty")
		return
	}

	hash := promptHash(body)
	if expected := strings.TrimSpace(r.FormValue("hash")); expected != "" && expected != hash {
		writeError(w, http.StatusBadRequest, "prompt hash mismatch")
		return
	}

	err = s.writePromptObject(r.Context(), hash, body)
	if err != nil {
		s.Logger.ErrorContext(r.Context(), "write prompt object failed", slog.String("name", name), slog.Any("error", err))
		writeError(w, http.StatusInternalServerError, "failed to store prompt file")
		return
	}

	prompt, err := prompts.CreatePromptVersion(r.Context(), repo.CreatePromptVersionParams{
		Name:      name,
		Hash:      hash,
		SizeBytes: int64(len(body)),
	})
	if err != nil {
		s.Logger.ErrorContext(r.Context(), "create prompt version failed", slog.String("name", name), slog.Any("error", err))
		writeError(w, http.StatusInternalServerError, "failed to create prompt version")
		return
	}
	writeJSON(w, http.StatusOK, toAdminPromptVersion(prompt))
}

// GetPromptVersion handles GET /api/v1/admin/prompts/{id}.
//
// @Summary   Get operator prompt version
// @Tags      admin
// @Produce   json
// @Param     id path string true "Prompt version UUID"
// @Success   200 {object} AdminPromptVersion
// @Failure   400 {object} ErrorResponse
// @Failure   404 {object} ErrorResponse
// @Failure   500 {object} ErrorResponse
// @Router    /admin/prompts/{id} [get]
func (s *Server) GetPromptVersion(w http.ResponseWriter, r *http.Request) {
	prompts := s.promptsOrError(w)
	if prompts == nil {
		return
	}
	id, err := uuid.Parse(r.PathValue("id"))
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid prompt id")
		return
	}
	prompt, err := prompts.GetPromptVersionByID(r.Context(), id)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			writeError(w, http.StatusNotFound, "prompt not found")
			return
		}
		s.Logger.ErrorContext(r.Context(), "get prompt version failed", slog.String("prompt_id", id.String()), slog.Any("error", err))
		writeError(w, http.StatusInternalServerError, "failed to load prompt")
		return
	}
	writeJSON(w, http.StatusOK, toAdminPromptVersion(prompt))
}

// ListPromptVersions handles GET /api/v1/admin/prompts.
//
// @Summary   List operator prompt versions
// @Tags      admin
// @Produce   json
// @Param     name  query string false "Prompt name"
// @Param     limit query int    false "Page size (default 50, max 500)"
// @Param     next  query int    false "Cursor for next page (default 1)"
// @Success   200 {object} AdminListPromptVersionsResponse
// @Failure   400 {object} ErrorResponse
// @Failure   500 {object} ErrorResponse
// @Router    /admin/prompts [get]
func (s *Server) ListPromptVersions(w http.ResponseWriter, r *http.Request) {
	prompts := s.promptsOrError(w)
	if prompts == nil {
		return
	}
	params, ok := parseAdminListParams(w, r)
	if !ok {
		return
	}
	key := normalizePromptName(r.URL.Query().Get("name"))
	var (
		rows []repo.PromptVersion
		err  error
	)
	if key == "" {
		rows, err = prompts.ListPromptVersions(r.Context(), params)
	} else {
		if !validPromptName(key) {
			writeError(w, http.StatusBadRequest, "invalid prompt name")
			return
		}
		rows, err = prompts.ListPromptVersionsByKey(r.Context(), key, params)
	}
	if err != nil {
		s.Logger.ErrorContext(r.Context(), "list prompt versions failed", slog.Any("error", err))
		writeError(w, http.StatusInternalServerError, "failed to list prompts")
		return
	}
	items := make([]AdminPromptVersion, 0, len(rows))
	for _, row := range rows {
		items = append(items, toAdminPromptVersion(row))
	}
	writeJSON(w, http.StatusOK, AdminListPromptVersionsResponse{Items: items, Limit: params.Limit, Next: params.Next, Count: len(items)})
}

func (s *Server) promptsOrError(w http.ResponseWriter) repo.Prompts {
	if s.Prompts == nil {
		writeError(w, http.StatusInternalServerError, "prompt repository unavailable")
		return nil
	}
	return s.Prompts
}

func (s *Server) writePromptObject(ctx context.Context, hash string, body []byte) error {
	if s.PromptStore == nil {
		return fmt.Errorf("prompt storage is unavailable")
	}
	return s.PromptStore.Put(ctx, prompt.ObjectKey(hash), bytes.NewReader(body), storage.PutOptions{ContentType: "text/markdown"})
}

func normalizePromptName(name string) string {
	return strings.ReplaceAll(strings.Trim(strings.TrimSpace(name), "."), "/", ".")
}

func validPromptName(name string) bool {
	return promptKeyPattern.MatchString(strings.ReplaceAll(name, ".", "/")) && !strings.Contains(name, "..")
}

func promptHash(body []byte) string {
	sum := sha256.Sum256(body)
	return fmt.Sprintf("sha256:%x", sum[:])
}

func toAdminPromptVersion(prompt repo.PromptVersion) AdminPromptVersion {
	return AdminPromptVersion{
		ID:        prompt.ID,
		Name:      prompt.Name,
		Version:   prompt.Version,
		Hash:      prompt.Hash,
		SizeBytes: prompt.SizeBytes,
		CreatedAt: prompt.CreatedAt,
	}
}
