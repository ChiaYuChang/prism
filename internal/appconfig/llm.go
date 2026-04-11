package appconfig

import "time"

// LLMConfig holds provider-agnostic LLM settings shared across worker commands.
// Flag prefix: llm-*  →  viper key prefix: llm.*
type LLMConfig struct {
	Provider string        `mapstructure:"provider" validate:"required,oneof=gemini openai ollama"`
	Key      string        `mapstructure:"key"      validate:"required"`
	Model    string        `mapstructure:"model"    validate:"required"`
	Timeout  time.Duration `mapstructure:"timeout"`
}
