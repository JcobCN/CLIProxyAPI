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
