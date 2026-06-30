package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"time"
	"unicode"

	"github.com/ChiaYuChang/prism/internal/repo"
	"github.com/google/uuid"
)

var errAlreadyRendered = errors.New("response already rendered")

type response struct {
	OK       bool        `json:"ok"`
	Endpoint string      `json:"endpoint,omitempty"`
	Action   string      `json:"action,omitempty"`
	Warnings []string    `json:"warnings,omitempty"`
	Result   any         `json:"result,omitempty"`
	Error    *errorField `json:"error,omitempty"`
}

type errorField struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

type tokenView struct {
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

type tokenSecretView struct {
	tokenView
	Token string `json:"token"`
}

func render(ctx *cliContext, endpoint, action string, result any, warnings []string) error {
	if ctx.output == "json" {
		return json.NewEncoder(os.Stdout).Encode(response{OK: true, Endpoint: endpoint, Action: action, Warnings: warnings, Result: result})
	}
	if result != nil {
		b, _ := json.MarshalIndent(result, "", "  ")
		_, _ = fmt.Fprintln(os.Stdout, string(b))
	}
	return nil
}

func renderError(ctx *cliContext, endpoint, action string, err error, warnings []string) error {
	if ctx.output == "json" {
		_ = json.NewEncoder(os.Stdout).Encode(response{OK: false, Endpoint: endpoint, Action: action, Warnings: warnings, Error: &errorField{Code: errorCode(err), Message: err.Error()}})
		return errAlreadyRendered
	}
	return err
}

func errorCode(err error) string {
	msg := err.Error()
	code := make([]rune, 0, len(msg))
	for _, r := range msg {
		if unicode.IsLetter(r) || unicode.IsDigit(r) {
			code = append(code, unicode.ToLower(r))
			continue
		}
		if len(code) > 0 && code[len(code)-1] != '_' {
			code = append(code, '_')
		}
	}
	if len(code) == 0 {
		return "error"
	}
	if code[len(code)-1] == '_' {
		code = code[:len(code)-1]
	}
	return string(code)
}

func toTokenView(tok repo.Token) tokenView {
	return tokenView{ID: tok.ID, Type: tok.Type, Name: tok.Name, HashAlgorithm: tok.HashAlgorithm, CreatedAt: tok.CreatedAt, ExpiresAt: tok.ExpiresAt, LastUsedAt: tok.LastUsedAt, RenewedAt: tok.RenewedAt, RotatedAt: tok.RotatedAt, RevokedAt: tok.RevokedAt}
}

func toTokenSecretView(tok repo.Token, raw string) tokenSecretView {
	return tokenSecretView{tokenView: toTokenView(tok), Token: raw}
}
