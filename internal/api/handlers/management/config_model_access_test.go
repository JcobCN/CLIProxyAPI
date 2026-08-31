package management

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/router-for-me/CLIProxyAPI/v7/internal/config"
)

func TestGetAndPutModelAccess(t *testing.T) {
	gin.SetMode(gin.TestMode)
	cfg := &config.Config{}
	cfg.ModelAccess = config.ModelAccessConfig{
		DefaultAction: "deny",
		Policies: []config.ModelAccessPolicy{
			{
				Name:        "free",
				APIKeys:     []string{"sk-free"},
				AllowModels: []string{"gpt-4o-mini"},
			},
		},
	}
	h := &Handler{cfg: cfg, configFilePath: writeTestConfigFile(t)}

	// 1. GET /v0/management/model-access
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	h.GetModelAccess(c)
	if w.Code != http.StatusOK {
		t.Fatalf("GetModelAccess returned %d", w.Code)
	}

	// 2. PUT /v0/management/model-access
	putJSON := `{"default-action":"deny","policies":[{"name":"dev","api-keys":["sk-dev"],"allow-models":["*"]}]}`
	w = httptest.NewRecorder()
	c, _ = gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodPut, "/v0/management/model-access", bytes.NewBufferString(putJSON))
	h.PutModelAccess(c)
	if w.Code != http.StatusOK {
		t.Fatalf("PutModelAccess returned %d: %s", w.Code, w.Body.String())
	}

	if len(h.cfg.ModelAccess.Policies) != 1 || h.cfg.ModelAccess.Policies[0].Name != "dev" {
		t.Fatalf("unexpected updated policies: %#v", h.cfg.ModelAccess.Policies)
	}
}
