// internal/web/server.go
package web

import (
	"context"
	"embed"
	"encoding/json"
	"fmt"
	"io/fs"
	"log"
	"net"
	"net/http"
	"os/exec"
	"runtime"
	"strings"
	"time"

	"github.com/tzone85/vortex-dispatch/internal/graph"
	"github.com/tzone85/vortex-dispatch/internal/state"
)

// Version is the application version, injected at build time or set by the
// caller (e.g. cli package). Defaults to "dev" for local builds.
var Version = "dev"

//go:embed static/*
var staticFiles embed.FS

type Server struct {
	eventStore state.EventStore
	projStore  *state.SQLiteStore
	hub        *Hub
	port       int
	reqFilter  state.ReqFilter
	httpServer *http.Server
	dagExport  *graph.DAGExport
	startTime  time.Time
	NoOpen     bool   // skip opening browser on start
	authToken  string // per-session secret gating the /ws command channel
}

func NewServer(es state.EventStore, ps *state.SQLiteStore, port int, filter state.ReqFilter) *Server {
	s := &Server{
		eventStore: es,
		projStore:  ps,
		port:       port,
		reqFilter:  filter,
		startTime:  time.Now(),
		authToken:  newAuthToken(),
	}
	s.hub = NewHub(s)
	return s
}

// SetDAG sets the DAG export for inclusion in state snapshots.
func (s *Server) SetDAG(dag *graph.DAGExport) {
	s.dagExport = dag
}

// serveIndex serves the dashboard HTML, performing the token handshake. When
// the request already carries the correct token (query param on the
// server-opened URL, or a previously-set cookie) the token is injected into the
// page as window.__VXD_TOKEN__ and persisted in a cookie so reloads stay
// authenticated. Requests without the token receive the page with an empty
// token and the dashboard surfaces an "unauthorized" banner — they cannot read
// the secret, so an unrelated local process cannot harvest it by fetching "/".
func (s *Server) serveIndex(w http.ResponseWriter, r *http.Request, staticFS fs.FS) {
	raw, err := fs.ReadFile(staticFS, "index.html")
	if err != nil {
		http.Error(w, "index not found", http.StatusInternalServerError)
		return
	}

	injected := ""
	if s.validToken(r) && s.authToken != "" {
		injected = s.authToken
		http.SetCookie(w, &http.Cookie{
			Name:     tokenCookieName,
			Value:    s.authToken,
			Path:     "/",
			HttpOnly: false, // the page's JS must read it to open the WebSocket
			SameSite: http.SameSiteStrictMode,
		})
	}

	// Inject the token before </head>. JSON-encoding guards against breaking
	// out of the script context.
	tokenJSON, _ := json.Marshal(injected)
	snippet := fmt.Sprintf("<script>window.__VXD_TOKEN__=%s;</script></head>", tokenJSON)
	html := strings.Replace(string(raw), "</head>", snippet, 1)

	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	_, _ = w.Write([]byte(html))
}

func (s *Server) Start(ctx context.Context) error {
	mux := http.NewServeMux()

	// Serve static files
	staticFS, err := fs.Sub(staticFiles, "static")
	if err != nil {
		return fmt.Errorf("static files: %w", err)
	}
	fileServer := http.FileServer(http.FS(staticFS))
	// The index page performs the token handshake; all other static assets are
	// served verbatim. The token is echoed back into the page (and a cookie)
	// ONLY when the request already presents it, so an unrelated local process
	// cannot read the token by simply fetching "/".
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/" || r.URL.Path == "/index.html" {
			s.serveIndex(w, r, staticFS)
			return
		}
		fileServer.ServeHTTP(w, r)
	})

	// WebSocket endpoint
	mux.HandleFunc("/ws", s.hub.HandleWebSocket)

	// Health check endpoint (for systemd/Docker/K8s probes)
	mux.HandleFunc("/health", s.healthHandler)

	// REST API endpoints (Phase 2/3 — read-only programmatic access)
	s.registerAPIRoutes(mux)

	addr := fmt.Sprintf("localhost:%d", s.port)
	listener, err := net.Listen("tcp", addr)
	if err != nil {
		return fmt.Errorf("port %d is already in use. Try: vxd dashboard --web --port %d", s.port, s.port+1)
	}

	s.httpServer = &http.Server{Handler: mux}

	// The opened URL carries the session token so the operator's browser is
	// authenticated on first load; the page then persists it in a cookie.
	url := fmt.Sprintf("http://%s", addr)
	authedURL := url
	if s.authToken != "" {
		authedURL = fmt.Sprintf("%s/?token=%s", url, s.authToken)
	}
	log.Printf("Dashboard server running at %s", authedURL)
	if !s.NoOpen {
		openBrowser(authedURL)
	}

	// Start hub broadcast loop
	go s.hub.Run(ctx)

	// Graceful shutdown
	go func() {
		<-ctx.Done()
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()
		s.httpServer.Shutdown(shutdownCtx) //nolint:errcheck
	}()

	return s.httpServer.Serve(listener)
}

// healthHandler returns a JSON status response for liveness probes
// with operational telemetry (uptime, event counts, store stats).
// Used by systemd, Docker, Kubernetes for health checks.
//
// Method on Server (not bare function) so handler can access store state.
// The bare healthHandler is kept for backward compatibility with tests
// that don't construct a full Server.
func (s *Server) healthHandler(w http.ResponseWriter, _ *http.Request) {
	resp := buildHealthResponse(s.eventStore, s.startTime)
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(w).Encode(resp)
}

// healthHandler is the legacy bare-function form. Returns minimal status.
// New deployments use Server.healthHandler which includes telemetry.
func healthHandler(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	resp := map[string]any{
		"status":  "ok",
		"version": Version,
	}
	_ = json.NewEncoder(w).Encode(resp)
}

// buildHealthResponse assembles the rich health payload. Pure function for
// testability — no I/O beyond the event store List call.
func buildHealthResponse(es state.EventStore, startTime time.Time) map[string]any {
	resp := map[string]any{
		"status":         "ok",
		"version":        Version,
		"uptime_seconds": int(time.Since(startTime).Seconds()),
	}
	if es != nil {
		if total, err := es.Count(state.EventFilter{}); err == nil {
			resp["events_total"] = total
		}
	}
	return resp
}

func openBrowser(url string) {
	var cmd *exec.Cmd
	switch runtime.GOOS {
	case "darwin":
		cmd = exec.Command("open", url)
	case "linux":
		cmd = exec.Command("xdg-open", url)
	case "windows":
		cmd = exec.Command("cmd", "/c", "start", url)
	default:
		log.Printf("Cannot open browser on %s — open %s manually", runtime.GOOS, url)
		return
	}
	if err := cmd.Start(); err != nil {
		log.Printf("Failed to open browser: %v — open %s manually", err, url)
	}
}

