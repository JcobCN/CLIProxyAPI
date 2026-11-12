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

const (
	// asciiCharsPerToken follows Pi-Agent's chars/4 heuristic (compaction.js estimateTokens)
	// but is slightly more conservative because proxied traffic is often code/JSON heavy.
	asciiCharsPerToken = 3.5
	// cjkCharsPerToken: measured Chinese text is ~0.6-0.7 tokens/char on DeepSeek/Qwen and
	// o200k_base tokenizers (DeepSeek docs: 1 Chinese char ≈ 0.6 token), i.e. ~1.6 chars/token.
	// cl100k_base is denser (~1.1 tokens/char); 1.6 stays a reasonable middle ground and is
	// far closer than Pi-Agent's flat 4 chars/token (0.25 tokens/char).
	cjkCharsPerToken = 1.6
	// estimatedImageTokens mirrors Pi-Agent's ESTIMATED_IMAGE_CHARS (4800 chars / 4).
	estimatedImageTokens = 1200
)

// structuralJSONKeys lists request fields whose string values are protocol metadata
// rather than prompt content, so they are excluded from the token estimate.
var structuralJSONKeys = map[string]struct{}{
	"id":             {},
	"metadata":       {},
	"model":          {},
	"object":         {},
	"role":           {},
	"session_id":     {},
	"stop":           {},
	"stop_sequences": {},
	"stream":         {},
	"type":           {},
	"user":           {},
}

// mediaJSONTypes lists content block types that carry non-text media.
// Such blocks are charged a flat estimate instead of their (often base64) payload size.
var mediaJSONTypes = map[string]struct{}{
	"audio":       {},
	"document":    {},
	"file":        {},
	"image":       {},
	"image_url":   {},
	"inlinedata":  {},
	"input_audio": {},
	"input_image": {},
	"video":       {},
}

// tokenEstimator accumulates content characters and converts them to tokens once at the end,
// mirroring Pi-Agent's estimator: per-string rounding would inflate the total for payloads with
// thousands of small strings (tool results, tool call arguments, ...).
//
// Reasoning/thinking content is intentionally counted. Pi-Agent normalizes upstream
// reasoning_content/reasoning/reasoning_text into thinking blocks and counts them in
// estimateTokens (compaction.js); our claude->openai translator likewise replays Claude
// thinking blocks as reasoning_content. Providers that drop the field only make this
// estimate more conservative.
type tokenEstimator struct {
	asciiChars    int
	cjkChars      int
	otherNonASCII int
	extraTokens   int
}

func (e *tokenEstimator) addString(text string) {
	for _, r := range text {
		switch {
		case r <= unicode.MaxASCII:
			e.asciiChars++
		case isCJKRune(r):
			e.cjkChars++
		default:
			e.otherNonASCII++
		}
	}
}

// isCJKRune reports whether the rune belongs to a script that packs multiple characters
// into one token on modern tokenizers.
func isCJKRune(r rune) bool {
	return unicode.Is(unicode.Han, r) ||
		unicode.Is(unicode.Hiragana, r) ||
		unicode.Is(unicode.Katakana, r) ||
		unicode.Is(unicode.Hangul, r)
}

func (e *tokenEstimator) tokens() int {
	// ASCII: ~3.5 chars/token. Han/Kana/Hangul: ~1.6 chars/token. Other non-ASCII
	// (emoji, Cyrillic, Arabic, ...): 1 token per character, which stays conservative.
	textTokens := float64(e.asciiChars)/asciiCharsPerToken + float64(e.cjkChars)/cjkCharsPerToken
	return e.extraTokens + e.otherNonASCII + int(math.Ceil(textTokens))
}

// EstimateRequestTokens computes an estimated token count from a raw JSON request payload.
// It walks every string value in the payload so nested content (Claude tool_result/tool_use
// blocks, thinking blocks, OpenAI responses input arrays, tool schemas, ...) is always counted,
// regardless of the protocol shape. Media blocks are charged a flat estimate.
func EstimateRequestTokens(rawJSON []byte) int {
	if len(rawJSON) == 0 {
		return 0
	}
	estimator := &tokenEstimator{}
	root := gjson.ParseBytes(rawJSON)
	if root.IsObject() || root.IsArray() {
		estimator.addJSONTokens(root)
	} else {
		// Non-JSON payloads (or bare scalars) are estimated from the raw text.
		estimator.addString(string(rawJSON))
	}
	return estimator.tokens()
}

// addJSONTokens recursively accumulates every content string in a JSON value.
func (e *tokenEstimator) addJSONTokens(value gjson.Result) {
	switch {
	case value.IsObject():
		if isMediaJSONBlock(value) {
			e.addMediaTokens(value)
			return
		}
		value.ForEach(func(key, child gjson.Result) bool {
			// Only structural scalars are skipped; nested objects/arrays are always walked
			// so content hidden under a structural key name is still counted.
			if _, skip := structuralJSONKeys[strings.ToLower(strings.TrimSpace(key.String()))]; skip && !child.IsObject() && !child.IsArray() {
				return true
			}
			e.addJSONTokens(child)
			return true
		})
	case value.IsArray():
		value.ForEach(func(_, child gjson.Result) bool {
			e.addJSONTokens(child)
			return true
		})
	case value.Type == gjson.String:
		e.addTokenString(value.String())
	case value.Type == gjson.Number:
		// Numeric values (e.g. completions token-id prompts) carry at least one token.
		e.extraTokens++
	}
}

// addMediaTokens charges a flat estimate for non-text media blocks and still counts
// plain-text payloads such as Claude documents with a text source.
func (e *tokenEstimator) addMediaTokens(block gjson.Result) {
	e.extraTokens += estimatedImageTokens
	if source := block.Get("source"); source.IsObject() && strings.EqualFold(strings.TrimSpace(source.Get("type").String()), "text") {
		e.addString(source.Get("data").String())
	}
}

// isMediaJSONBlock reports whether the object is a non-text media content block.
// Detection is based on explicit block types only: matching arbitrary key names would
// misclassify tool schemas that declare properties such as "image_url".
func isMediaJSONBlock(value gjson.Result) bool {
	if value.Get("inlineData").Exists() || value.Get("inline_data").Exists() {
		return true
	}
	blockType := strings.ToLower(strings.TrimSpace(value.Get("type").String()))
	_, isMedia := mediaJSONTypes[blockType]
	return isMedia
}

// addTokenString accumulates one string value, charging media data URLs a flat estimate.
func (e *tokenEstimator) addTokenString(text string) {
	if hasASCIIPrefixFold(text, "data:") {
		rest := text[len("data:"):]
		for _, kind := range []string{"image/", "audio/", "video/"} {
			if hasASCIIPrefixFold(rest, kind) {
				e.extraTokens += estimatedImageTokens
				return
			}
		}
	}
	e.addString(text)
}

// hasASCIIPrefixFold reports whether s starts with the given ASCII prefix, ignoring case.
func hasASCIIPrefixFold(s, prefix string) bool {
	return len(s) >= len(prefix) && strings.EqualFold(s[:len(prefix)], prefix)
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
