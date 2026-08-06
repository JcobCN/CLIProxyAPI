package handlers

import (
	"context"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	sdkconfig "github.com/router-for-me/CLIProxyAPI/v7/sdk/config"
)

// 回归测试：确保没有配置 blocked-models 时完全不影响现有功能
func TestRegression_NoBlockedModelsConfigured(t *testing.T) {
	gin.SetMode(gin.TestMode)

	// 场景1：完全空的配置
	handler := NewBaseAPIHandlers(&sdkconfig.SDKConfig{}, nil)
	rec := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(rec)
	c.Set("userApiKey", "any-key")
	ctx := context.WithValue(context.Background(), "gin", c)

	if got := handler.blockedModelError(ctx, "gpt-5"); got != nil {
		t.Fatalf("无配置时应该返回 nil，但得到了: %+v", got)
	}

	// 场景2：有配置但为空切片
	handler2 := NewBaseAPIHandlers(&sdkconfig.SDKConfig{
		BlockedModels: []sdkconfig.BlockedModelRule{},
	}, nil)
	if got := handler2.blockedModelError(ctx, "gpt-5"); got != nil {
		t.Fatalf("空切片时应该返回 nil，但得到了: %+v", got)
	}
}

// 回归测试：确保配置存在但不匹配时不影响现有功能
func TestRegression_ConfiguredButNoMatch(t *testing.T) {
	gin.SetMode(gin.TestMode)

	handler := NewBaseAPIHandlers(&sdkconfig.SDKConfig{
		BlockedModels: []sdkconfig.BlockedModelRule{
			{
				APIKeys: []string{"blocked-key"},
				Models:  []string{"blocked-model"},
			},
		},
	}, nil)

	rec := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(rec)
	c.Set("userApiKey", "normal-key")
	ctx := context.WithValue(context.Background(), "gin", c)

	// 不同的 key，相同的 model
	if got := handler.blockedModelError(ctx, "blocked-model"); got != nil {
		t.Fatalf("key 不匹配时应该返回 nil，但得到了: %+v", got)
	}

	// 相同的 key，不同的 model
	c.Set("userApiKey", "blocked-key")
	if got := handler.blockedModelError(ctx, "normal-model"); got != nil {
		t.Fatalf("model 不匹配时应该返回 nil，但得到了: %+v", got)
	}
}

// 回归测试：确保 thinking suffix 处理安全
func TestRegression_ThinkingSuffixSafety(t *testing.T) {
	gin.SetMode(gin.TestMode)

	handler := NewBaseAPIHandlers(&sdkconfig.SDKConfig{
		BlockedModels: []sdkconfig.BlockedModelRule{
			{
				Models: []string{"gpt-5"},
			},
		},
	}, nil)

	rec := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(rec)
	c.Set("userApiKey", "any-key")
	ctx := context.WithValue(context.Background(), "gin", c)

	// 没有 suffix 的 model
	if got := handler.blockedModelError(ctx, "gpt-5"); got == nil {
		t.Fatal("应该匹配 gpt-5")
	}

	// 有 suffix 的 model（应该匹配 base model）
	if got := handler.blockedModelError(ctx, "gpt-5(8192)"); got == nil {
		t.Fatal("应该匹配 gpt-5 的 base model")
	}

	// 完全不同的 model
	if got := handler.blockedModelError(ctx, "claude-3"); got != nil {
		t.Fatalf("不应该匹配 claude-3，但得到了: %+v", got)
	}
}

// 回归测试：确保空 API key 场景安全
func TestRegression_EmptyAPIKey(t *testing.T) {
	gin.SetMode(gin.TestMode)

	handler := NewBaseAPIHandlers(&sdkconfig.SDKConfig{
		BlockedModels: []sdkconfig.BlockedModelRule{
			{
				APIKeys: []string{"specific-key"},
				Models:  []string{"gpt-5"},
			},
		},
	}, nil)

	rec := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(rec)
	// 不设置 userApiKey
	ctx := context.WithValue(context.Background(), "gin", c)

	// 空 key 不应该匹配 specific-key
	if got := handler.blockedModelError(ctx, "gpt-5"); got != nil {
		t.Fatalf("空 key 不应该匹配 specific-key，但得到了: %+v", got)
	}

	// 但是应该匹配空 api-keys 的规则（匹配任何 key）
	handler2 := NewBaseAPIHandlers(&sdkconfig.SDKConfig{
		BlockedModels: []sdkconfig.BlockedModelRule{
			{
				APIKeys: []string{}, // 空 = 匹配任何
				Models:  []string{"gpt-5"},
			},
		},
	}, nil)

	if got := handler2.blockedModelError(ctx, "gpt-5"); got == nil {
		t.Fatal("空 api-keys 应该匹配任何 key，包括空 key")
	}
}
