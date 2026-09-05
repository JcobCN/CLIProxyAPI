package config

import "testing"

func TestSanitizeWSProxy(t *testing.T) {
	cfg := &Config{WSProxy: WSProxyConfig{
		{Path: "easytier", Upstream: "ws://127.0.0.1:11011"},
		{Path: "/other", Upstream: "  wss://example.com:443  "},
		{Path: "", Upstream: "ws://127.0.0.1:11011"},
		{Path: "/no-upstream", Upstream: ""},
		{Path: "/bad-scheme", Upstream: "tcp://127.0.0.1:11011"},
		{Path: "/no-host", Upstream: "ws://"},
		{Path: "/easytier", Upstream: "ws://127.0.0.1:11012"},
	}}
	cfg.SanitizeWSProxy()

	if len(cfg.WSProxy) != 2 {
		t.Fatalf("route count mismatch: got %d, want 2", len(cfg.WSProxy))
	}

	first := cfg.WSProxy[0]
	if first.Path != "/easytier" {
		t.Fatalf("first path mismatch: got %q, want %q", first.Path, "/easytier")
	}
	if first.Upstream != "ws://127.0.0.1:11011" {
		t.Fatalf("first upstream mismatch: got %q", first.Upstream)
	}

	second := cfg.WSProxy[1]
	if second.Path != "/other" {
		t.Fatalf("second path mismatch: got %q, want %q", second.Path, "/other")
	}
	if second.Upstream != "wss://example.com:443" {
		t.Fatalf("second upstream mismatch: got %q", second.Upstream)
	}
}

func TestSanitizeWSProxyEmpty(t *testing.T) {
	cfg := &Config{}
	cfg.SanitizeWSProxy()
	if len(cfg.WSProxy) != 0 {
		t.Fatalf("expected no routes, got %d", len(cfg.WSProxy))
	}
}

func TestValidateWSProxyUpstream(t *testing.T) {
	valid := []string{
		"ws://127.0.0.1:11011",
		"wss://example.com",
		"http://127.0.0.1:11011",
		"https://example.com:8443/path",
	}
	for _, upstream := range valid {
		if err := ValidateWSProxyUpstream(upstream); err != nil {
			t.Errorf("expected %q to be valid, got error: %v", upstream, err)
		}
	}

	invalid := []string{
		"",
		"tcp://127.0.0.1:11011",
		"ws://",
		"127.0.0.1:11011",
	}
	for _, upstream := range invalid {
		if err := ValidateWSProxyUpstream(upstream); err == nil {
			t.Errorf("expected %q to be invalid", upstream)
		}
	}
}
