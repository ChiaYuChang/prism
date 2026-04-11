package appconfig

import (
	"fmt"
	"strings"

	"github.com/spf13/pflag"
	"github.com/spf13/viper"
)

// BindFlags binds all pflags prefixed with "pg-" to nested viper keys under "postgres.".
// e.g. pg-host → postgres.host, pg-sslmode → postgres.sslmode
func (PostgresConfig) BindFlags(v *viper.Viper, fs *pflag.FlagSet) error {
	return bindWithReplacer(v, fs, "pg-",
		strings.NewReplacer("pg-", "postgres.", "-", "."))
}

// BindFlags binds all pflags prefixed with "valkey-" to nested viper keys under "valkey.".
// e.g. valkey-host → valkey.host
func (ValkeyConfig) BindFlags(v *viper.Viper, fs *pflag.FlagSet) error {
	return bindWithReplacer(v, fs, "valkey-",
		strings.NewReplacer("valkey-", "valkey.", "-", "."))
}

// BindFlags binds all pflags prefixed with "log-" to nested viper keys under "logger.".
// e.g. log-path → logger.path, log-level → logger.level
func (LoggerConfig) BindFlags(v *viper.Viper, fs *pflag.FlagSet) error {
	return bindWithReplacer(v, fs, "log-",
		strings.NewReplacer("log-", "logger.", "-", "."))
}

// BindFlags binds all pflags prefixed with "llm-" to nested viper keys under "llm.".
// e.g. llm-key → llm.key, llm-model → llm.model
func (LLMConfig) BindFlags(v *viper.Viper, fs *pflag.FlagSet) error {
	return bindWithReplacer(v, fs, "llm-",
		strings.NewReplacer("llm-", "llm.", "-", "."))
}

// bindWithReplacer iterates over all flags whose name starts with prefix,
// applies r to derive the viper key, and calls v.BindPFlag for each match.
func bindWithReplacer(v *viper.Viper, fs *pflag.FlagSet, prefix string, r *strings.Replacer) error {
	var bindErr error
	fs.VisitAll(func(f *pflag.Flag) {
		if bindErr != nil || !strings.HasPrefix(f.Name, prefix) {
			return
		}
		key := r.Replace(f.Name)
		if err := v.BindPFlag(key, f); err != nil {
			bindErr = fmt.Errorf("bind %s: %w", key, err)
		}
	})
	return bindErr
}
