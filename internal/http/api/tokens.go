package api

import (
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"time"

	prismauth "github.com/ChiaYuChang/prism/internal/auth"
	"github.com/ChiaYuChang/prism/internal/auth/permission"
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
	Permissions   uint8      `json:"permissions"`
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
	Type        string                 `json:"type"`
	Name        string                 `json:"name"`
	Permissions *permission.Permission `json:"permissions,omitempty"`
	ExpiresAt   *time.Time             `json:"expires_at,omitempty"`
}

type TokenExpiryRequest struct {
	ExpiresAt *time.Time `json:"expires_at,omitempty"`
}

// CreateToken handles token creation for CLI/shared admin flows.
func (s *Server) CreateToken(w http.ResponseWriter, r *http.Request) {
	actor, ok := tokenActor(r)
	if !ok {
		writeError(w, http.StatusUnauthorized, "unauthorized")
		return
	}
	var req CreateTokenRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid payload")
		return
	}
	if s.TokenService == nil {
		writeError(w, http.StatusInternalServerError, "token service unavailable")
		return
	}
	result, err := s.TokenService.CreateToken(r.Context(), actor, prismauth.CreateTokenRequest{
		Type:        authtoken.Type(req.Type),
		Name:        req.Name,
		Permissions: req.Permissions,
		ExpiresAt:   req.ExpiresAt,
	})
	if err != nil {
		s.writeTokenError(w, r, "create token", err)
		return
	}
	writeJSON(w, http.StatusOK, toAdminTokenSecret(result.Token, result.Raw))
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
	actor, ok := tokenActor(r)
	if !ok {
		writeError(w, http.StatusUnauthorized, "unauthorized")
		return
	}
	params, ok := parseAdminListParams(w, r)
	if !ok {
		return
	}
	if s.TokenService == nil {
		writeError(w, http.StatusInternalServerError, "token service unavailable")
		return
	}
	rows, err := s.TokenService.ListTokens(r.Context(), actor, params)
	if err != nil {
		s.writeTokenError(w, r, "list tokens", err)
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
	actor, ok := tokenActor(r)
	if !ok {
		writeError(w, http.StatusUnauthorized, "unauthorized")
		return
	}
	id, ok := tokenIDFromPath(w, r)
	if !ok || s.TokenService == nil {
		if s.TokenService == nil && ok {
			writeError(w, http.StatusInternalServerError, "token service unavailable")
		}
		return
	}
	token, err := s.TokenService.GetToken(r.Context(), actor, id)
	if err != nil {
		s.writeTokenError(w, r, "get token", err)
		return
	}
	writeJSON(w, http.StatusOK, toAdminToken(token))
}

func (s *Server) RenewToken(w http.ResponseWriter, r *http.Request) {
	actor, ok := tokenActor(r)
	if !ok {
		writeError(w, http.StatusUnauthorized, "unauthorized")
		return
	}
	id, ok := tokenIDFromPath(w, r)
	if !ok {
		return
	}
	var req TokenExpiryRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid payload")
		return
	}
	if s.TokenService == nil {
		writeError(w, http.StatusInternalServerError, "token service unavailable")
		return
	}
	renewed, err := s.TokenService.RenewToken(r.Context(), actor, id, req.ExpiresAt)
	if err != nil {
		s.writeTokenError(w, r, "renew token", err)
		return
	}
	writeJSON(w, http.StatusOK, toAdminToken(renewed))
}

func (s *Server) RotateToken(w http.ResponseWriter, r *http.Request) {
	actor, ok := tokenActor(r)
	if !ok {
		writeError(w, http.StatusUnauthorized, "unauthorized")
		return
	}
	id, ok := tokenIDFromPath(w, r)
	if !ok {
		return
	}
	var req TokenExpiryRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid payload")
		return
	}
	if s.TokenService == nil {
		writeError(w, http.StatusInternalServerError, "token service unavailable")
		return
	}
	rotated, err := s.TokenService.RotateToken(r.Context(), actor, id, req.ExpiresAt)
	if err != nil {
		s.writeTokenError(w, r, "rotate token", err)
		return
	}
	writeJSON(w, http.StatusOK, toAdminTokenSecret(rotated.Token, rotated.Raw))
}

func (s *Server) RevokeToken(w http.ResponseWriter, r *http.Request) {
	actor, ok := tokenActor(r)
	if !ok {
		writeError(w, http.StatusUnauthorized, "unauthorized")
		return
	}
	id, ok := tokenIDFromPath(w, r)
	if !ok {
		return
	}
	if s.TokenService == nil {
		writeError(w, http.StatusInternalServerError, "token service unavailable")
		return
	}
	revoked, err := s.TokenService.RevokeToken(r.Context(), actor, id)
	if err != nil {
		s.writeTokenError(w, r, "revoke token", err)
		return
	}
	writeJSON(w, http.StatusOK, toAdminToken(revoked))
}
func tokenActor(r *http.Request) (prismauth.Actor, bool) {
	principal, ok := middleware.PrincipalFromContext(r.Context())
	if !ok {
		return prismauth.Actor{}, false
	}
	return prismauth.Actor{
		TokenID:     principal.TokenID,
		Type:        principal.Type,
		Name:        principal.Name,
		Permissions: principal.Permissions,
	}, true
}

func tokenIDFromPath(w http.ResponseWriter, r *http.Request) (uuid.UUID, bool) {
	id, err := uuid.Parse(r.PathValue("id"))
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid token id")
		return uuid.Nil, false
	}
	return id, true
}

func (s *Server) writeTokenError(w http.ResponseWriter, r *http.Request, msg string, err error) {
	if errors.Is(err, prismauth.ErrForbidden) {
		writeError(w, http.StatusForbidden, "forbidden")
		return
	}
	if errors.Is(err, pgx.ErrNoRows) {
		writeError(w, http.StatusNotFound, "token not found")
		return
	}
	if errors.Is(err, prismauth.ErrInvalidToken) {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	s.Logger.ErrorContext(r.Context(), msg+" failed", slog.Any("error", err))
	writeError(w, http.StatusInternalServerError, "failed to "+msg)
}

func (s *Server) requireAdmin(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		principal, ok := middleware.PrincipalFromContext(r.Context())
		if !ok || !principal.Permissions.Has(permission.AdminAPI) {
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
		Permissions:   token.Permissions,
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
