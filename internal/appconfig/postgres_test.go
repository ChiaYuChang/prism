package appconfig

import (
	"net/url"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestPostgresConfigConnStringEscapesPassword(t *testing.T) {
	wantPassword := "p@ss#word$%^&"
	cfg := PostgresConfig{
		Host:     "db.local",
		Port:     5432,
		Username: "prism_rootctl",
		Password: wantPassword,
		DB:       "prism",
		SSLMode:  "disable",
	}

	parsed, err := url.Parse(cfg.ConnString())
	require.NoError(t, err)
	gotPassword, ok := parsed.User.Password()
	require.True(t, ok)
	require.Equal(t, wantPassword, gotPassword)
	require.Equal(t, "prism_rootctl", parsed.User.Username())
	require.Equal(t, "db.local:5432", parsed.Host)
	require.Equal(t, "/prism", parsed.Path)
	require.Equal(t, "disable", parsed.Query().Get("sslmode"))
}
