package claude

import (
	"encoding/json"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/router-for-me/CLIProxyAPI/v7/internal/registry"
	"github.com/router-for-me/CLIProxyAPI/v7/sdk/api/handlers"
	sdkconfig "github.com/router-for-me/CLIProxyAPI/v7/sdk/config"
	"github.com/tidwall/gjson"
)

func TestClaudeModelsResponseUsesConfiguredDisplayName(t *testing.T) {
	const clientID = "claude-display-name-catalog-test"
	const modelID = "claude-display-name-catalog-test"
	registryRef := registry.GetGlobalRegistry()
	registryRef.RegisterClient(clientID, "claude", []*registry.ModelInfo{{
		ID: modelID, Object: "model", OwnedBy: "test", DisplayName: "Configured Claude Name",
	}})
	t.Cleanup(func() {
		registryRef.UnregisterClient(clientID)
	})

	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	NewClaudeCodeAPIHandler(&handlers.BaseAPIHandler{}).ClaudeModels(ctx)

	var response struct {
		Data []struct {
			ID          string `json:"id"`
			DisplayName string `json:"display_name"`
		} `json:"data"`
	}
	if errUnmarshal := json.Unmarshal(recorder.Body.Bytes(), &response); errUnmarshal != nil {
		t.Fatalf("decode response: %v", errUnmarshal)
	}
	for _, model := range response.Data {
		if model.ID == modelID {
			if model.DisplayName != "Configured Claude Name" {
				t.Fatalf("display_name = %q, want Configured Claude Name", model.DisplayName)
			}
			return
		}
	}
	t.Fatalf("model %q not found in response", modelID)
}

func TestClaudeModelsResponseDisablesModelListCloaking(t *testing.T) {
	const clientID = "claude-disable-model-list-cloaking-test"
	const modelID = "gpt-disable-model-list-cloaking-test"
	registryRef := registry.GetGlobalRegistry()
	registryRef.RegisterClient(clientID, "claude", []*registry.ModelInfo{{
		ID: modelID, Object: "model", OwnedBy: "test",
	}})
	t.Cleanup(func() {
		registryRef.UnregisterClient(clientID)
	})

	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	baseHandler := &handlers.BaseAPIHandler{Cfg: &sdkconfig.SDKConfig{
		ClaudeCode: sdkconfig.ClaudeCodeConfig{DisableCloakingModelList: true},
	}}
	NewClaudeCodeAPIHandler(baseHandler).ClaudeModels(ctx)

	var response struct {
		Data []struct {
			ID string `json:"id"`
		} `json:"data"`
	}
	if errUnmarshal := json.Unmarshal(recorder.Body.Bytes(), &response); errUnmarshal != nil {
		t.Fatalf("decode response: %v", errUnmarshal)
	}
	for _, model := range response.Data {
		if model.ID == modelID {
			return
		}
	}
	t.Fatalf("uncloaked model %q not found in response", modelID)
}

func TestClaudeModelsResponseFiltersByModelAccess(t *testing.T) {
	const clientID = "claude-access-test-client"
	modelRegistry := registry.GetGlobalRegistry()
	modelRegistry.RegisterClient(clientID, "claude", []*registry.ModelInfo{
		{ID: "claude-access-allowed", Object: "model", OwnedBy: "test"},
		{ID: "claude-access-denied", Object: "model", OwnedBy: "test"},
	})
	t.Cleanup(func() {
		modelRegistry.UnregisterClient(clientID)
	})

	cfg := &sdkconfig.SDKConfig{
		ModelAccess: sdkconfig.ModelAccessConfig{
			DefaultAction: "deny",
			Policies: []sdkconfig.ModelAccessPolicy{
				{
					Name:          "claude-dev",
					APIKeys:       []string{"sk-claude-key"},
					DefaultAction: "deny",
					AllowModels:   []string{"claude-access-allowed"},
				},
			},
		},
	}
	baseHandler := handlers.NewBaseAPIHandlers(cfg, nil)
	baseHandler.Cfg.ClaudeCode.DisableCloakingModelList = true

	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	ctx.Set("userApiKey", "sk-claude-key")
	NewClaudeCodeAPIHandler(baseHandler).ClaudeModels(ctx)

	var response struct {
		Data []struct {
			ID string `json:"id"`
		} `json:"data"`
	}
	if err := json.Unmarshal(recorder.Body.Bytes(), &response); err != nil {
		t.Fatalf("decode response: %v", err)
	}

	hasAllowed := false
	hasDenied := false
	for _, m := range response.Data {
		if m.ID == "claude-access-allowed" {
			hasAllowed = true
		}
		if m.ID == "claude-access-denied" {
			hasDenied = true
		}
	}
	if !hasAllowed {
		t.Fatalf("expected claude-access-allowed to be present")
	}
	if hasDenied {
		t.Fatalf("expected claude-access-denied to be filtered out")
	}
}

func TestRewriteClaudeDDModelInBody(t *testing.T) {
	tests := []struct {
		name      string
		body      string
		wantModel string
	}{
		{
			name:      "encoded model is decoded",
			body:      `{"model":"claude-fable-5-dd-o4-tpg","messages":[]}`,
			wantModel: "gpt-4o",
		},
		{
			name:      "plain claude model unchanged",
			body:      `{"model":"claude-sonnet-4-6","messages":[]}`,
			wantModel: "claude-sonnet-4-6",
		},
		{
			name:      "encoded model with thinking suffix",
			body:      `{"model":"claude-fable-5-dd-o4-tpg(high)","stream":true}`,
			wantModel: "gpt-4o(high)",
		},
		{
			name:      "missing model field unchanged",
			body:      `{"messages":[]}`,
			wantModel: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := rewriteClaudeDDModelInBody([]byte(tt.body))
			if model := gjson.GetBytes(got, "model").String(); model != tt.wantModel {
				t.Fatalf("model = %q, want %q; body=%s", model, tt.wantModel, string(got))
			}
		})
	}
}
