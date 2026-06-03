package dev_test

import (
	"net/url"
	"testing"

	"github.com/ChiaYuChang/prism/internal/dev"
	"github.com/stretchr/testify/require"
)

func TestFixturePathRedactsSensitiveQueryValues(t *testing.T) {
	u, err := url.Parse("https://www.googleapis.com/customsearch/v1?cx=cx-id&key=secret-key&q=computex&siteSearch=tw.news.yahoo.com")
	require.NoError(t, err)

	path := dev.FixturePath(u)
	require.Contains(t, path, "key-REDACTED")
	require.NotContains(t, path, "secret-key")
	require.Contains(t, path, "q-computex")
}
