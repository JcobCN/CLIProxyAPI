package diff

import (
	"testing"

	"github.com/router-for-me/CLIProxyAPI/v7/internal/config"
)

// 回归测试：确保 diff 不会在没有变化时产生误报
func TestRegression_BuildConfigChangeDetails_NoBlockedModelsChange(t *testing.T) {
	oldCfg := &config.Config{
		SDKConfig: config.SDKConfig{
			APIKeys: []string{"key1"},
		},
	}
	newCfg := &config.Config{
		SDKConfig: config.SDKConfig{
			APIKeys: []string{"key1"},
		},
	}

	changes := BuildConfigChangeDetails(oldCfg, newCfg)

	// 不应该有 blocked-models 相关的变化
	for _, change := range changes {
		if change == "blocked-models: updated (0 -> 0 entries)" {
			t.Fatal("不应该报告 blocked-models 变化")
		}
	}
}

// 回归测试：确保 diff 正确报告 blocked-models 变化
func TestRegression_BuildConfigChangeDetails_BlockedModelsChanged(t *testing.T) {
	oldCfg := &config.Config{
		SDKConfig: config.SDKConfig{
			BlockedModels: []config.BlockedModelRule{
				{Models: []string{"old-model"}},
			},
		},
	}
	newCfg := &config.Config{
		SDKConfig: config.SDKConfig{
			BlockedModels: []config.BlockedModelRule{
				{Models: []string{"new-model"}},
			},
		},
	}

	changes := BuildConfigChangeDetails(oldCfg, newCfg)

	found := false
	for _, change := range changes {
		if change == "blocked-models: updated (1 -> 1 entries)" {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("应该报告 blocked-models 变化，但实际变化: %v", changes)
	}
}

// 回归测试：确保 nil 和空切片的 diff 行为一致
func TestRegression_BuildConfigChangeDetails_NilVsEmpty(t *testing.T) {
	oldCfg := &config.Config{
		SDKConfig: config.SDKConfig{
			BlockedModels: nil,
		},
	}
	newCfg := &config.Config{
		SDKConfig: config.SDKConfig{
			BlockedModels: []config.BlockedModelRule{},
		},
	}

	changes := BuildConfigChangeDetails(oldCfg, newCfg)

	// nil 和空切片应该被视为相同（reflect.DeepEqual 的行为）
	for _, change := range changes {
		if change == "blocked-models: updated (0 -> 0 entries)" {
			t.Fatal("nil 和空切片不应该被视为变化")
		}
	}
}
