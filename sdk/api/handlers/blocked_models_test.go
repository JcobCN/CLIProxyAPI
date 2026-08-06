package handlers

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	coreauth "github.com/router-for-me/CLIProxyAPI/v7/sdk/cliproxy/auth"
	sdkconfig "github.com/router-for-me/CLIProxyAPI/v7/sdk/config"
)

func TestBlockedPatternMatch(t *testing.T) {
	tests := []struct {
		name    string
		pattern string
		value   string
		want    bool
	}{
		{name: "empty pattern", pattern: "", value: "gpt-5", want: false},
		{name: "exact match", pattern: "gpt-5", value: "gpt-5", want: true},
		{name: "exact mismatch", pattern: "gpt-5", value: "gpt-6", want: false},
		{name: "prefix wildcard", pattern: "gemini-*", value: "gemini-2.5-pro", want: true},
		{name: "prefix wildcard mismatch", pattern: "gemini-*", value: "claude-3", want: false},
		{name: "suffix wildcard", pattern: "*-thinking", value: "claude-opus-thinking", want: true},
		{name: "substring wildcard", pattern: "*flash*", value: "gemini-2.5-flash-lite", want: true},
		{name: "middle wildcard", pattern: "gpt-*-mini", value: "gpt-5-codex-mini", want: true},
		{name: "middle wildcard mismatch", pattern: "gpt-*-mini", value: "gpt-5-codex", want: false},
		{name: "overlapping prefix suffix", pattern: "abcde*de", value: "abcde", want: false},
		{name: "prefix equals value", pattern: "abcde*", value: "abcde", want: true},
		{name: "all wildcard", pattern: "*", value: "anything", want: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := blockedPatternMatch(tt.pattern, tt.value); got != tt.want {
				t.Fatalf("blockedPatternMatch(%q, %q) = %v, want %v", tt.pattern, tt.value, got, tt.want)
			}
		})
	}
}

func TestBlockedModelMatchesKey(t *testing.T) {
	tests := []struct {
		name    string
		pattern []string
		key     string
		want    bool
	}{
		{name: "empty list matches any", pattern: nil, key: "k1", want: true},
		{name: "exact match", pattern: []string{"k1"}, key: "k1", want: true},
		{name: "exact mismatch", pattern: []string{"k1"}, key: "k2", want: false},
		{name: "wildcard match", pattern: []string{"team-*"}, key: "team-a", want: true},
		{name: "any of list", pattern: []string{"k1", "team-*"}, key: "team-b", want: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := blockedModelMatchesKey(tt.pattern, tt.key); got != tt.want {
				t.Fatalf("blockedModelMatchesKey(%v, %q) = %v, want %v", tt.pattern, tt.key, got, tt.want)
			}
		})
	}
}

func TestBlockedModelError(t *testing.T) {
	gin.SetMode(gin.TestMode)
	cfg := &sdkconfig.SDKConfig{
		BlockedModels: []sdkconfig.BlockedModelRule{
			{
				APIKeys:    []string{"key-1"},
				Models:     []string{"gpt-5-codex"},
				StatusCode: http.StatusBadRequest,
				Message:    "blocked for key-1",
			},
			{
				APIKeys:    []string{"team-*"},
				Models:     []string{"gemini-*"},
				StatusCode: http.StatusForbidden,
			},
			{
				Models:  []string{"secret-*"},
				Message: "blocked for all",
			},
		},
	}
	handler := NewBaseAPIHandlers(cfg, coreauth.NewManager(nil, nil, nil))

	newCtx := func(apiKey string) context.Context {
		rec := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(rec)
		if apiKey != "" {
			c.Set("userApiKey", apiKey)
		}
		return context.WithValue(context.Background(), "gin", c)
	}

	tests := []struct {
		name       string
		apiKey     string
		model      string
		wantStatus int
		wantMsg    string
	}{
		{
			name:       "exact key and model blocked",
			apiKey:     "key-1",
			model:      "gpt-5-codex",
			wantStatus: http.StatusBadRequest,
			wantMsg:    "blocked for key-1",
		},
		{
			name:       "key mismatch not blocked",
			apiKey:     "key-2",
			model:      "gpt-5-codex",
			wantStatus: 0,
		},
		{
			name:       "model mismatch not blocked",
			apiKey:     "key-1",
			model:      "claude-3",
			wantStatus: 0,
		},
		{
			name:       "wildcard key and model",
			apiKey:     "team-a",
			model:      "gemini-2.5-pro",
			wantStatus: http.StatusForbidden,
		},
		{
			name:       "wildcard key model mismatch",
			apiKey:     "team-a",
			model:      "gpt-5",
			wantStatus: 0,
		},
		{
			name:       "any key rule",
			apiKey:     "other-key",
			model:      "secret-model",
			wantStatus: http.StatusBadRequest,
			wantMsg:    "blocked for all",
		},
		{
			name:       "thinking suffix still matches base model",
			apiKey:     "key-1",
			model:      "gpt-5-codex(8192)",
			wantStatus: http.StatusBadRequest,
			wantMsg:    "blocked for key-1",
		},
		{
			name:       "no api key present",
			apiKey:     "",
			model:      "gpt-5-codex",
			wantStatus: 0,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := handler.blockedModelError(newCtx(tt.apiKey), tt.model)
			if tt.wantStatus == 0 {
				if got != nil {
					t.Fatalf("blockedModelError() = %+v, want nil", got)
				}
				return
			}
			if got == nil {
				t.Fatal("blockedModelError() = nil, want error")
			}
			if got.StatusCode != tt.wantStatus {
				t.Fatalf("status = %d, want %d", got.StatusCode, tt.wantStatus)
			}
			if tt.wantMsg != "" && !strings.Contains(got.Error.Error(), tt.wantMsg) {
				t.Fatalf("message = %q, want contains %q", got.Error.Error(), tt.wantMsg)
			}
		})
	}
}

func TestBlockedModelErrorNoConfig(t *testing.T) {
	gin.SetMode(gin.TestMode)
	handler := NewBaseAPIHandlers(&sdkconfig.SDKConfig{}, coreauth.NewManager(nil, nil, nil))
	rec := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(rec)
	c.Set("userApiKey", "key-1")
	ctx := context.WithValue(context.Background(), "gin", c)
	if got := handler.blockedModelError(ctx, "gpt-5-codex"); got != nil {
		t.Fatalf("blockedModelError() = %+v, want nil", got)
	}
}

func TestUserAPIKeyFromContext(t *testing.T) {
	gin.SetMode(gin.TestMode)
	rec := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(rec)
	c.Set("userApiKey", "MyKey")
	ctx := context.WithValue(context.Background(), "gin", c)
	if got := userAPIKeyFromContext(ctx); got != "mykey" {
		t.Fatalf("userAPIKeyFromContext() = %q, want %q", got, "mykey")
	}
	if got := userAPIKeyFromContext(context.Background()); got != "" {
		t.Fatalf("userAPIKeyFromContext(empty ctx) = %q, want empty", got)
	}
}
