package utils

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestNormalizeURL(t *testing.T) {
	testCases := []struct {
		name      string
		rawURL    string
		want      string
		wantError bool
	}{
		{
			name:      "OK - Happy path",
			rawURL:    "https://example.com/path",
			want:      "https://example.com/path",
			wantError: false,
		},
		{
			name:      "OK - Default HTTP port stripped",
			rawURL:    "http://example.com:80/path",
			want:      "http://example.com/path",
			wantError: false,
		},
		{
			name:      "OK - Default HTTPS port stripped",
			rawURL:    "https://example.com:443/path",
			want:      "https://example.com/path",
			wantError: false,
		},
		{
			name:      "OK - Non-default port preserved",
			rawURL:    "https://example.com:8443/path",
			want:      "https://example.com:8443/path",
			wantError: false,
		},
		{
			name:      "OK - Fragment removed",
			rawURL:    "https://example.com/article#section",
			want:      "https://example.com/article",
			wantError: false,
		},
		{
			name:      "Error - User-info rejected",
			rawURL:    "https://user:pass@example.com/path",
			want:      "",
			wantError: true,
		},
		{
			name:      "OK - Whitespace trimmed",
			rawURL:    "  https://example.com/path  ",
			want:      "https://example.com/path",
			wantError: false,
		},
		{
			name:      "Error - Non-HTTP scheme rejected",
			rawURL:    "ftp://example.com/file",
			want:      "",
			wantError: true,
		},
		{
			name:      "Error - Empty host rejected",
			rawURL:    "http:///path",
			want:      "",
			wantError: true,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			// WHEN
			got, err := NormalizeURL(tc.rawURL)

			// THEN
			if tc.wantError {
				require.Error(t, err)
			} else {
				require.NoError(t, err)
				require.Equal(t, tc.want, got)
			}
		})
	}
}
