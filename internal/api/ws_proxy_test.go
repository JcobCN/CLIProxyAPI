package api

import (
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/gorilla/websocket"
	proxyconfig "github.com/router-for-me/CLIProxyAPI/v7/internal/config"
)

// newWSEchoBackend starts an HTTP server that upgrades every request to a
// WebSocket connection, sends the request path (with query) as the first text
// message, and then echoes messages back.
func newWSEchoBackend(t *testing.T) *httptest.Server {
	t.Helper()
	upgrader := websocket.Upgrader{}
	backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, errUpgrade := upgrader.Upgrade(w, r, nil)
		if errUpgrade != nil {
			return
		}
		defer func() {
			if errClose := conn.Close(); errClose != nil {
				t.Logf("backend: close websocket error: %v", errClose)
			}
		}()
		firstMessage := r.URL.Path
		if r.URL.RawQuery != "" {
			firstMessage += "?" + r.URL.RawQuery
		}
		if errWrite := conn.WriteMessage(websocket.TextMessage, []byte(firstMessage)); errWrite != nil {
			return
		}
		for {
			messageType, payload, errRead := conn.ReadMessage()
			if errRead != nil {
				return
			}
			if errWrite := conn.WriteMessage(messageType, payload); errWrite != nil {
				return
			}
		}
	}))
	t.Cleanup(backend.Close)
	return backend
}

// newWSProxyTestServer builds a minimal Server with a gin engine and the given
// ws-proxy routes registered.
func newWSProxyTestServer(t *testing.T, routes []proxyconfig.WSProxyRoute) (*Server, *httptest.Server) {
	t.Helper()
	gin.SetMode(gin.TestMode)
	cfg := &proxyconfig.Config{WSProxy: routes}
	s := &Server{engine: gin.New(), cfg: cfg}
	s.registerWSProxyRoutes()
	frontend := httptest.NewServer(s.engine)
	t.Cleanup(frontend.Close)
	return s, frontend
}

// dialWSProxy connects a WebSocket client to the frontend server at the given
// path. The frontend URL scheme is rewritten to ws.
func dialWSProxy(t *testing.T, frontend *httptest.Server, path string) *websocket.Conn {
	t.Helper()
	wsURL := "ws" + strings.TrimPrefix(frontend.URL, "http") + path
	conn, _, errDial := websocket.DefaultDialer.Dial(wsURL, nil)
	if errDial != nil {
		t.Fatalf("failed to dial websocket %s: %v", wsURL, errDial)
	}
	t.Cleanup(func() {
		if errClose := conn.Close(); errClose != nil {
			t.Logf("client: close websocket error: %v", errClose)
		}
	})
	return conn
}

func TestWSProxyUpstreamURL(t *testing.T) {
	cases := []struct {
		name     string
		upstream string
		want     string
		wantErr  bool
	}{
		{name: "ws to http", upstream: "ws://127.0.0.1:11011", want: "http://127.0.0.1:11011"},
		{name: "wss to https", upstream: "wss://example.com:443", want: "https://example.com:443"},
		{name: "http passthrough", upstream: "http://127.0.0.1:11011", want: "http://127.0.0.1:11011"},
		{name: "https passthrough", upstream: "https://example.com", want: "https://example.com"},
		{name: "ws with path", upstream: "ws://127.0.0.1:11011/some/path", want: "http://127.0.0.1:11011/some/path"},
		{name: "tcp scheme rejected", upstream: "tcp://127.0.0.1:11011", wantErr: true},
		{name: "missing host rejected", upstream: "ws://", wantErr: true},
		{name: "empty rejected", upstream: "", wantErr: true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := wsProxyUpstreamURL(tc.upstream)
			if tc.wantErr {
				if err == nil {
					t.Fatalf("expected error for %q, got %q", tc.upstream, got)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error for %q: %v", tc.upstream, err)
			}
			if got.String() != tc.want {
				t.Fatalf("upstream %q: got %q, want %q", tc.upstream, got.String(), tc.want)
			}
		})
	}
}

// readWSMessage reads one text message and fails the test on error.
func readWSMessage(t *testing.T, conn *websocket.Conn) string {
	t.Helper()
	_, payload, errRead := conn.ReadMessage()
	if errRead != nil {
		t.Fatalf("failed to read message: %v", errRead)
	}
	return string(payload)
}

func TestWSProxyForwardsWebSocketAndRewritesPath(t *testing.T) {
	backend := newWSEchoBackend(t)
	_, frontend := newWSProxyTestServer(t, []proxyconfig.WSProxyRoute{
		{Path: "/easytier", Upstream: backend.URL},
	})

	conn := dialWSProxy(t, frontend, "/easytier")

	if got := readWSMessage(t, conn); got != "/" {
		t.Fatalf("backend path mismatch: got %q, want \"/\"", got)
	}

	sent := "hello easytier"
	if errWrite := conn.WriteMessage(websocket.TextMessage, []byte(sent)); errWrite != nil {
		t.Fatalf("failed to write message: %v", errWrite)
	}
	if got := readWSMessage(t, conn); got != sent {
		t.Fatalf("echo mismatch: got %q, want %q", got, sent)
	}
}

func TestWSProxyRejectsNonUpgradeRequest(t *testing.T) {
	backend := newWSEchoBackend(t)
	_, frontend := newWSProxyTestServer(t, []proxyconfig.WSProxyRoute{
		{Path: "/easytier", Upstream: backend.URL},
	})

	resp, errGet := http.Get(frontend.URL + "/easytier")
	if errGet != nil {
		t.Fatalf("plain GET failed: %v", errGet)
	}
	defer func() {
		if errClose := resp.Body.Close(); errClose != nil {
			t.Logf("close response body error: %v", errClose)
		}
	}()
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("status mismatch: got %d, want %d", resp.StatusCode, http.StatusBadRequest)
	}
}

func TestWSProxyUpstreamHotReload(t *testing.T) {
	backendA := newWSEchoBackend(t)
	backendB := newWSEchoBackend(t)
	s, frontend := newWSProxyTestServer(t, []proxyconfig.WSProxyRoute{
		{Path: "/easytier", Upstream: backendA.URL},
	})

	connBefore := dialWSProxy(t, frontend, "/easytier")
	if got := readWSMessage(t, connBefore); got != "/" {
		t.Fatalf("backend A path mismatch: got %q, want \"/\"", got)
	}

	// Simulate a hot reload that replaces the configuration pointer.
	s.cfg = &proxyconfig.Config{WSProxy: []proxyconfig.WSProxyRoute{
		{Path: "/easytier", Upstream: backendB.URL},
	}}

	connAfter := dialWSProxy(t, frontend, "/easytier")
	if got := readWSMessage(t, connAfter); got != "/" {
		t.Fatalf("backend B path mismatch: got %q, want \"/\"", got)
	}

	// The pre-reload connection must keep working on the old upstream.
	if errWrite := connBefore.WriteMessage(websocket.TextMessage, []byte("still-alive")); errWrite != nil {
		t.Fatalf("failed to write on pre-reload connection: %v", errWrite)
	}
	if got := readWSMessage(t, connBefore); got != "still-alive" {
		t.Fatalf("pre-reload echo mismatch: got %q, want %q", got, "still-alive")
	}
}

func TestWSProxySkipsConflictingPath(t *testing.T) {
	backend := newWSEchoBackend(t)
	routes := []proxyconfig.WSProxyRoute{
		{Path: "/healthz", Upstream: backend.URL},
		{Path: "/easytier", Upstream: backend.URL},
	}

	gin.SetMode(gin.TestMode)
	s := &Server{engine: gin.New(), cfg: &proxyconfig.Config{WSProxy: routes}}
	s.engine.GET("/healthz", func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{"status": "ok"})
	})
	s.registerWSProxyRoutes()

	if len(s.engine.Routes()) != 2 {
		t.Fatalf("route count mismatch: got %d, want 2", len(s.engine.Routes()))
	}

	frontend := httptest.NewServer(s.engine)
	defer frontend.Close()

	// /healthz must still be served by the original handler.
	resp, errGet := http.Get(frontend.URL + "/healthz")
	if errGet != nil {
		t.Fatalf("GET /healthz failed: %v", errGet)
	}
	defer func() {
		if errClose := resp.Body.Close(); errClose != nil {
			t.Logf("close response body error: %v", errClose)
		}
	}()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("/healthz status mismatch: got %d, want %d", resp.StatusCode, http.StatusOK)
	}
}

func TestWSProxyUnavailableUpstreamReturns502(t *testing.T) {
	// Reserve an unused local port for the upstream.
	listener, errListen := net.Listen("tcp", "127.0.0.1:0")
	if errListen != nil {
		t.Skipf("failed to reserve a port: %v", errListen)
	}
	upstreamHost := listener.Addr().String()
	if errClose := listener.Close(); errClose != nil {
		t.Fatalf("failed to close reserved listener: %v", errClose)
	}

	_, frontend := newWSProxyTestServer(t, []proxyconfig.WSProxyRoute{
		{Path: "/easytier", Upstream: "ws://" + upstreamHost},
	})

	// Send a WebSocket upgrade request with a plain HTTP client and verify the
	// proxy responds with 502 when the upstream is unavailable.
	req, errNewRequest := http.NewRequest(http.MethodGet, frontend.URL+"/easytier", nil)
	if errNewRequest != nil {
		t.Fatalf("failed to build request: %v", errNewRequest)
	}
	req.Header.Set("Connection", "Upgrade")
	req.Header.Set("Upgrade", "websocket")
	req.Header.Set("Sec-WebSocket-Version", "13")
	req.Header.Set("Sec-WebSocket-Key", "dGhlIHNhbXBsZSBub25jZQ==")

	resp, errDo := http.DefaultClient.Do(req)
	if errDo != nil {
		t.Fatalf("upgrade request failed: %v", errDo)
	}
	defer func() {
		if errClose := resp.Body.Close(); errClose != nil {
			t.Logf("close response body error: %v", errClose)
		}
	}()
	if resp.StatusCode != http.StatusBadGateway {
		t.Fatalf("status mismatch: got %d, want %d", resp.StatusCode, http.StatusBadGateway)
	}
}

func TestWSProxyQueryParamsPreserved(t *testing.T) {
	backend := newWSEchoBackend(t)
	_, frontend := newWSProxyTestServer(t, []proxyconfig.WSProxyRoute{
		{Path: "/easytier", Upstream: backend.URL},
	})

	conn := dialWSProxy(t, frontend, "/easytier?token=abc&mode=1")

	// The backend reports the path and query it received as the first message.
	if got := readWSMessage(t, conn); got != "/?token=abc&mode=1" {
		t.Fatalf("backend path/query mismatch: got %q, want %q", got, "/?token=abc&mode=1")
	}
}
