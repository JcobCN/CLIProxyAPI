package config

import "testing"

func TestSanitizeBlockedModels(t *testing.T) {
	cfg := &Config{
		SDKConfig: SDKConfig{
			BlockedModels: []BlockedModelRule{
				{
					APIKeys:    []string{"  Key-1 ", "", "team-*"},
					Models:     []string{"  GPT-5-Codex ", ""},
					StatusCode: 400,
					Message:    "  custom message  ",
				},
				{
					Models:     []string{"gemini-*"},
					StatusCode: 999,
					Message:    "  ",
				},
				{
					APIKeys: []string{"key-2"},
					// no models: rule should be dropped
				},
			},
		},
	}

	cfg.SanitizeBlockedModels()

	if len(cfg.BlockedModels) != 2 {
		t.Fatalf("len(BlockedModels) = %d, want 2", len(cfg.BlockedModels))
	}

	first := cfg.BlockedModels[0]
	if len(first.APIKeys) != 2 || first.APIKeys[0] != "key-1" || first.APIKeys[1] != "team-*" {
		t.Fatalf("first rule APIKeys = %#v, want [key-1 team-*]", first.APIKeys)
	}
	if len(first.Models) != 1 || first.Models[0] != "gpt-5-codex" {
		t.Fatalf("first rule Models = %#v, want [gpt-5-codex]", first.Models)
	}
	if first.Message != "custom message" {
		t.Fatalf("first rule Message = %q, want %q", first.Message, "custom message")
	}
	if first.StatusCode != 400 {
		t.Fatalf("first rule StatusCode = %d, want 400", first.StatusCode)
	}

	second := cfg.BlockedModels[1]
	if second.StatusCode != 0 {
		t.Fatalf("second rule StatusCode = %d, want 0 (reset to default)", second.StatusCode)
	}
	if len(second.Models) != 1 || second.Models[0] != "gemini-*" {
		t.Fatalf("second rule Models = %#v, want [gemini-*]", second.Models)
	}
}

func TestSanitizeBlockedModelsNil(t *testing.T) {
	var cfg *Config
	cfg.SanitizeBlockedModels()

	empty := &Config{}
	empty.SanitizeBlockedModels()
	if len(empty.BlockedModels) != 0 {
		t.Fatalf("BlockedModels = %#v, want empty", empty.BlockedModels)
	}
}
