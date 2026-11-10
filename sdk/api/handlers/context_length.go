package handlers

import (
	"context"
	"errors"
	"fmt"
	"math"
	"net/http"
	"strings"
	"unicode"

	"github.com/gin-gonic/gin"
	"github.com/router-for-me/CLIProxyAPI/v7/internal/interfaces"
	"github.com/router-for-me/CLIProxyAPI/v7/internal/registry"
	"github.com/router-for-me/CLIProxyAPI/v7/internal/thinking"
	"github.com/tidwall/gjson"
)

// Default estimated tokens for image inputs.
const estimatedImageTokens = 1200

// EstimateRequestTokens computes an estimated token count from a raw JSON request payload.
// It supports OpenAI, Claude, Gemini, completions, responses formats, and tool schemas.
func EstimateRequestTokens(rawJSON []byte) int {
	if len(rawJSON) == 0 {
		return 0
	}

	totalTokens := 0

	// 1. OpenAI messages / Claude messages
	if messages := gjson.GetBytes(rawJSON, "messages"); messages.IsArray() {
		messages.ForEach(func(_, msg gjson.Result) bool {
			content := msg.Get("content")
			if content.Type == gjson.String {
				totalTokens += estimateTokensFromText(content.String())
			} else if content.IsArray() {
				content.ForEach(func(_, part gjson.Result) bool {
					if part.Type == gjson.String {
						totalTokens += estimateTokensFromText(part.String())
					} else if text := part.Get("text"); text.Exists() {
						totalTokens += estimateTokensFromText(text.String())
					} else if partType := part.Get("type").String(); partType == "image_url" || partType == "image" {
						totalTokens += estimatedImageTokens
					}
					return true
				})
			}
			if toolCalls := msg.Get("tool_calls"); toolCalls.IsArray() {
				toolCalls.ForEach(func(_, tc gjson.Result) bool {
					totalTokens += estimateTokensFromText(tc.Get("function.name").String())
					totalTokens += estimateTokensFromText(tc.Get("function.arguments").String())
					return true
				})
			}
			return true
		})
	}

	// 2. Claude system prompt (can be string or block array)
	if sys := gjson.GetBytes(rawJSON, "system"); sys.Exists() {
		if sys.Type == gjson.String {
			totalTokens += estimateTokensFromText(sys.String())
		} else if sys.IsArray() {
			sys.ForEach(func(_, part gjson.Result) bool {
				totalTokens += estimateTokensFromText(part.Get("text").String())
				return true
			})
		}
	}

	// 3. Gemini contents & systemInstruction
	if contents := gjson.GetBytes(rawJSON, "contents"); contents.IsArray() {
		contents.ForEach(func(_, item gjson.Result) bool {
			if parts := item.Get("parts"); parts.IsArray() {
				parts.ForEach(func(_, part gjson.Result) bool {
					if text := part.Get("text"); text.Exists() {
						totalTokens += estimateTokensFromText(text.String())
					}
					return true
				})
			}
			return true
		})
	}
	if sys := gjson.GetBytes(rawJSON, "systemInstruction.parts"); sys.IsArray() {
		sys.ForEach(func(_, part gjson.Result) bool {
			if text := part.Get("text"); text.Exists() {
				totalTokens += estimateTokensFromText(text.String())
			}
			return true
		})
	}

	// 4. OpenAI completions "prompt"
	if prompt := gjson.GetBytes(rawJSON, "prompt"); prompt.Exists() {
		if prompt.Type == gjson.String {
			totalTokens += estimateTokensFromText(prompt.String())
		} else if prompt.IsArray() {
			prompt.ForEach(func(_, p gjson.Result) bool {
				totalTokens += estimateTokensFromText(p.String())
				return true
			})
		}
	}

	// 5. OpenAI responses "input" / "instructions"
	if input := gjson.GetBytes(rawJSON, "input"); input.Exists() && input.Type == gjson.String {
		totalTokens += estimateTokensFromText(input.String())
	}
	if inst := gjson.GetBytes(rawJSON, "instructions"); inst.Exists() && inst.Type == gjson.String {
		totalTokens += estimateTokensFromText(inst.String())
	}

	// 6. Tools schemas
	if tools := gjson.GetBytes(rawJSON, "tools"); tools.Exists() && len(tools.Raw) > 0 {
		totalTokens += estimateTokensFromText(tools.Raw)
	}

	// Fallback if none of the standard keys matched
	if totalTokens == 0 && len(rawJSON) > 0 {
		totalTokens = estimateTokensFromText(string(rawJSON))
	}

	return totalTokens
}

// estimateTokensFromText estimates token count from text using conservative heuristics:
// ASCII: ~3.5 chars per token (math.Ceil(ascii / 3.5))
// Non-ASCII (Chinese, CJK, emojis): 1 token per character.
func estimateTokensFromText(text string) int {
	if len(text) == 0 {
		return 0
	}
	var asciiCount int
	var nonAsciiCount int
	for _, r := range text {
		if r <= unicode.MaxASCII {
			asciiCount++
		} else {
			nonAsciiCount++
		}
	}
	asciiTokens := int(math.Ceil(float64(asciiCount) / 3.5))
	return asciiTokens + nonAsciiCount
}

// validateContextLength validates whether the incoming request exceeds the configured
// maximum context length. If exceeded, it returns an ErrorMessage to immediately reject the request.
func (h *BaseAPIHandler) validateContextLength(ctx context.Context, modelName string, rawJSON []byte) *interfaces.ErrorMessage {
	if h == nil || h.Cfg == nil || len(rawJSON) == 0 {
		return nil
	}

	limit, statusCode := h.resolveMaxContextLength(ctx, modelName)
	if limit <= 0 {
		return nil
	}

	estimatedTokens := EstimateRequestTokens(rawJSON)
	if estimatedTokens > limit {
		code := statusCode
		if code == 0 {
			code = http.StatusBadRequest
		}
		errMsg := fmt.Sprintf("context length %d exceeds maximum limit of %d tokens", estimatedTokens, limit)
		return &interfaces.ErrorMessage{
			StatusCode: code,
			Error:      errors.New(errMsg),
		}
	}

	return nil
}

// resolveMaxContextLength resolves the maximum context length limit for a request.
// Resolution precedence:
// 1. Matched ModelAccessPolicy (per-key/per-policy limit)
// 2. Explicit MaxContextLength on the model from the ModelRegistry
// 3. Global ModelAccess.MaxContextLength
// 4. Global MaxContextLength
func (h *BaseAPIHandler) resolveMaxContextLength(ctx context.Context, modelName string) (int, int) {
	key := ""
	if gc, ok := ctx.Value("gin").(*gin.Context); ok && gc != nil {
		if v, exists := gc.Get("userApiKey"); exists {
			key, _ = v.(string)
		}
	}

	// 1. Check ModelAccessPolicy
	if len(h.Cfg.ModelAccess.Policies) > 0 {
		for _, policy := range h.Cfg.ModelAccess.Policies {
			if len(policy.APIKeys) > 0 && !blockedAny(policy.APIKeys, key) {
				continue
			}
			if policy.MaxContextLength > 0 {
				return policy.MaxContextLength, policy.StatusCode
			}
			break
		}
	}

	// 2. Check model's explicit MaxContextLength in ModelRegistry
	modelNameClean := strings.ToLower(strings.TrimSpace(modelName))
	baseModel := strings.ToLower(strings.TrimSpace(thinking.ParseSuffix(modelNameClean).ModelName))

	reg := registry.GetGlobalRegistry()
	if reg != nil {
		if info := reg.GetModelInfo(modelNameClean, ""); info != nil && info.MaxContextLength > 0 {
			return info.MaxContextLength, 0
		}
		if baseModel != "" && baseModel != modelNameClean {
			if info := reg.GetModelInfo(baseModel, ""); info != nil && info.MaxContextLength > 0 {
				return info.MaxContextLength, 0
			}
		}
	}

	// 3. Check ModelAccess global MaxContextLength
	if h.Cfg.ModelAccess.MaxContextLength > 0 {
		return h.Cfg.ModelAccess.MaxContextLength, 0
	}

	// 4. Check global top-level MaxContextLength
	if h.Cfg.MaxContextLength > 0 {
		return h.Cfg.MaxContextLength, 0
	}

	return 0, 0
}
