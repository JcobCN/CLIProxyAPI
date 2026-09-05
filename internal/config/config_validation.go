package config

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/url"
	"strings"

	log "github.com/sirupsen/logrus"
	"golang.org/x/crypto/bcrypt"
)

// SanitizePayloadRules validates raw JSON payload rule params and drops invalid rules.
func (cfg *Config) SanitizePayloadRules() {
	if cfg == nil {
		return
	}
	cfg.Payload.DefaultRaw = sanitizePayloadRawRules(cfg.Payload.DefaultRaw, "default-raw")
	cfg.Payload.OverrideRaw = sanitizePayloadRawRules(cfg.Payload.OverrideRaw, "override-raw")
}

// SanitizeModelAccess normalizes and validates model-access config.
func (cfg *Config) SanitizeModelAccess() {
	if cfg == nil {
		return
	}
	cfg.ModelAccess.DefaultAction = strings.ToLower(strings.TrimSpace(cfg.ModelAccess.DefaultAction))
	if cfg.ModelAccess.DefaultAction != "deny" {
		cfg.ModelAccess.DefaultAction = "allow"
	}
	outPolicies := make([]ModelAccessPolicy, 0, len(cfg.ModelAccess.Policies))
	for i, policy := range cfg.ModelAccess.Policies {
		policy.Name = strings.TrimSpace(policy.Name)
		policy.APIKeys = normalizeBlockedStrings(policy.APIKeys)
		policy.AllowModels = normalizeBlockedStrings(policy.AllowModels)
		policy.DenyModels = normalizeBlockedStrings(policy.DenyModels)
		policy.Message = strings.TrimSpace(policy.Message)
		policy.DefaultAction = strings.ToLower(strings.TrimSpace(policy.DefaultAction))
		if policy.DefaultAction != "deny" {
			policy.DefaultAction = "allow"
		}
		if len(policy.APIKeys) == 0 && len(policy.AllowModels) == 0 && len(policy.DenyModels) == 0 {
			log.WithField("policy_index", i+1).Warn("model-access policy dropped: empty policy")
			continue
		}
		if policy.StatusCode < 400 || policy.StatusCode > 599 {
			policy.StatusCode = 0
		}
		outPolicies = append(outPolicies, policy)
	}
	cfg.ModelAccess.Policies = outPolicies
}

// SanitizeBlockedModels normalizes and validates blocked-model rules.
func (cfg *Config) SanitizeBlockedModels() {
	if cfg == nil {
		return
	}
	out := make([]BlockedModelRule, 0, len(cfg.BlockedModels))
	for i, rule := range cfg.BlockedModels {
		rule.APIKeys = normalizeBlockedStrings(rule.APIKeys)
		rule.Models = normalizeBlockedStrings(rule.Models)
		rule.Message = strings.TrimSpace(rule.Message)
		if len(rule.Models) == 0 {
			log.WithField("rule_index", i+1).Warn("blocked-model rule dropped: no models configured")
			continue
		}
		if rule.StatusCode < 400 || rule.StatusCode > 599 {
			rule.StatusCode = 0
		}
		out = append(out, rule)
	}
	cfg.BlockedModels = out
}

func normalizeBlockedStrings(items []string) []string {
	out := make([]string, 0, len(items))
	for _, item := range items {
		if item = strings.ToLower(strings.TrimSpace(item)); item != "" {
			out = append(out, item)
		}
	}
	return out
}

// ValidateWSProxyUpstream reports whether an upstream URL is usable for
// WebSocket reverse proxying. Supported schemes are ws, wss, http, and https.
func ValidateWSProxyUpstream(upstream string) error {
	parsed, errParse := url.Parse(strings.TrimSpace(upstream))
	if errParse != nil {
		return fmt.Errorf("invalid upstream url: %w", errParse)
	}
	switch parsed.Scheme {
	case "ws", "wss", "http", "https":
	default:
		return fmt.Errorf("unsupported upstream scheme %q (use ws, wss, http, or https)", parsed.Scheme)
	}
	if parsed.Host == "" {
		return fmt.Errorf("upstream host is empty")
	}
	return nil
}

// SanitizeWSProxy normalizes and validates ws-proxy routes, dropping invalid
// or duplicate entries with a warning.
func (cfg *Config) SanitizeWSProxy() {
	if cfg == nil || len(cfg.WSProxy) == 0 {
		return
	}
	out := make([]WSProxyRoute, 0, len(cfg.WSProxy))
	seen := make(map[string]struct{}, len(cfg.WSProxy))
	for i, route := range cfg.WSProxy {
		route.Path = strings.TrimSpace(route.Path)
		route.Upstream = strings.TrimSpace(route.Upstream)
		if route.Path == "" || route.Upstream == "" {
			log.WithField("route_index", i+1).Warn("ws-proxy route dropped: empty path or upstream")
			continue
		}
		if !strings.HasPrefix(route.Path, "/") {
			route.Path = "/" + route.Path
		}
		if errValidate := ValidateWSProxyUpstream(route.Upstream); errValidate != nil {
			log.WithField("path", route.Path).Warnf("ws-proxy route dropped: %v", errValidate)
			continue
		}
		if _, duplicate := seen[route.Path]; duplicate {
			log.WithField("path", route.Path).Warn("ws-proxy route dropped: duplicate path")
			continue
		}
		seen[route.Path] = struct{}{}
		out = append(out, route)
	}
	cfg.WSProxy = out
}

func sanitizePayloadRawRules(rules []PayloadRule, section string) []PayloadRule {
	if len(rules) == 0 {
		return rules
	}
	out := make([]PayloadRule, 0, len(rules))
	for i := range rules {
		rule := rules[i]
		if len(rule.Params) == 0 {
			continue
		}
		invalid := false
		for path, value := range rule.Params {
			raw, ok := payloadRawString(value)
			if !ok {
				continue
			}
			trimmed := bytes.TrimSpace(raw)
			if len(trimmed) == 0 || !json.Valid(trimmed) {
				log.WithFields(log.Fields{
					"section":    section,
					"rule_index": i + 1,
					"param":      path,
				}).Warn("payload rule dropped: invalid raw JSON")
				invalid = true
				break
			}
		}
		if invalid {
			continue
		}
		out = append(out, rule)
	}
	return out
}

func payloadRawString(value any) ([]byte, bool) {
	switch typed := value.(type) {
	case string:
		return []byte(typed), true
	case []byte:
		return typed, true
	default:
		return nil, false
	}
}

// looksLikeBcrypt returns true if the provided string appears to be a bcrypt hash.
func looksLikeBcrypt(s string) bool {
	return len(s) > 4 && (s[:4] == "$2a$" || s[:4] == "$2b$" || s[:4] == "$2y$")
}

// hashSecret hashes the given secret using bcrypt.
func hashSecret(secret string) (string, error) {
	// Use default cost for simplicity.
	hashedBytes, err := bcrypt.GenerateFromPassword([]byte(secret), bcrypt.DefaultCost)
	if err != nil {
		return "", err
	}
	return string(hashedBytes), nil
}
