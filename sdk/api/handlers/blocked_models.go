package handlers

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/router-for-me/CLIProxyAPI/v7/internal/interfaces"
	"github.com/router-for-me/CLIProxyAPI/v7/internal/thinking"
)

func (h *BaseAPIHandler) blockedModelError(ctx context.Context, modelName string) *interfaces.ErrorMessage {
	if h == nil || h.Cfg == nil || len(h.Cfg.BlockedModels) == 0 {
		return nil
	}
	key := ""
	if gc, ok := ctx.Value("gin").(*gin.Context); ok && gc != nil {
		if v, exists := gc.Get("userApiKey"); exists {
			key, _ = v.(string)
		}
	}
	modelName = strings.ToLower(strings.TrimSpace(modelName))
	base := strings.ToLower(strings.TrimSpace(thinking.ParseSuffix(modelName).ModelName))
	for _, rule := range h.Cfg.BlockedModels {
		if len(rule.APIKeys) > 0 && !blockedAny(rule.APIKeys, key) {
			continue
		}
		for _, pattern := range rule.Models {
			if blockedMatch(pattern, modelName) || (base != "" && blockedMatch(pattern, base)) {
				code := rule.StatusCode
				if code == 0 {
					code = http.StatusBadRequest
				}
				msg := rule.Message
				if msg == "" {
					msg = fmt.Sprintf("model %s is blocked for this API key", modelName)
				}
				return &interfaces.ErrorMessage{StatusCode: code, Error: errors.New(msg)}
			}
		}
	}
	return nil
}

func blockedAny(patterns []string, value string) bool {
	for _, p := range patterns {
		if blockedMatch(p, value) {
			return true
		}
	}
	return false
}

func blockedModelMatchesKey(patterns []string, value string) bool {
	return len(patterns) == 0 || blockedAny(patterns, value)
}
func blockedPatternMatch(pattern, value string) bool { return blockedMatch(pattern, value) }
func userAPIKeyFromContext(ctx context.Context) string {
	if ctx == nil {
		return ""
	}
	if gc, ok := ctx.Value("gin").(*gin.Context); ok && gc != nil {
		if v, exists := gc.Get("userApiKey"); exists {
			key, _ := v.(string)
			return strings.ToLower(strings.TrimSpace(key))
		}
	}
	return ""
}
func blockedMatch(pattern, value string) bool {
	if strings.TrimSpace(pattern) == "" {
		return false
	}
	pattern, value = strings.ToLower(pattern), strings.ToLower(value)
	parts := strings.Split(pattern, "*")
	pos := 0
	for i, part := range parts {
		if part == "" {
			continue
		}
		idx := strings.Index(value[pos:], part)
		if idx < 0 || (i == 0 && idx != 0) || (i == len(parts)-1 && !strings.HasSuffix(value, part)) {
			return false
		}
		pos += idx + len(part)
	}
	return true
}
