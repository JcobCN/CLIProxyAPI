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
	if h == nil || h.Cfg == nil {
		return nil
	}

	key := userAPIKeyFromContext(ctx)
	modelName = strings.ToLower(strings.TrimSpace(modelName))
	base := strings.ToLower(strings.TrimSpace(thinking.ParseSuffix(modelName).ModelName))

	// 1. ModelAccess evaluation
	if len(h.Cfg.ModelAccess.Policies) > 0 || h.Cfg.ModelAccess.DefaultAction != "" {
		matchedPolicy := false
		for _, policy := range h.Cfg.ModelAccess.Policies {
			if len(policy.APIKeys) > 0 && !blockedAny(policy.APIKeys, key) {
				continue
			}
			matchedPolicy = true
			// Check DenyModels first (deny > allow)
			for _, pattern := range policy.DenyModels {
				if blockedMatch(pattern, modelName) || (base != "" && blockedMatch(pattern, base)) {
					code := policy.StatusCode
					if code == 0 {
						code = http.StatusForbidden
					}
					msg := policy.Message
					if msg == "" {
						msg = fmt.Sprintf("model %s is denied by model access policy", modelName)
					}
					return &interfaces.ErrorMessage{StatusCode: code, Error: errors.New(msg)}
				}
			}
			// Check AllowModels
			allowed := false
			for _, pattern := range policy.AllowModels {
				if blockedMatch(pattern, modelName) || (base != "" && blockedMatch(pattern, base)) {
					allowed = true
					break
				}
			}
			if allowed {
				break
			}
			// Fallback to policy's DefaultAction
			action := policy.DefaultAction
			if action == "" {
				action = "allow"
			}
			if action == "deny" {
				code := policy.StatusCode
				if code == 0 {
					code = http.StatusForbidden
				}
				msg := policy.Message
				if msg == "" {
					msg = fmt.Sprintf("model %s is not allowed by policy", modelName)
				}
				return &interfaces.ErrorMessage{StatusCode: code, Error: errors.New(msg)}
			}
			break
		}

		if !matchedPolicy && h.Cfg.ModelAccess.DefaultAction == "deny" {
			return &interfaces.ErrorMessage{StatusCode: http.StatusForbidden, Error: errors.New(fmt.Sprintf("model %s is denied by default model access policy", modelName))}
		}
	}

	// 2. Legacy BlockedModels evaluation (final deny layer)
	if len(h.Cfg.BlockedModels) > 0 {
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
	if gc, ok := ctx.(*gin.Context); ok && gc != nil {
		if v, exists := gc.Get("userApiKey"); exists {
			if key, ok := v.(string); ok {
				return strings.ToLower(strings.TrimSpace(key))
			}
		}
	}
	if gc, ok := ctx.Value("gin").(*gin.Context); ok && gc != nil {
		if v, exists := gc.Get("userApiKey"); exists {
			if key, ok := v.(string); ok {
				return strings.ToLower(strings.TrimSpace(key))
			}
		}
	}
	return ""
}

// IsModelAllowed returns true if the specified model is allowed for the caller in context.
func (h *BaseAPIHandler) IsModelAllowed(ctx context.Context, modelName string) bool {
	return h.blockedModelError(ctx, modelName) == nil
}

// FilterAllowedModels filters out models that are not allowed for the caller in context.
func (h *BaseAPIHandler) FilterAllowedModels(ctx context.Context, models []map[string]any) []map[string]any {
	if h == nil || h.Cfg == nil || len(models) == 0 {
		return models
	}
	if len(h.Cfg.ModelAccess.Policies) == 0 && h.Cfg.ModelAccess.DefaultAction == "" && len(h.Cfg.BlockedModels) == 0 {
		return models
	}
	filtered := make([]map[string]any, 0, len(models))
	for _, m := range models {
		modelID := ""
		if id, ok := m["id"].(string); ok && id != "" {
			modelID = id
		} else if name, ok := m["name"].(string); ok && name != "" {
			modelID = strings.TrimPrefix(name, "models/")
		}
		if modelID != "" && !h.IsModelAllowed(ctx, modelID) {
			continue
		}
		filtered = append(filtered, m)
	}
	return filtered
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
