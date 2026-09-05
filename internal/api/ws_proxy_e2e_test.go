package api

import (
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	proxyconfig "github.com/router-for-me/CLIProxyAPI/v7/internal/config"
	"github.com/router-for-me/CLIProxyAPI/v7/sdk/access"
	"github.com/router-for-me/CLIProxyAPI/v7/sdk/cliproxy/auth"
)

// TestWSProxyEndToEndFromYAMLConfig exercises the full production path: a real
// config.yaml with a ws-proxy section is loaded through LoadConfig, a real
// Server is built through NewServer (including setupRoutes and route conflict
// checks), and a real WebSocket client connects through the registered route.
func TestWSProxyEndToEndFromYAMLConfig(t *testing.T) {
	backend := newWSEchoBackend(t)

	tmpDir := t.TempDir()
	authDir := filepath.Join(tmpDir, "auth")
	if err := os.MkdirAll(authDir, 0o700); err != nil {
		t.Fatalf("failed to create auth dir: %v", err)
	}

	configYAML := "port: 0\nauth-dir: " + authDir + "\napi-keys:\n  - \"test-key\"\nws-proxy:\n  - path: /easytier\n    upstream: " + backend.URL + "\n"
	configPath := filepath.Join(tmpDir, "config.yaml")
	if err := os.WriteFile(configPath, []byte(configYAML), 0o644); err != nil {
		t.Fatalf("failed to write config: %v", err)
	}

	cfg, errLoad := proxyconfig.LoadConfig(configPath)
	if errLoad != nil {
		t.Fatalf("LoadConfig failed: %v", errLoad)
	}
	if len(cfg.WSProxy) != 1 {
		t.Fatalf("ws-proxy not loaded from YAML: %+v", cfg.WSProxy)
	}
	if cfg.WSProxy[0].Path != "/easytier" || cfg.WSProxy[0].Upstream != backend.URL {
		t.Fatalf("ws-proxy mismatch: %+v", cfg.WSProxy[0])
	}

	authManager := auth.NewManager(nil, nil, nil)
	accessManager := access.NewManager()
	server := NewServer(cfg, authManager, accessManager, configPath)

	// The route must coexist with all production routes.
	found := false
	for _, route := range server.engine.Routes() {
		if route.Method == "GET" && route.Path == "/easytier" {
			found = true
			break
		}
	}
	if !found {
		t.Fatal("GET /easytier route not registered by NewServer")
	}

	// A real WebSocket round trip through the full server stack.
	frontend := httptest.NewServer(server.engine)
	defer frontend.Close()
	conn := dialWSProxy(t, frontend, "/easytier")
	if got := readWSMessage(t, conn); got != "/" {
		t.Fatalf("backend path mismatch: got %q, want \"/\"", got)
	}
}
