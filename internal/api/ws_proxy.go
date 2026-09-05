package api

import (
	"fmt"
	"net"
	"net/http"
	"net/http/httputil"
	"net/url"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/router-for-me/CLIProxyAPI/v7/internal/config"
	log "github.com/sirupsen/logrus"
)

// wsProxyTransport is dedicated to WebSocket reverse proxying. Proxy is nil so
// HTTP(S)_PROXY environment variables cannot hijack connections to local
// upstream listeners. No response timeouts are set: once a proxied WebSocket
// connection is established it must stay open indefinitely. The dial timeout
// below only covers connection establishment.
var wsProxyTransport = &http.Transport{
	Proxy: nil,
	DialContext: (&net.Dialer{
		Timeout:   10 * time.Second,
		KeepAlive: 30 * time.Second,
	}).DialContext,
	MaxIdleConns:        100,
	IdleConnTimeout:     90 * time.Second,
	TLSHandshakeTimeout: 10 * time.Second,
}

// wsProxyUpstreamURL maps a configured upstream URL to the http(s) scheme
// expected by httputil.ReverseProxy. ws maps to http and wss maps to https.
func wsProxyUpstreamURL(upstream string) (*url.URL, error) {
	parsed, errParse := url.Parse(strings.TrimSpace(upstream))
	if errParse != nil {
		return nil, fmt.Errorf("invalid upstream url: %w", errParse)
	}
	switch parsed.Scheme {
	case "ws":
		parsed.Scheme = "http"
	case "wss":
		parsed.Scheme = "https"
	case "http", "https":
	default:
		return nil, fmt.Errorf("unsupported upstream scheme %q", parsed.Scheme)
	}
	if parsed.Host == "" {
		return nil, fmt.Errorf("upstream host is empty")
	}
	return parsed, nil
}

// currentWSProxyRoute resolves the configured route for a registered path from
// the current configuration. The upstream is read per request, so upstream
// changes are applied without a restart.
func (s *Server) currentWSProxyRoute(routePath string) (config.WSProxyRoute, bool) {
	if s == nil || s.cfg == nil {
		return config.WSProxyRoute{}, false
	}
	for _, route := range s.cfg.WSProxy {
		if route.Path == routePath {
			return route, true
		}
	}
	return config.WSProxyRoute{}, false
}

// wsReverseProxy returns a cached ReverseProxy for the given upstream target.
// Caching keeps backend connections pooled; a changed upstream produces a new
// proxy instance on the next request while established connections continue
// on the previous one.
func (s *Server) wsReverseProxy(target *url.URL) *httputil.ReverseProxy {
	key := target.String()
	s.wsProxyMu.Lock()
	defer s.wsProxyMu.Unlock()
	if s.wsProxyCache == nil {
		s.wsProxyCache = make(map[string]*httputil.ReverseProxy)
	}
	if proxy, exists := s.wsProxyCache[key]; exists {
		return proxy
	}
	proxy := &httputil.ReverseProxy{
		Rewrite: func(pr *httputil.ProxyRequest) {
			pr.SetURL(target)
			// The public path is a routing key only. Forward to the upstream
			// root so backends do not need to serve the public path.
			pr.Out.URL.Path = "/"
			pr.Out.URL.RawPath = ""
		},
		Transport: wsProxyTransport,
		ErrorHandler: func(w http.ResponseWriter, r *http.Request, err error) {
			log.Errorf("ws-proxy: upstream %s request error: %v", target.Host, err)
			http.Error(w, "ws proxy upstream unavailable", http.StatusBadGateway)
		},
	}
	s.wsProxyCache[key] = proxy
	return proxy
}

// isWebSocketUpgradeRequest reports whether the request asks for a WebSocket
// protocol upgrade.
func isWebSocketUpgradeRequest(c *gin.Context) bool {
	upgrade := strings.ToLower(strings.TrimSpace(c.GetHeader("Upgrade")))
	connection := strings.ToLower(c.GetHeader("Connection"))
	return upgrade == "websocket" && strings.Contains(connection, "upgrade")
}

// wsProxyHandler serves one configured ws-proxy path. Only WebSocket upgrade
// requests are forwarded; anything else gets a 400 response.
func (s *Server) wsProxyHandler(routePath string) gin.HandlerFunc {
	return func(c *gin.Context) {
		if !isWebSocketUpgradeRequest(c) {
			c.JSON(http.StatusBadRequest, gin.H{"error": "websocket upgrade required"})
			return
		}
		route, found := s.currentWSProxyRoute(routePath)
		if !found {
			c.JSON(http.StatusBadGateway, gin.H{"error": "ws proxy upstream not configured"})
			return
		}
		target, errTarget := wsProxyUpstreamURL(route.Upstream)
		if errTarget != nil {
			log.Errorf("ws-proxy: path %s: %v", routePath, errTarget)
			c.JSON(http.StatusBadGateway, gin.H{"error": "ws proxy upstream invalid"})
			return
		}
		s.wsReverseProxy(target).ServeHTTP(c.Writer, c.Request)
		c.Abort()
	}
}

// registerWSProxyRoutes registers one GET route per configured ws-proxy entry.
// Routes are registered once at startup: changing a path requires a restart,
// while upstream changes are picked up per request.
func (s *Server) registerWSProxyRoutes() {
	if s == nil || s.engine == nil || s.cfg == nil || len(s.cfg.WSProxy) == 0 {
		return
	}
	registered := make(map[string]struct{})
	for _, routeInfo := range s.engine.Routes() {
		registered[routeInfo.Method+" "+routeInfo.Path] = struct{}{}
	}
	for _, route := range s.cfg.WSProxy {
		key := http.MethodGet + " " + route.Path
		if _, exists := registered[key]; exists {
			log.Errorf("ws-proxy: path %s already registered by another route; skipping", route.Path)
			continue
		}
		registered[key] = struct{}{}
		s.registerWSProxyRoute(route.Path)
	}
}

// registerWSProxyRoute attaches one route, converting Gin registration panics
// (for example wildcard conflicts with existing routes) into logged errors.
func (s *Server) registerWSProxyRoute(path string) {
	defer func() {
		if rec := recover(); rec != nil {
			log.Errorf("ws-proxy: failed to register path %s: %v", path, rec)
		}
	}()
	s.engine.GET(path, s.wsProxyHandler(path))
}
