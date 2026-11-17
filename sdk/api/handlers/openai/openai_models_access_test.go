package openai

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/router-for-me/CLIProxyAPI/v7/internal/registry"
	"github.com/router-for-me/CLIProxyAPI/v7/sdk/api/handlers"
	sdkconfig "github.com/router-for-me/CLIProxyAPI/v7/sdk/config"
)

func TestOpenAIModelsFiltersByModelAccess(t *testing.T) {
	gin.SetMode(gin.TestMode)

	const clientID = "openai-access-test-client"
	modelRegistry := registry.GetGlobalRegistry()
	modelRegistry.RegisterClient(clientID, "openai-compatibility", []*registry.ModelInfo{
		{ID: "access-test-allowed", Object: "model", OwnedBy: "test"},
		{ID: "access-test-denied", Object: "model", OwnedBy: "test"},
	})
	t.Cleanup(func() {
		modelRegistry.UnregisterClient(clientID)
	})

	cfg := &sdkconfig.SDKConfig{
		ModelAccess: sdkconfig.ModelAccessConfig{
			DefaultAction: "deny",
			Policies: []sdkconfig.ModelAccessPolicy{
				{
					Name:          "dev-policy",
					APIKeys:       []string{"sk-test-dev"},
					DefaultAction: "deny",
					AllowModels:   []string{"access-test-allowed"},
				},
			},
		},
	}
	base := handlers.NewBaseAPIHandlers(cfg, nil)
	handler := NewOpenAIAPIHandler(base)

	// 1. Standard /v1/models request
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodGet, "/v1/models", nil)
	c.Set("userApiKey", "sk-test-dev")

	handler.OpenAIModels(c)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}

	var resp struct {
		Object string `json:"object"`
		Data   []struct {
			ID string `json:"id"`
		} `json:"data"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("unmarshal error: %v", err)
	}

	hasAllowed := false
	hasDenied := false
	for _, m := range resp.Data {
		if m.ID == "access-test-allowed" {
			hasAllowed = true
		}
		if m.ID == "access-test-denied" {
			hasDenied = true
		}
	}

	if !hasAllowed {
		t.Fatalf("expected allowed model access-test-allowed to be present")
	}
	if hasDenied {
		t.Fatalf("expected denied model access-test-denied to be filtered out")
	}

	// 2. Codex client /v1/models?client_version=0.137.0 request
	wCodex := httptest.NewRecorder()
	cCodex, _ := gin.CreateTestContext(wCodex)
	cCodex.Request = httptest.NewRequest(http.MethodGet, "/v1/models?client_version=0.137.0", nil)
	cCodex.Set("userApiKey", "sk-test-dev")

	handler.OpenAIModels(cCodex)

	if wCodex.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", wCodex.Code)
	}

	var codexResp struct {
		Models []struct {
			Slug string `json:"slug"`
		} `json:"models"`
	}
	if err := json.Unmarshal(wCodex.Body.Bytes(), &codexResp); err != nil {
		t.Fatalf("unmarshal codex error: %v", err)
	}

	hasCodexAllowed := false
	hasCodexDenied := false
	for _, m := range codexResp.Models {
		if m.Slug == "access-test-allowed" {
			hasCodexAllowed = true
		}
		if m.Slug == "access-test-denied" {
			hasCodexDenied = true
		}
	}
	if !hasCodexAllowed {
		t.Fatalf("codex client: expected allowed model access-test-allowed to be present")
	}
	if hasCodexDenied {
		t.Fatalf("codex client: expected denied model access-test-denied to be filtered out")
	}
}
