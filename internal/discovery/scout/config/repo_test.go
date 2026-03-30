package config_test

import (
	"path/filepath"
	"testing"

	scoutconfig "github.com/ChiaYuChang/prism/internal/discovery/scout/config"
	"github.com/stretchr/testify/require"
)

func TestLoad(t *testing.T) {
	cfg, err := scoutconfig.ReadFile(filepath.Join("scouts.yaml"))
	require.NoError(t, err)

	repo, err := scoutconfig.New(cfg)
	require.NoError(t, err)

	require.Equal(t, scoutconfig.CurrentVersion, repo.Config().Version)

	dpp, ok := repo.HTML("dpp")
	require.True(t, ok)
	require.True(t, dpp.Enabled)
	require.Equal(t, []string{"www.dpp.org.tw"}, dpp.Hosts)
	require.NotEmpty(t, dpp.Config.Headers["User-Agent"])
	require.Len(t, dpp.Config.Rules, 2)

	tpp, ok := repo.HTML("tpp")
	require.True(t, ok)
	require.Equal(t, []string{"www.tpp.org.tw"}, tpp.Hosts)
	require.Len(t, tpp.Config.Rules, 1)

	cna, ok := repo.RSS("cna")
	require.True(t, ok)
	require.True(t, cna.Enabled)
	require.Equal(t, []string{"feeds.feedburner.com"}, cna.Hosts)

	pts, ok := repo.Atom("pts")
	require.True(t, ok)
	require.Equal(t, []string{"news.pts.org.tw"}, pts.Hosts)

	kmt, ok := repo.Atom("kmt")
	require.True(t, ok)
	require.Equal(t, []string{"www.kmt.org.tw"}, kmt.Hosts)

	yahoo, ok := repo.Custom("yahoo")
	require.True(t, ok)
	require.Equal(t, []string{"tw.news.yahoo.com"}, yahoo.Hosts)
}
