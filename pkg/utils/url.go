package utils

import (
	"errors"
	"net/url"
	"strings"
)

// NormalizeURL ensures the URL is valid HTTP/HTTPS and normalizes the scheme and host to lowercase.
func NormalizeURL(raw string) (string, error) {
	parsed, err := url.Parse(strings.TrimSpace(raw))
	if err != nil || (parsed.Scheme != "http" && parsed.Scheme != "https") || parsed.Host == "" {
		return "", errors.New("invalid candidate URL")
	}

	// Reject user info to prevent credential leakage and uniqueness bypassing
	if parsed.User != nil {
		return "", errors.New("invalid candidate URL: user info not allowed")
	}

	parsed.Scheme = strings.ToLower(parsed.Scheme)
	parsed.Host = strings.ToLower(parsed.Host)
	parsed.Fragment = ""

	// Remove default ports for standard schemes
	if (parsed.Scheme == "http" && parsed.Port() == "80") ||
		(parsed.Scheme == "https" && parsed.Port() == "443") {
		parsed.Host = parsed.Hostname()
	}

	return parsed.String(), nil
}
