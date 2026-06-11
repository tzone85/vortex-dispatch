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
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
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
	TokenPath  string // path to dashboard auth token (defaults to ~/.vxd/dashboard.token); empty disables auth (NOT recommended outside tests)
}

func NewServer(es state.EventStore, ps *state.SQLiteStore, port int, filter state.ReqFilter) *Server {
	s := &Server{
		eventStore: es,
		projStore:  ps,
		port:       port,
		reqFilter:  filter,
		startTime:  time.Now(),
	}
	s.hub = NewHub(s)
	return s
}

// SetDAG sets the DAG export for inclusion in state snapshots.
func (s *Server) SetDAG(dag *graph.DAGExport) {
	s.dagExport = dag
}

func (s *Server) Start(ctx context.Context) error {
	mux := http.NewServeMux()

	// Serve static files
	staticFS, err := fs.Sub(staticFiles, "static")
	if err != nil {
		return fmt.Errorf("static files: %w", err)
	}
	mux.Handle("/", http.FileServer(http.FS(staticFS)))

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

	// Auth: persistent bearer token + a per-process single-use bootstrap
	// nonce. Browser auto-open carries only the nonce in its URL; the
	// nonce is invalidated on first use, so browser history / logs /
	// shoulder surfing can't replay it. The persistent token only
	// travels in the Authorization header or the SameSite cookie.
	tokenPath := s.TokenPath
	if tokenPath == "" {
		tokenPath = defaultDashboardTokenPath()
	}
	token, err := LoadOrGenerateToken(tokenPath)
	if err != nil {
		return fmt.Errorf("dashboard token: %w", err)
	}
	nonce, err := generateToken()
	if err != nil {
		return fmt.Errorf("dashboard bootstrap nonce: %w", err)
	}
	handler := NewAuthMiddleware(AuthOptions{
		Token:          token,
		BootstrapNonce: nonce,
	})(mux)

	s.httpServer = &http.Server{Handler: handler}

	url := fmt.Sprintf("http://%s", addr)
	browserURL := fmt.Sprintf("%s/?%s=%s", url, NonceQueryParam, nonce)
	log.Printf("Dashboard server running at %s", url)
	// Print only a short prefix so the full token does not land in
	// launchd / systemd / Docker log drivers / log aggregators. The full
	// token sits at ~/.vxd/dashboard.token (mode 0o600); operators run
	// `cat ~/.vxd/dashboard.token` if they need it for curl.
	log.Printf("Dashboard auth token: %s… (full value at ~/.vxd/dashboard.token; use `Authorization: Bearer <token>`)", redactTokenForLog(token))
	if !s.NoOpen {
		openBrowser(browserURL)
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

// redactTokenForLog returns the first 8 chars of the token followed by an
// ellipsis. Keeps enough for operators to disambiguate sessions in logs
// without printing the full bearer to stdout / stderr.
func redactTokenForLog(token string) string {
	if len(token) > 8 {
		return token[:8]
	}
	return token
}

// defaultDashboardTokenPath returns the on-disk location of the bearer
// token. We persist under the user's HOME so multiple `vxd dashboard`
// invocations share one token across sessions (avoids printing a fresh
// token every start). Falls back to a temp path if HOME is unset —
// non-portable hosts shouldn't be locked out of their own dashboard.
func defaultDashboardTokenPath() string {
	home, err := os.UserHomeDir()
	if err != nil || home == "" {
		return filepath.Join(os.TempDir(), "vxd-dashboard.token")
	}
	return filepath.Join(home, ".vxd", "dashboard.token")
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

