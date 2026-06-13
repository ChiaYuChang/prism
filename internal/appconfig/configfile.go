package appconfig

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"text/template"

	"github.com/spf13/viper"
)

// ReadTemplatedConfig reads path as a Go text/template config file and returns
// the rendered bytes plus the config type expected by Viper.
func ReadTemplatedConfig(path string) ([]byte, string, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, "", fmt.Errorf("read config file %q: %w", path, err)
	}

	tmpl, err := template.New(filepath.Base(path)).Funcs(template.FuncMap{
		"env": envWithDefault,
	}).Parse(string(raw))
	if err != nil {
		return nil, "", fmt.Errorf("parse config template %q: %w", path, err)
	}

	var rendered bytes.Buffer
	if err := tmpl.Execute(&rendered, nil); err != nil {
		return nil, "", fmt.Errorf("render config template %q: %w", path, err)
	}

	configType, err := configTypeForPath(path)
	if err != nil {
		return nil, "", err
	}
	return rendered.Bytes(), configType, nil
}

// ReadConfigFile renders a template-capable config file and loads it into v.
func ReadConfigFile(v *viper.Viper, path string) error {
	body, configType, err := ReadTemplatedConfig(path)
	if err != nil {
		return err
	}
	v.SetConfigType(configType)
	if err := v.ReadConfig(bytes.NewReader(body)); err != nil {
		return fmt.Errorf("parse rendered config file %q: %w", path, err)
	}
	return nil
}

func envWithDefault(name, fallback string) string {
	value, ok := os.LookupEnv(name)
	if !ok {
		return fallback
	}
	return value
}

func configTypeForPath(path string) (string, error) {
	switch strings.ToLower(filepath.Ext(path)) {
	case ".yaml", ".yml":
		return "yaml", nil
	case ".json":
		return "json", nil
	default:
		return "", fmt.Errorf("unsupported config file extension %q for %q", filepath.Ext(path), path)
	}
}
