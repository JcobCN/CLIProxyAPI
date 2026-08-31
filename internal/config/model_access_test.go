package config

import (
	"testing"
)

func TestSanitizeModelAccess(t *testing.T) {
	cfg := &Config{}
	cfg.ModelAccess = ModelAccessConfig{
		DefaultAction: "DENY",
		Policies: []ModelAccessPolicy{
			{
				Name:          "Dev",
				APIKeys:       []string{" SK-KEY-1 ", "sk-key-2"},
				DefaultAction: "ALLOW",
				AllowModels:   []string{" GPT-4* ", "claude-*"},
				DenyModels:    []string{" gpt-4-32k "},
				StatusCode:    403,
				Message:       "Denied",
			},
			{
				// Empty policy should be dropped
				Name: "Empty",
			},
		},
	}

	cfg.SanitizeModelAccess()

	if cfg.ModelAccess.DefaultAction != "deny" {
		t.Fatalf("DefaultAction = %q, want %q", cfg.ModelAccess.DefaultAction, "deny")
	}
	if len(cfg.ModelAccess.Policies) != 1 {
		t.Fatalf("len(Policies) = %d, want 1", len(cfg.ModelAccess.Policies))
	}
	p := cfg.ModelAccess.Policies[0]
	if p.Name != "Dev" {
		t.Fatalf("Name = %q, want Dev", p.Name)
	}
	if len(p.APIKeys) != 2 || p.APIKeys[0] != "sk-key-1" {
		t.Fatalf("APIKeys = %#v", p.APIKeys)
	}
	if len(p.AllowModels) != 2 || p.AllowModels[0] != "gpt-4*" {
		t.Fatalf("AllowModels = %#v", p.AllowModels)
	}
	if len(p.DenyModels) != 1 || p.DenyModels[0] != "gpt-4-32k" {
		t.Fatalf("DenyModels = %#v", p.DenyModels)
	}
	if p.StatusCode != 403 {
		t.Fatalf("StatusCode = %d, want 403", p.StatusCode)
	}
}
