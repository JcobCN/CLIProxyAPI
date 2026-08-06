package config

import (
	"testing"
)

// 回归测试：确保 SanitizeBlockedModels 不影响其他配置
func TestRegression_SanitizeBlockedModels_DoesNotAffectOtherFields(t *testing.T) {
	cfg := &Config{
		SDKConfig: SDKConfig{
			APIKeys: []string{"key1", "key2"},
			BlockedModels: []BlockedModelRule{
				{
					APIKeys: []string{"blocked-key"},
					Models:  []string{"blocked-model"},
				},
			},
		},
		Payload: PayloadConfig{
			Default: []PayloadRule{
				{
					Models: []PayloadModelRule{{Name: "gpt-*"}},
					Params: map[string]any{"temperature": 0.7},
				},
			},
		},
	}

	cfg.SanitizeBlockedModels()

	// 验证 APIKeys 没有被修改
	if len(cfg.APIKeys) != 2 || cfg.APIKeys[0] != "key1" || cfg.APIKeys[1] != "key2" {
		t.Fatalf("APIKeys 被意外修改: %v", cfg.APIKeys)
	}

	// 验证 Payload 没有被修改
	if len(cfg.Payload.Default) != 1 {
		t.Fatalf("Payload.Default 被意外修改: %v", cfg.Payload.Default)
	}
	if cfg.Payload.Default[0].Models[0].Name != "gpt-*" {
		t.Fatalf("Payload.Default[0].Models[0].Name 被意外修改: %v", cfg.Payload.Default[0].Models[0].Name)
	}
}

// 回归测试：确保空配置调用 SanitizeBlockedModels 安全
func TestRegression_SanitizeBlockedModels_EmptyConfig(t *testing.T) {
	cfg := &Config{}

	// 不应该 panic
	cfg.SanitizeBlockedModels()

	if len(cfg.BlockedModels) != 0 {
		t.Fatalf("空配置应该保持空: %v", cfg.BlockedModels)
	}
}

// 回归测试：确保 nil 配置调用 SanitizeBlockedModels 安全
func TestRegression_SanitizeBlockedModels_NilConfig(t *testing.T) {
	var cfg *Config

	// 不应该 panic
	cfg.SanitizeBlockedModels()
}
