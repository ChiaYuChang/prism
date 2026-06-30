package api

import (
	"crypto/sha256"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"github.com/ChiaYuChang/prism/internal/repo"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

const maxPromptUploadBytes = 1 << 20

var promptKeyPattern = regexp.MustCompile(`^[a-z0-9][a-z0-9_-]*(/[a-z0-9][a-z0-9_-]*){1,8}$`)

type AdminPromptVersion struct {
	ID        uuid.UUID `json:"id"`
	KeyID     uuid.UUID `json:"key_id"`
	Key       string    `json:"key"`
	Version   int32     `json:"version"`
	Hash      string    `json:"hash"`
	Path      string    `json:"path"`
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
// @Param     key  formData string true  "Prompt key, e.g. worker/planner/analysis/extractor"
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

	key := strings.TrimSpace(r.FormValue("key"))
	if !validPromptKey(key) {
		writeError(w, http.StatusBadRequest, "invalid prompt key")
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

	path, err := s.writePromptObject(key, hash, body)
	if err != nil {
		s.Logger.ErrorContext(r.Context(), "write prompt object failed", slog.String("key", key), slog.Any("error", err))
		writeError(w, http.StatusInternalServerError, "failed to store prompt file")
		return
	}

	prompt, err := prompts.CreatePromptVersion(r.Context(), repo.CreatePromptVersionParams{
		Key:       key,
		Hash:      hash,
		Path:      path,
		SizeBytes: int64(len(body)),
	})
	if err != nil {
		s.Logger.ErrorContext(r.Context(), "create prompt version failed", slog.String("key", key), slog.Any("error", err))
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
// @Param     key   query string false "Prompt key"
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
	key := strings.TrimSpace(r.URL.Query().Get("key"))
	var (
		rows []repo.PromptVersion
		err  error
	)
	if key == "" {
		rows, err = prompts.ListPromptVersions(r.Context(), params)
	} else {
		if !validPromptKey(key) {
			writeError(w, http.StatusBadRequest, "invalid prompt key")
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

func (s *Server) writePromptObject(key string, hash string, body []byte) (string, error) {
	hexHash := strings.TrimPrefix(hash, "sha256:")
	dir := filepath.Join(append([]string{s.PromptRoot}, strings.Split(key, "/")...)...)
	path := filepath.Join(dir, hexHash+".md")

	if existing, err := os.ReadFile(path); err == nil {
		if promptHash(existing) != hash {
			return "", fmt.Errorf("prompt object hash collision at %s", path)
		}
		return path, nil
	} else if !errors.Is(err, os.ErrNotExist) {
		return "", err
	}

	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", err
	}
	tmp, err := os.CreateTemp(dir, ".prompt-*.tmp")
	if err != nil {
		return "", err
	}
	tmpName := tmp.Name()
	defer func() { _ = os.Remove(tmpName) }()
	if _, err := tmp.Write(body); err != nil {
		_ = tmp.Close()
		return "", err
	}
	if err := tmp.Close(); err != nil {
		return "", err
	}
	if err := os.Rename(tmpName, path); err != nil {
		return "", err
	}
	return path, nil
}

func validPromptKey(key string) bool {
	return promptKeyPattern.MatchString(key) && !strings.Contains(key, "..")
}

func promptHash(body []byte) string {
	sum := sha256.Sum256(body)
	return fmt.Sprintf("sha256:%x", sum[:])
}

func toAdminPromptVersion(prompt repo.PromptVersion) AdminPromptVersion {
	return AdminPromptVersion{
		ID:        prompt.ID,
		KeyID:     prompt.KeyID,
		Key:       prompt.Key,
		Version:   prompt.Version,
		Hash:      prompt.Hash,
		Path:      prompt.Path,
		SizeBytes: prompt.SizeBytes,
		CreatedAt: prompt.CreatedAt,
	}
}
