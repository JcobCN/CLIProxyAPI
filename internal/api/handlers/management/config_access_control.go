package management

import (
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/router-for-me/CLIProxyAPI/v7/internal/config"
)

type accessControlRequest struct {
	APIKeys     *[]string                 `json:"api-keys"`
	ModelAccess *config.ModelAccessConfig `json:"model-access"`
}

// GetAccessControl returns the API keys and per-key model policies used by the
// user-management synchronizer. It intentionally excludes all other config
// sections, including upstream credentials.
func (h *Handler) GetAccessControl(c *gin.Context) {
	h.mu.Lock()
	defer h.mu.Unlock()
	c.JSON(http.StatusOK, gin.H{
		"api-keys":     h.cfg.APIKeys,
		"model-access": h.cfg.ModelAccess,
	})
}

// PutAccessControl updates API keys and model-access together. CLIProxyAPI
// remains the only component that persists and reloads config.yaml.
func (h *Handler) PutAccessControl(c *gin.Context) {
	var req accessControlRequest
	if err := c.ShouldBindJSON(&req); err != nil || req.APIKeys == nil || req.ModelAccess == nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "api-keys and model-access are required"})
		return
	}

	h.mu.Lock()
	defer h.mu.Unlock()
	h.cfg.APIKeys = append([]string(nil), (*req.APIKeys)...)
	h.cfg.ModelAccess = *req.ModelAccess
	h.cfg.SanitizeModelAccess()
	h.persistLocked(c)
}
