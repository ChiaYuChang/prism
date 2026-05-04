package appconfig

import (
	"fmt"
	"os"
	"strings"
)

// LoadFromFile reads a single-line secret from path, trimming trailing CR/LF
// and surrounding whitespace. An empty path returns ("", nil) so callers can
// treat "no file flag set" as a soft fallback to an env-var or flag value.
//
// Intended use: prod containers receive a path to a mounted secret (e.g.
// /run/secrets/pg-password) rather than the raw value, so the credential
// never appears in argv, /proc/<pid>/environ (when read via *_FILE env var
// only), or `docker inspect` output. Workers should call this for every
// secret-bearing config field after viper unmarshal:
//
//	if v, err := appconfig.LoadFromFile(cfg.Postgres.PasswordFile); err != nil {
//	    return nil, err
//	} else if v != "" {
//	    cfg.Postgres.Password = v
//	}
//
// Errors are wrapped with the path so misconfigured deployments fail loudly.
func LoadFromFile(path string) (string, error) {
	if path == "" {
		return "", nil
	}
	b, err := os.ReadFile(path)
	if err != nil {
		return "", fmt.Errorf("read secret file %q: %w", path, err)
	}
	return strings.TrimRight(string(b), "\r\n\t "), nil
}
