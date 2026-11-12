package handlers

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/router-for-me/CLIProxyAPI/v7/internal/registry"
	"github.com/router-for-me/CLIProxyAPI/v7/sdk/config"
)

func TestEstimateRequestTokens(t *testing.T) {
	tests := []struct {
		name    string
		payload string
		minWant int
		maxWant int
	}{
		{
			name:    "empty",
			payload: "",
			minWant: 0,
			maxWant: 0,
		},
		{
			name:    "openai simple user message",
			payload: `{"model":"gpt-4o","messages":[{"role":"user","content":"Hello world!"}]}`,
			minWant: 3,
			maxWant: 6,
		},
		{
			name:    "openai chinese message",
			payload: `{"model":"gpt-4o","messages":[{"role":"user","content":"你好世界，这是一个上下文测试"}]}`,
			minWant: 10,
			maxWant: 10,
		},
		{
			name: "openai multipart with image",
			payload: `{
				"model":"gpt-4o",
				"messages":[{
					"role":"user",
					"content":[
						{"type":"text","text":"Look at this picture"},
						{"type":"image_url","image_url":{"url":"data:image/png;base64,abc"}}
					]
				}]
			}`,
			minWant: 1205,
			maxWant: 1220,
		},
		{
			name: "claude with system prompt",
			payload: `{
				"model":"claude-3-7-sonnet",
				"system":"You are an expert coder.",
				"messages":[{"role":"user","content":"Write a function"}]
			}`,
			minWant: 10,
			maxWant: 15,
		},
		{
			name: "gemini contents format",
			payload: `{
				"contents":[{"parts":[{"text":"Explain quantum physics"}]}]
			}`,
			minWant: 6,
			maxWant: 10,
		},
		{
			name: "openai completions prompt",
			payload: `{
				"model":"text-davinci-003",
				"prompt":"Once upon a time"
			}`,
			minWant: 4,
			maxWant: 8,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := EstimateRequestTokens([]byte(tc.payload))
			if got < tc.minWant || got > tc.maxWant {
				t.Fatalf("EstimateRequestTokens() = %d, want between %d and %d", got, tc.minWant, tc.maxWant)
			}
		})
	}
}

func TestValidateContextLength(t *testing.T) {
	newContextWithKey := func(apiKey string) context.Context {
		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		if apiKey != "" {
			c.Set("userApiKey", apiKey)
		}
		return context.WithValue(context.Background(), "gin", c)
	}

	cfg := &config.SDKConfig{
		MaxContextLength: 100, // global limit: 100 tokens
		ModelAccess: config.ModelAccessConfig{
			Policies: []config.ModelAccessPolicy{
				{
					Name:             "strict-user",
					APIKeys:          []string{"sk-strict"},
					MaxContextLength: 10, // strict key limit: 10 tokens
				},
				{
					Name:             "generous-user",
					APIKeys:          []string{"sk-generous"},
					MaxContextLength: 500, // generous key limit: 500 tokens
				},
			},
		},
	}

	h := &BaseAPIHandler{Cfg: cfg}

	// Model with explicit 50 tokens limit registered
	reg := registry.GetGlobalRegistry()
	reg.RegisterClient("test-auth-model", "openai", []*registry.ModelInfo{
		{
			ID:               "limited-model",
			MaxContextLength: 50,
		},
	})
	defer reg.UnregisterClient("test-auth-model")

	// Payload with ~25 tokens
	// "这是一段中文测试文本，用于测试上下文限制，长度刚好在二十几个Token左右。" -> 37 Chinese characters = 37 tokens
	payloadMedium := []byte(`{"model":"gpt-4","messages":[{"role":"user","content":"这是一段中文测试文本，用于测试上下文限制，长度刚好在二十几个Token左右。"}]}`)

	// Payload with ~5 tokens
	payloadSmall := []byte(`{"model":"gpt-4","messages":[{"role":"user","content":"hi"}]}`)

	t.Run("strict key exceeds policy limit", func(t *testing.T) {
		ctx := newContextWithKey("sk-strict")
		errMsg := h.validateContextLength(ctx, "any-model", payloadMedium)
		if errMsg == nil {
			t.Fatal("expected error, got nil")
		}
		if errMsg.StatusCode != http.StatusBadRequest {
			t.Fatalf("expected 400, got %d", errMsg.StatusCode)
		}
		if !strings.Contains(errMsg.Error.Error(), "exceeds maximum limit of 10 tokens") {
			t.Fatalf("unexpected error message: %v", errMsg.Error)
		}
	})

	t.Run("strict key under policy limit passes", func(t *testing.T) {
		ctx := newContextWithKey("sk-strict")
		errMsg := h.validateContextLength(ctx, "any-model", payloadSmall)
		if errMsg != nil {
			t.Fatalf("expected nil, got error: %v", errMsg)
		}
	})

	t.Run("generous key passes with medium payload", func(t *testing.T) {
		ctx := newContextWithKey("sk-generous")
		errMsg := h.validateContextLength(ctx, "any-model", payloadMedium)
		if errMsg != nil {
			t.Fatalf("expected nil, got error: %v", errMsg)
		}
	})

	t.Run("model explicit limit takes effect when key has no policy override", func(t *testing.T) {
		ctx := newContextWithKey("sk-other")
		// payloadMedium is ~37 tokens, limited-model has limit 50 -> should pass
		if errMsg := h.validateContextLength(ctx, "limited-model", payloadMedium); errMsg != nil {
			t.Fatalf("expected pass for 37 < 50 tokens, got: %v", errMsg)
		}

		// Very long payload, well above 50 tokens
		payloadLong := []byte(`{"model":"limited-model","messages":[{"role":"user","content":"这是一段中文测试文本，用于测试上下文限制，长度刚好在二十几个Token左右。这是一段中文测试文本，用于测试上下文限制，长度刚好在二十几个Token左右。这是一段中文测试文本，用于测试上下文限制，长度刚好在二十几个Token左右。"}]}`)
		errMsg := h.validateContextLength(ctx, "limited-model", payloadLong)
		if errMsg == nil {
			t.Fatal("expected error for long payload exceeding model limit, got nil")
		}
		if !strings.Contains(errMsg.Error.Error(), "exceeds maximum limit of 50 tokens") {
			t.Fatalf("unexpected error message: %v", errMsg.Error)
		}
	})

	t.Run("global limit takes effect when neither policy nor model sets one", func(t *testing.T) {
		ctx := newContextWithKey("sk-other")
		// Global limit is 100. Let's make a ~120 token payload.
		repeatText := strings.Repeat("这是一段用于测试全局超限的中文文本。", 10) // 18 * 10 chars ≈ 120 tokens
		payloadHuge := []byte(`{"model":"unlimited-model","messages":[{"role":"user","content":"` + repeatText + `"}]}`)
		errMsg := h.validateContextLength(ctx, "unlimited-model", payloadHuge)
		if errMsg == nil {
			t.Fatal("expected error for payload exceeding global limit, got nil")
		}
		if !strings.Contains(errMsg.Error.Error(), "exceeds maximum limit of 100 tokens") {
			t.Fatalf("unexpected error message: %v", errMsg.Error)
		}
	})

	t.Run("no limit configured passes anything", func(t *testing.T) {
		hNoLimit := &BaseAPIHandler{Cfg: &config.SDKConfig{}}
		ctx := newContextWithKey("sk-other")
		errMsg := hNoLimit.validateContextLength(ctx, "any-model", payloadMedium)
		if errMsg != nil {
			t.Fatalf("expected nil when no limit configured, got: %v", errMsg)
		}
	})

	t.Run("executeWithAuthManagerFormats rejects oversized payload", func(t *testing.T) {
		ctx := newContextWithKey("sk-strict")
		_, _, errMsg := h.executeWithAuthManagerFormats(ctx, "openai", "openai", "any-model", payloadMedium, "", false, modelExecutionOptions{})
		if errMsg == nil {
			t.Fatal("expected rejection, got nil")
		}
		if errMsg.StatusCode != http.StatusBadRequest {
			t.Fatalf("expected 400, got %d", errMsg.StatusCode)
		}
	})

	t.Run("executeStreamWithAuthManagerFormats rejects oversized payload", func(t *testing.T) {
		ctx := newContextWithKey("sk-strict")
		_, _, errChan := h.executeStreamWithAuthManagerFormats(ctx, "openai", "openai", "any-model", payloadMedium, "", false, modelExecutionOptions{})
		if errChan == nil {
			t.Fatal("expected errChan, got nil")
		}
		errMsg := <-errChan
		if errMsg == nil {
			t.Fatal("expected error from errChan, got nil")
		}
		if errMsg.StatusCode != http.StatusBadRequest {
			t.Fatalf("expected 400, got %d", errMsg.StatusCode)
		}
	})
}

func TestEstimateRequestTokensCountsNestedContent(t *testing.T) {
	bigText := strings.Repeat("a", 7000) // ~2000 ASCII tokens

	tests := []struct {
		name    string
		payload string
		minWant int
		maxWant int
	}{
		{
			name:    "claude tool_result string content",
			payload: `{"messages":[{"role":"user","content":[{"type":"tool_result","tool_use_id":"toolu_1","content":"` + bigText + `"}]}]}`,
			minWant: 2000,
			maxWant: 2100,
		},
		{
			name:    "claude tool_result block content",
			payload: `{"messages":[{"role":"user","content":[{"type":"tool_result","tool_use_id":"toolu_1","content":[{"type":"text","text":"` + bigText + `"}]}]}]}`,
			minWant: 2000,
			maxWant: 2100,
		},
		{
			name:    "claude tool_use input",
			payload: `{"messages":[{"role":"assistant","content":[{"type":"tool_use","id":"toolu_1","name":"Write","input":{"content":"` + bigText + `"}}]}]}`,
			minWant: 2000,
			maxWant: 2100,
		},
		{
			name:    "claude thinking block",
			payload: `{"messages":[{"role":"assistant","content":[{"type":"thinking","thinking":"` + bigText + `","signature":"sig"}]}]}`,
			minWant: 2000,
			maxWant: 2100,
		},
		{
			name:    "openai responses input array",
			payload: `{"input":[{"role":"user","content":[{"type":"input_text","text":"` + bigText + `"}]}]}`,
			minWant: 2000,
			maxWant: 2100,
		},
		{
			name:    "gemini functionResponse",
			payload: `{"contents":[{"parts":[{"functionResponse":{"name":"read","response":{"content":"` + bigText + `"}}}]}]}`,
			minWant: 2000,
			maxWant: 2100,
		},
		{
			name:    "openai tool call arguments",
			payload: `{"messages":[{"role":"assistant","tool_calls":[{"id":"c1","type":"function","function":{"name":"write","arguments":"{\"content\":\"` + bigText + `\"}"}}]}]}`,
			minWant: 2000,
			maxWant: 2100,
		},
		{
			name:    "media payload uses flat estimate",
			payload: `{"messages":[{"role":"user","content":[{"type":"image","source":{"type":"base64","media_type":"image/png","data":"` + bigText + `"}}]}]}`,
			minWant: 1200,
			maxWant: 1200,
		},
		{
			name:    "openai reasoning_content is counted",
			payload: `{"messages":[{"role":"assistant","reasoning_content":"` + bigText + `","content":"ok"}]}`,
			minWant: 2000,
			maxWant: 2100,
		},
		{
			name:    "openai reasoning field is counted",
			payload: `{"messages":[{"role":"assistant","reasoning":"` + bigText + `","content":"ok"}]}`,
			minWant: 2000,
			maxWant: 2100,
		},
		{
			name:    "cjk uses about 1.6 characters per token",
			payload: `{"messages":[{"role":"user","content":"` + strings.Repeat("中", 160) + `"}]}`,
			minWant: 100,
			maxWant: 100,
		},
		{
			name:    "mixed cjk and ascii buckets are summed",
			payload: `{"messages":[{"role":"user","content":"` + strings.Repeat("中", 160) + strings.Repeat("a", 350) + `"}]}`,
			minWant: 200,
			maxWant: 200,
		},
		{
			name:    "many small strings are summed before rounding",
			payload: `{"messages":[{"role":"user","content":[{"type":"text","text":"` + strings.Repeat("a", 3500) + `"},{"type":"text","text":"` + strings.Repeat("a", 3500) + `"}]}]}`,
			minWant: 2000,
			maxWant: 2000,
		},
		{
			name:    "structural fields are ignored",
			payload: `{"model":"gpt-4o","stream":true,"metadata":{"user_id":"u-1"},"messages":[{"role":"user","content":"hi"}]}`,
			minWant: 1,
			maxWant: 2,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := EstimateRequestTokens([]byte(tc.payload))
			if got < tc.minWant || got > tc.maxWant {
				t.Fatalf("EstimateRequestTokens() = %d, want between %d and %d", got, tc.minWant, tc.maxWant)
			}
		})
	}
}

func TestEstimateRequestTokensEdgeCases(t *testing.T) {
	longText := strings.Repeat("a", 7000) // ~2000 ASCII tokens

	tests := []struct {
		name    string
		payload string
		minWant int
		maxWant int
	}{
		{
			name:    "non-json payload falls back to raw text",
			payload: longText,
			minWant: 2000,
			maxWant: 2100,
		},
		{
			name:    "completions token id prompt counts per number",
			payload: `{"prompt":[1,2,3,4,5,6,7,8,9,10]}`,
			minWant: 10,
			maxWant: 10,
		},
		{
			name:    "nested completions token id prompt counts per number",
			payload: `{"prompt":[[1,2,3,4,5],[6,7,8,9,10]]}`,
			minWant: 10,
			maxWant: 10,
		},
		{
			name:    "tool schema property named image_url is not flattened",
			payload: `{"tools":[{"type":"function","function":{"name":"img","parameters":{"type":"object","properties":{"image_url":{"type":"string","description":"` + longText + `"}}}}}]}`,
			minWant: 2000,
			maxWant: 2100,
		},
		{
			name:    "claude text document source is counted",
			payload: `{"messages":[{"role":"user","content":[{"type":"document","source":{"type":"text","media_type":"text/plain","data":"` + longText + `"}}]}]}`,
			minWant: 3200,
			maxWant: 3300,
		},
		{
			name:    "claude base64 document source uses flat estimate",
			payload: `{"messages":[{"role":"user","content":[{"type":"document","source":{"type":"base64","media_type":"application/pdf","data":"` + longText + `"}}]}]}`,
			minWant: 1200,
			maxWant: 1200,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := EstimateRequestTokens([]byte(tc.payload))
			if got < tc.minWant || got > tc.maxWant {
				t.Fatalf("EstimateRequestTokens() = %d, want between %d and %d", got, tc.minWant, tc.maxWant)
			}
		})
	}
}
