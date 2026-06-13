package appconfig

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/spf13/viper"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestReadTemplatedConfigStaticYAML(t *testing.T) {
	path := writeConfigFile(t, "config.yaml", "name: prism\n")

	body, configType, err := ReadTemplatedConfig(path)
	require.NoError(t, err)
	assert.Equal(t, "yaml", configType)
	assert.Equal(t, "name: prism\n", string(body))
}

func TestReadTemplatedConfigEnvDefault(t *testing.T) {
	path := writeConfigFile(t, "config.yaml", "name: '{{ env \"PRISM_TEST_NAME\" \"fallback\" }}'\n")

	body, _, err := ReadTemplatedConfig(path)
	require.NoError(t, err)
	assert.Equal(t, "name: 'fallback'\n", string(body))
}

func TestReadTemplatedConfigEnvOverride(t *testing.T) {
	t.Setenv("PRISM_TEST_NAME", "override")
	path := writeConfigFile(t, "config.yaml", "name: '{{ env \"PRISM_TEST_NAME\" \"fallback\" }}'\n")

	body, _, err := ReadTemplatedConfig(path)
	require.NoError(t, err)
	assert.Equal(t, "name: 'override'\n", string(body))
}

func TestReadConfigFile(t *testing.T) {
	t.Setenv("PRISM_TEST_NAME", "loaded")
	path := writeConfigFile(t, "config.yaml", "name: '{{ env \"PRISM_TEST_NAME\" \"fallback\" }}'\n")
	v := viper.New()

	require.NoError(t, ReadConfigFile(v, path))
	assert.Equal(t, "loaded", v.GetString("name"))
}

func TestReadTemplatedConfigUnsupportedExtension(t *testing.T) {
	path := writeConfigFile(t, "config.conf", "name: prism\n")

	_, _, err := ReadTemplatedConfig(path)
	require.Error(t, err)
	assert.ErrorContains(t, err, "unsupported config file extension")
}

func writeConfigFile(t *testing.T, name, body string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), name)
	require.NoError(t, os.WriteFile(path, []byte(body), 0600))
	return path
}
