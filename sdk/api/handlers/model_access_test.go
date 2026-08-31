package handlers

import (
	"context"
	"net/http"
	"testing"

	"github.com/gin-gonic/gin"
	sdkconfig "github.com/router-for-me/CLIProxyAPI/v7/sdk/config"
)

func newCtxWithKey(apiKey string) context.Context {
	c := &gin.Context{}
	c.Set("userApiKey", apiKey)
	return context.WithValue(context.Background(), "gin", c)
}

func TestModelAccessEvaluation(t *testing.T) {
	cfg := &sdkconfig.SDKConfig{
		ModelAccess: sdkconfig.ModelAccessConfig{
			DefaultAction: "deny",
			Policies: []sdkconfig.ModelAccessPolicy{
				{
					Name:          "tier-free",
					APIKeys:       []string{"sk-free"},
					DefaultAction: "deny",
					AllowModels:   []string{"gemini-*-flash", "gpt-4o-mini"},
					DenyModels:    []string{"gpt-4o-pro"},
					StatusCode:    403,
					Message:       "upgrade required",
				},
				{
					Name:          "tier-dev",
					APIKeys:       []string{"sk-dev"},
					DefaultAction: "allow",
					DenyModels:    []string{"o1-preview"},
				},
			},
		},
		BlockedModels: []sdkconfig.BlockedModelRule{
			{
				APIKeys: []string{"sk-dev"},
				Models:  []string{"claude-opus*"},
			},
		},
	}

	h := &BaseAPIHandler{Cfg: cfg}

	// 1. sk-free: allow gpt-4o-mini
	if err := h.blockedModelError(newCtxWithKey("sk-free"), "gpt-4o-mini"); err != nil {
		t.Fatalf("expected nil for allowed model, got %v", err)
	}
	// 2. sk-free: allow wildcard gemini-1.5-flash
	if err := h.blockedModelError(newCtxWithKey("sk-free"), "gemini-1.5-flash"); err != nil {
		t.Fatalf("expected nil for allowed model wildcard, got %v", err)
	}
	// 3. sk-free: deny explicit gpt-4o-pro (deny > allow)
	if err := h.blockedModelError(newCtxWithKey("sk-free"), "gpt-4o-pro"); err == nil {
		t.Fatalf("expected error for denied model")
	} else if err.StatusCode != http.StatusForbidden || err.Error.Error() != "upgrade required" {
		t.Fatalf("unexpected error response: %+v", err)
	}
	// 4. sk-free: deny default unlisted model gpt-4o
	if err := h.blockedModelError(newCtxWithKey("sk-free"), "gpt-4o"); err == nil {
		t.Fatalf("expected error for unlisted model with default-action: deny")
	}

	// 5. sk-dev: allow unlisted model gpt-4o (default-action: allow)
	if err := h.blockedModelError(newCtxWithKey("sk-dev"), "gpt-4o"); err != nil {
		t.Fatalf("expected nil for allowed default action, got %v", err)
	}
	// 6. sk-dev: deny explicit deny-model o1-preview
	if err := h.blockedModelError(newCtxWithKey("sk-dev"), "o1-preview"); err == nil {
		t.Fatalf("expected error for o1-preview")
	}
	// 7. sk-dev: blocked by legacy BlockedModels layer claude-opus-3
	if err := h.blockedModelError(newCtxWithKey("sk-dev"), "claude-opus-3"); err == nil {
		t.Fatalf("expected error from legacy BlockedModels layer")
	}

	// 8. unlisted key sk-other: denied by global ModelAccess default-action: deny
	if err := h.blockedModelError(newCtxWithKey("sk-other"), "gpt-4o"); err == nil {
		t.Fatalf("expected error for unlisted key with global default-action: deny")
	}
}
