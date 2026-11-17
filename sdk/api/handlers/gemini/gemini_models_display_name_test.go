package gemini

import (
	"encoding/json"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/router-for-me/CLIProxyAPI/v7/internal/registry"
	"github.com/router-for-me/CLIProxyAPI/v7/sdk/api/handlers"
	sdkconfig "github.com/router-for-me/CLIProxyAPI/v7/sdk/config"
)

func TestGeminiModelsResponseUsesConfiguredDisplayName(t *testing.T) {
	const clientID = "gemini-display-name-catalog-test"
	const modelID = "gemini-display-name-catalog-test"
	registryRef := registry.GetGlobalRegistry()
	registryRef.RegisterClient(clientID, "gemini", []*registry.ModelInfo{{
		ID: modelID, Name: modelID, DisplayName: "Configured Gemini Name",
	}})
	t.Cleanup(func() {
		registryRef.UnregisterClient(clientID)
	})

	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	NewGeminiAPIHandler(&handlers.BaseAPIHandler{}).GeminiModels(ctx)

	var response struct {
		Models []struct {
			Name        string `json:"name"`
			DisplayName string `json:"displayName"`
		} `json:"models"`
	}
	if errUnmarshal := json.Unmarshal(recorder.Body.Bytes(), &response); errUnmarshal != nil {
		t.Fatalf("decode response: %v", errUnmarshal)
	}
	for _, model := range response.Models {
		if model.Name == "models/"+modelID {
			if model.DisplayName != "Configured Gemini Name" {
				t.Fatalf("displayName = %q, want Configured Gemini Name", model.DisplayName)
			}
			return
		}
	}
	t.Fatalf("model %q not found in response", modelID)
}

func TestGeminiModelsFiltersByModelAccess(t *testing.T) {
	const clientID = "gemini-access-test-client"
	modelRegistry := registry.GetGlobalRegistry()
	modelRegistry.RegisterClient(clientID, "gemini", []*registry.ModelInfo{
		{ID: "gemini-access-allowed", Name: "gemini-access-allowed", DisplayName: "Allowed"},
		{ID: "gemini-access-denied", Name: "gemini-access-denied", DisplayName: "Denied"},
	})
	t.Cleanup(func() {
		modelRegistry.UnregisterClient(clientID)
	})

	cfg := &sdkconfig.SDKConfig{
		ModelAccess: sdkconfig.ModelAccessConfig{
			DefaultAction: "deny",
			Policies: []sdkconfig.ModelAccessPolicy{
				{
					Name:          "gemini-dev",
					APIKeys:       []string{"sk-gemini-key"},
					DefaultAction: "deny",
					AllowModels:   []string{"gemini-access-allowed"},
				},
			},
		},
	}
	base := handlers.NewBaseAPIHandlers(cfg, nil)
	handler := NewGeminiAPIHandler(base)

	// 1. GeminiModels list
	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	ctx.Set("userApiKey", "sk-gemini-key")
	handler.GeminiModels(ctx)

	var response struct {
		Models []struct {
			Name string `json:"name"`
		} `json:"models"`
	}
	if err := json.Unmarshal(recorder.Body.Bytes(), &response); err != nil {
		t.Fatalf("decode response: %v", err)
	}

	hasAllowed := false
	hasDenied := false
	for _, m := range response.Models {
		if m.Name == "models/gemini-access-allowed" {
			hasAllowed = true
		}
		if m.Name == "models/gemini-access-denied" {
			hasDenied = true
		}
	}
	if !hasAllowed {
		t.Fatalf("expected models/gemini-access-allowed to be present")
	}
	if hasDenied {
		t.Fatalf("expected models/gemini-access-denied to be filtered out")
	}

	// 2. GeminiGetHandler for denied model
	recorderGetDenied := httptest.NewRecorder()
	ctxGetDenied, _ := gin.CreateTestContext(recorderGetDenied)
	ctxGetDenied.Params = gin.Params{{Key: "action", Value: "/gemini-access-denied"}}
	ctxGetDenied.Set("userApiKey", "sk-gemini-key")
	handler.GeminiGetHandler(ctxGetDenied)
	if recorderGetDenied.Code != 404 {
		t.Fatalf("expected 404 for denied model get, got %d", recorderGetDenied.Code)
	}

	// 3. GeminiGetHandler for allowed model
	recorderGetAllowed := httptest.NewRecorder()
	ctxGetAllowed, _ := gin.CreateTestContext(recorderGetAllowed)
	ctxGetAllowed.Params = gin.Params{{Key: "action", Value: "/gemini-access-allowed"}}
	ctxGetAllowed.Set("userApiKey", "sk-gemini-key")
	handler.GeminiGetHandler(ctxGetAllowed)
	if recorderGetAllowed.Code != 200 {
		t.Fatalf("expected 200 for allowed model get, got %d", recorderGetAllowed.Code)
	}
}
