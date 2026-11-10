package management

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/router-for-me/CLIProxyAPI/v7/internal/config"
)

func TestPermissionsEndpointsPreserveUnrelatedConfig(t *testing.T) {
	gin.SetMode(gin.TestMode)
	cfg := &config.Config{}
	cfg.APIKeys = []string{"external-key"}
	cfg.ModelAccess = config.ModelAccessConfig{
		DefaultAction: "allow",
		Policies: []config.ModelAccessPolicy{{
			Name:          "existing",
			APIKeys:       []string{"external-key"},
			DefaultAction: "allow",
			AllowModels:   []string{"gpt-4o"},
		}},
	}
	cfg.BlockedModels = []config.BlockedModelRule{{
		APIKeys: []string{"external-key"},
		Models:  []string{"o1*"},
	}}
	h := &Handler{cfg: cfg, configFilePath: writeTestConfigFile(t)}

	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	h.GetPermissions(c)
	if w.Code != http.StatusOK {
		t.Fatalf("GetPermissions returned %d", w.Code)
	}
	var got map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &got); err != nil {
		t.Fatalf("decode permissions response: %v", err)
	}
	if _, ok := got["model-access"]; !ok {
		t.Fatal("permissions response omitted model-access")
	}
	if _, ok := got["blocked-models"]; !ok {
		t.Fatal("permissions response omitted blocked-models")
	}

	body := `{"model-access":{"default-action":"deny","policies":[]},"blocked-models":[]}`
	w = httptest.NewRecorder()
	c, _ = gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodPut, "/v0/management/permissions", bytes.NewBufferString(body))
	h.PutPermissions(c)
	if w.Code != http.StatusOK {
		t.Fatalf("PutPermissions returned %d: %s", w.Code, w.Body.String())
	}
	if len(h.cfg.APIKeys) != 1 || h.cfg.APIKeys[0] != "external-key" {
		t.Fatalf("api keys were unexpectedly changed: %#v", h.cfg.APIKeys)
	}
	if h.cfg.ModelAccess.DefaultAction != "deny" || len(h.cfg.BlockedModels) != 0 {
		t.Fatalf("unexpected permissions after PUT: model-access=%#v blocked=%#v", h.cfg.ModelAccess, h.cfg.BlockedModels)
	}
}

func TestPermissionsEndpointsSupportMaxContextLength(t *testing.T) {
	gin.SetMode(gin.TestMode)
	cfg := &config.Config{}
	h := &Handler{cfg: cfg, configFilePath: writeTestConfigFile(t)}

	putBody := `{
		"model-access": {
			"default-action": "allow",
			"max-context-length": 65536,
			"policies": [
				{
					"name": "p1",
					"api-keys": ["key1"],
					"max-context-length": 32768,
					"allow-models": ["gpt-4o"]
				}
			]
		}
	}`

	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodPut, "/v0/management/permissions", bytes.NewBufferString(putBody))
	h.PutPermissions(c)
	if w.Code != http.StatusOK {
		t.Fatalf("PutPermissions returned %d: %s", w.Code, w.Body.String())
	}

	if h.cfg.ModelAccess.MaxContextLength != 65536 {
		t.Fatalf("expected ModelAccess.MaxContextLength = 65536, got %d", h.cfg.ModelAccess.MaxContextLength)
	}
	if len(h.cfg.ModelAccess.Policies) != 1 || h.cfg.ModelAccess.Policies[0].MaxContextLength != 32768 {
		t.Fatalf("expected policy MaxContextLength = 32768, got %d", h.cfg.ModelAccess.Policies[0].MaxContextLength)
	}

	// Now verify GET /v0/management/permissions
	wGet := httptest.NewRecorder()
	cGet, _ := gin.CreateTestContext(wGet)
	h.GetPermissions(cGet)
	if wGet.Code != http.StatusOK {
		t.Fatalf("GetPermissions returned %d: %s", wGet.Code, wGet.Body.String())
	}
	var got struct {
		ModelAccess config.ModelAccessConfig `json:"model-access"`
	}
	if err := json.Unmarshal(wGet.Body.Bytes(), &got); err != nil {
		t.Fatalf("decode GET permissions: %v", err)
	}
	if got.ModelAccess.MaxContextLength != 65536 {
		t.Fatalf("GET returned MaxContextLength = %d, want 65536", got.ModelAccess.MaxContextLength)
	}
	if len(got.ModelAccess.Policies) != 1 || got.ModelAccess.Policies[0].MaxContextLength != 32768 {
		t.Fatalf("GET returned policy MaxContextLength = %d, want 32768", got.ModelAccess.Policies[0].MaxContextLength)
	}
}

func TestAccessControlEndpointRequiresBothFields(t *testing.T) {
	gin.SetMode(gin.TestMode)
	h := &Handler{cfg: &config.Config{}, configFilePath: writeTestConfigFile(t)}
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodPut, "/v0/management/access-control", bytes.NewBufferString(`{"api-keys":[]}`))
	h.PutAccessControl(c)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("PutAccessControl returned %d, want %d", w.Code, http.StatusBadRequest)
	}
}
