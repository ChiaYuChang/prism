package api

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"time"

	authtoken "github.com/ChiaYuChang/prism/internal/auth/token"
	"github.com/ChiaYuChang/prism/internal/http/middleware"
	"github.com/ChiaYuChang/prism/internal/repo"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

type AdminToken struct {
	ID            uuid.UUID  `json:"id"`
	Type          string     `json:"type"`
	Name          string     `json:"name"`
	HashAlgorithm string     `json:"hash_algorithm"`
	CreatedAt     time.Time  `json:"created_at"`
	ExpiresAt     time.Time  `json:"expires_at"`
	LastUsedAt    *time.Time `json:"last_used_at,omitempty"`
	RenewedAt     *time.Time `json:"renewed_at,omitempty"`
	RotatedAt     *time.Time `json:"rotated_at,omitempty"`
	RevokedAt     *time.Time `json:"revoked_at,omitempty"`
}

type AdminTokenSecretResponse struct {
	AdminToken
	Token string `json:"token"`
}

type AdminListTokensResponse struct {
	Items []AdminToken `json:"items"`
	Limit int32        `json:"limit"`
	Next  int32        `json:"next"`
	Count int          `json:"count"`
}

type CreateTokenRequest struct {
	Type      string     `json:"type"`
	Name      string     `json:"name"`
	ExpiresAt *time.Time `json:"expires_at,omitempty"`
}

type TokenExpiryRequest struct {
	ExpiresAt *time.Time `json:"expires_at,omitempty"`
}

// CreateToken handles token creation for CLI/shared admin flows.
func (s *Server) CreateToken(w http.ResponseWriter, r *http.Request) {
	var req CreateTokenRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid payload")
		return
	}
	token, raw, err := s.createToken(r.Context(), req.Type, req.Name, req.ExpiresAt)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, toAdminTokenSecret(token, raw))
}

// ListTokens handles GET /api/v1/admin/tokens.
//
// @Summary   List admin tokens
// @Tags      admin
// @Produce   json
// @Param     limit query int false "Page size (default 50, max 500)"
// @Param     next  query int false "Cursor for next page (default 1)"
// @Success   200 {object} AdminListTokensResponse
// @Failure   400 {object} ErrorResponse
// @Failure   500 {object} ErrorResponse
// @Router    /admin/tokens [get]
func (s *Server) ListTokens(w http.ResponseWriter, r *http.Request) {
	params, ok := parseAdminListParams(w, r)
	if !ok {
		return
	}
	rows, err := s.Tokens.ListTokens(r.Context(), params)
	if err != nil {
		s.Logger.ErrorContext(r.Context(), "list tokens failed", slog.Any("error", err))
		writeError(w, http.StatusInternalServerError, "failed to list tokens")
		return
	}
	items := make([]AdminToken, 0, len(rows))
	for _, row := range rows {
		items = append(items, toAdminToken(row))
	}
	writeJSON(w, http.StatusOK, AdminListTokensResponse{Items: items, Limit: params.Limit, Next: params.Next, Count: len(items)})
}

// GetToken handles GET /api/v1/admin/tokens/{id}.
//
// @Summary   Get admin token
// @Tags      admin
// @Produce   json
// @Param     id path string true "Token UUID"
// @Success   200 {object} AdminToken
// @Failure   400 {object} ErrorResponse
// @Failure   404 {object} ErrorResponse
// @Failure   500 {object} ErrorResponse
// @Router    /admin/tokens/{id} [get]
func (s *Server) GetToken(w http.ResponseWriter, r *http.Request) {
	token, ok := s.tokenByPathID(w, r)
	if !ok {
		return
	}
	writeJSON(w, http.StatusOK, toAdminToken(token))
}

func (s *Server) RenewToken(w http.ResponseWriter, r *http.Request) {
	token, ok := s.tokenByPathID(w, r)
	if !ok {
		return
	}
	principal, authenticated := middleware.PrincipalFromContext(r.Context())
	if !authenticated || !canRenew(principal, token) {
		writeError(w, http.StatusForbidden, "forbidden")
		return
	}
	var req TokenExpiryRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid payload")
		return
	}
	expiresAt, err := s.resolveTokenExpiry(token.Type, req.ExpiresAt)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	renewed, err := s.Tokens.RenewToken(r.Context(), token.ID, expiresAt)
	if err != nil {
		s.writeTokenRepoError(w, r, "renew token", err)
		return
	}
	writeJSON(w, http.StatusOK, toAdminToken(renewed))
}

func (s *Server) RotateToken(w http.ResponseWriter, r *http.Request) {
	token, ok := s.tokenByPathID(w, r)
	if !ok {
		return
	}
	var req TokenExpiryRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid payload")
		return
	}
	expiresAt, err := s.resolveTokenExpiry(token.Type, req.ExpiresAt)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	raw, secret, err := authtoken.Generate(authtoken.Type(token.Type), token.ID)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	rotated, err := s.Tokens.RotateToken(r.Context(), repo.RotateTokenParams{
		ID:            token.ID,
		HashAlgorithm: s.TokenHasher.Algorithm(),
		TokenHash:     s.TokenHasher.Hash(secret),
		ExpiresAt:     expiresAt,
	})
	if err != nil {
		s.writeTokenRepoError(w, r, "rotate token", err)
		return
	}
	writeJSON(w, http.StatusOK, toAdminTokenSecret(rotated, raw))
}

func (s *Server) RevokeToken(w http.ResponseWriter, r *http.Request) {
	token, ok := s.tokenByPathID(w, r)
	if !ok {
		return
	}
	revoked, err := s.Tokens.RevokeToken(r.Context(), token.ID)
	if err != nil {
		s.writeTokenRepoError(w, r, "revoke token", err)
		return
	}
	writeJSON(w, http.StatusOK, toAdminToken(revoked))
}

func (s *Server) createToken(ctx context.Context, tokenType, name string, rawExpiresAt *time.Time) (repo.Token, string, error) {
	if s.Tokens == nil || s.TokenHasher == nil {
		return repo.Token{}, "", errors.New("token repository unavailable")
	}
	if name == "" {
		return repo.Token{}, "", errors.New("token name is required")
	}
	expiresAt, err := s.resolveTokenExpiry(tokenType, rawExpiresAt)
	if err != nil {
		return repo.Token{}, "", err
	}
	id := uuid.Must(uuid.NewV7())
	raw, secret, err := authtoken.Generate(authtoken.Type(tokenType), id)
	if err != nil {
		return repo.Token{}, "", err
	}
	created, err := s.Tokens.CreateToken(ctx, repo.CreateTokenParams{
		ID:            id,
		Type:          tokenType,
		Name:          name,
		HashAlgorithm: s.TokenHasher.Algorithm(),
		TokenHash:     s.TokenHasher.Hash(secret),
		ExpiresAt:     expiresAt,
	})
	if err != nil {
		return repo.Token{}, "", err
	}
	return created, raw, nil
}

func (s *Server) resolveTokenExpiry(tokenType string, raw *time.Time) (time.Time, error) {
	cfg, ok := s.TokenTypes[tokenType]
	if !ok {
		return time.Time{}, errors.New("unsupported token type")
	}
	now := time.Now()
	expiresAt := now.Add(cfg.DefaultTTL)
	if raw != nil {
		expiresAt = *raw
	}
	if !expiresAt.After(now) {
		return time.Time{}, errors.New("expires_at must be in the future")
	}
	if expiresAt.After(now.Add(cfg.MaxTTL)) {
		return time.Time{}, errors.New("expires_at exceeds max ttl")
	}
	return expiresAt, nil
}

func (s *Server) tokenByPathID(w http.ResponseWriter, r *http.Request) (repo.Token, bool) {
	id, err := uuid.Parse(r.PathValue("id"))
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid token id")
		return repo.Token{}, false
	}
	token, err := s.Tokens.GetTokenByID(r.Context(), id)
	if err != nil {
		s.writeTokenRepoError(w, r, "get token", err)
		return repo.Token{}, false
	}
	return token, true
}

func (s *Server) writeTokenRepoError(w http.ResponseWriter, r *http.Request, msg string, err error) {
	if errors.Is(err, pgx.ErrNoRows) {
		writeError(w, http.StatusNotFound, "token not found")
		return
	}
	s.Logger.ErrorContext(r.Context(), msg+" failed", slog.Any("error", err))
	writeError(w, http.StatusInternalServerError, "failed to "+msg)
}

func canRenew(principal middleware.Principal, token repo.Token) bool {
	if principal.Type == authtoken.TypeAdmin {
		return true
	}
	if token.RevokedAt != nil || !time.Now().Before(token.ExpiresAt) {
		return false
	}
	return principal.TokenID == token.ID && string(principal.Type) == token.Type && (principal.Type == authtoken.TypeUser || principal.Type == authtoken.TypeWorker)
}

func (s *Server) requireAdmin(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		principal, ok := middleware.PrincipalFromContext(r.Context())
		if !ok || principal.Type != authtoken.TypeAdmin {
			writeError(w, http.StatusForbidden, "forbidden")
			return
		}
		next.ServeHTTP(w, r)
	})
}

func toAdminToken(token repo.Token) AdminToken {
	return AdminToken{
		ID:            token.ID,
		Type:          token.Type,
		Name:          token.Name,
		HashAlgorithm: token.HashAlgorithm,
		CreatedAt:     token.CreatedAt,
		ExpiresAt:     token.ExpiresAt,
		LastUsedAt:    token.LastUsedAt,
		RenewedAt:     token.RenewedAt,
		RotatedAt:     token.RotatedAt,
		RevokedAt:     token.RevokedAt,
	}
}

func toAdminTokenSecret(token repo.Token, raw string) AdminTokenSecretResponse {
	return AdminTokenSecretResponse{AdminToken: toAdminToken(token), Token: raw}
}
