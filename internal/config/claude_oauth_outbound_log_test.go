package config

import (
	"testing"

	"gopkg.in/yaml.v3"
)

func TestClaudeOAuthOutboundLogV8Path(t *testing.T) {
	raw := []byte("config-version: 8\nobservability:\n  logs:\n    claude-oauth-outbound-log: true\n")
	flat, _, errLayout := NormalizeConfigLayout(raw, false)
	if errLayout != nil {
		t.Fatalf("NormalizeConfigLayout() error = %v", errLayout)
	}
	var cfg Config
	if err := yaml.Unmarshal(flat, &cfg); err != nil {
		t.Fatalf("unmarshal flattened config: %v", err)
	}
	if !cfg.ClaudeOAuthOutboundLog {
		t.Fatalf("ClaudeOAuthOutboundLog = false, flattened config:\n%s", flat)
	}
}
