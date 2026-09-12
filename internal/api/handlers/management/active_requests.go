package management

import (
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/router-for-me/CLIProxyAPI/v7/internal/activetracker"
)

// GetActiveRequests returns currently in-flight proxy requests grouped by API key.
func (h *Handler) GetActiveRequests(c *gin.Context) {
	tracker := activetracker.Default
	snapshot := []activetracker.ActiveRequest{}
	if tracker != nil {
		if snap := tracker.Snapshot(); snap != nil {
			snapshot = snap
		}
	}

	c.JSON(http.StatusOK, gin.H{
		"active_requests": snapshot,
		"total":           len(snapshot),
	})
}
