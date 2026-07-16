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
	TokenPath  string // path to dashboard auth token (defaults to ~/.vxd/dashboard.token); empty disables auth (NOT recommended outside tests)
	// Pidfile, when non-empty, makes the server write its PID atomically on
	// listen and best-effort remove the file on shutdown. Set by the long-
	// lived daemon path (`vxd dashboard --web --pidfile=...`) so later
	// `vxd req` invocations can find and reuse the running daemon.
	Pidfile string
	// BootstrapFile, when non-empty, makes the server write its initial
	// single-use bootstrap nonce there with mode 0o600 so other CLI
	// processes can build a one-shot dashboard URL without scraping logs.
	BootstrapFile string
	// TokenTTL, when > 0, rotates the persistent dashboard token at startup
	// if the token file is older than this duration (dashboard.token_ttl_hours,
	// default 168h). Zero/negative disables rotation.
	TokenTTL time.Duration

	// rotator is captured during Start so the loopback /internal/bootstrap
	// endpoint can mint fresh per-tab nonces against the live auth state.
	rotator NonceRotator
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

	// Loopback-only internal endpoint: lets another local `vxd` process
	// (typically `vxd req`) mint a fresh single-use bootstrap nonce for a
	// new browser tab against the long-lived daemon. The handler itself
	// re-checks RemoteAddr against loopback so even if a future refactor
	// removes the auth bypass entry, off-host calls still get 403.
	mux.HandleFunc("/internal/bootstrap", s.internalBootstrapHandler)

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
	token, rotated, err := LoadOrGenerateTokenWithTTL(tokenPath, s.TokenTTL)
	if err != nil {
		return fmt.Errorf("dashboard token: %w", err)
	}
	if rotated {
		log.Printf("Dashboard token was older than %s — rotated (previous token is now invalid)", s.TokenTTL)
		s.emitTokenRotated("ttl_expired", tokenPath)
	}
	nonce, err := generateToken()
	if err != nil {
		return fmt.Errorf("dashboard bootstrap nonce: %w", err)
	}
	authMw, rotator := NewAuthMiddlewareWithRotator(AuthOptions{
		Token:          token,
		BootstrapNonce: nonce,
	})
	handler := authMw(mux)
	s.rotator = rotator

	s.httpServer = &http.Server{Handler: handler}

	// Pidfile + bootstrap-file: written BEFORE Serve starts so any caller
	// that probed /health and got 200 is guaranteed to find the artifacts
	// already in place. Both files are 0o600; the directory is mkdir'd
	// 0o700 so the bootstrap nonce isn't readable by other local users on
	// a shared host.
	if err := writeBootstrapFile(s.BootstrapFile, nonce); err != nil {
		log.Printf("dashboard bootstrap file: %v", err)
	}
	if err := writePidfile(s.Pidfile, os.Getpid()); err != nil {
		log.Printf("dashboard pidfile: %v", err)
	}

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
	go func() { // #nosec G118 -- the request-scoped ctx is already cancelled when this runs; graceful shutdown requires a fresh context
		<-ctx.Done()
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()
		s.httpServer.Shutdown(shutdownCtx) //nolint:errcheck
		removePidfileIfMine(s.Pidfile, os.Getpid())
	}()

	return s.httpServer.Serve(listener)
}

// internalBootstrapHandler serves POST /internal/bootstrap. It is exposed on
// the same mux as the dashboard but performs its own loopback gate so even
// if the auth bypass list is changed in a future refactor, off-host calls
// still get 403.
//
// Response body is a JSON object: {"bootstrap": "<32-byte hex nonce>"}.
// The nonce is single-use and tied to the live authenticator's state — once
// returned, /?bootstrap=<nonce> sets the cookie exactly once.
func (s *Server) internalBootstrapHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if !isLoopback(r.RemoteAddr) {
		http.Error(w, "forbidden", http.StatusForbidden)
		return
	}
	if s.rotator == nil {
		http.Error(w, "unavailable", http.StatusServiceUnavailable)
		return
	}
	nonce, err := s.rotator.Rotate()
	if err != nil {
		http.Error(w, "rotate failed", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]string{"bootstrap": nonce})
}

// isLoopback returns true iff remoteAddr refers to 127.0.0.0/8 or ::1.
func isLoopback(remoteAddr string) bool {
	host, _, err := net.SplitHostPort(remoteAddr)
	if err != nil {
		host = remoteAddr
	}
	ip := net.ParseIP(host)
	if ip == nil {
		return false
	}
	return ip.IsLoopback()
}

// writePidfile atomically writes pid to path with 0o600. Path "" is a no-op.
// The directory is mkdir'd 0o700 so a shared-host attacker can't observe the
// PID via dirent metadata.
func writePidfile(path string, pid int) error {
	if path == "" {
		return nil
	}
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return fmt.Errorf("mkdir pidfile dir: %w", err)
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, []byte(fmt.Sprintf("%d\n", pid)), 0o600); err != nil {
		return fmt.Errorf("write pidfile tmp: %w", err)
	}
	if err := os.Rename(tmp, path); err != nil {
		_ = os.Remove(tmp)
		return fmt.Errorf("rename pidfile: %w", err)
	}
	return nil
}

// writeBootstrapFile atomically writes nonce to path with 0o600. Path "" is
// a no-op. The bootstrap nonce gives anyone with read access to this file
// one shot at the dashboard, so the file must not be group/world-readable.
func writeBootstrapFile(path, nonce string) error {
	if path == "" {
		return nil
	}
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return fmt.Errorf("mkdir bootstrap dir: %w", err)
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, []byte(nonce+"\n"), 0o600); err != nil {
		return fmt.Errorf("write bootstrap tmp: %w", err)
	}
	if err := os.Rename(tmp, path); err != nil {
		_ = os.Remove(tmp)
		return fmt.Errorf("rename bootstrap: %w", err)
	}
	return nil
}

// removePidfileIfMine deletes path iff it currently records pid. Guards
// against racing two daemons: if another daemon overwrote the pidfile we
// don't want shutdown of the OLD process to clobber the new one's record.
func removePidfileIfMine(path string, pid int) {
	if path == "" {
		return
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return
	}
	got := strings.TrimSpace(string(data))
	if got == fmt.Sprintf("%d", pid) {
		_ = os.Remove(path)
	}
}

// healthHandler returns a minimal JSON status response for liveness
// probes. Telemetry (uptime, event counts, version) is deliberately
// withheld from this UNAUTHENTICATED endpoint — those move to
// authenticated /api/v1/metrics where bearer-token access is required.
// A liveness probe only needs "is the process up", which is what 200 OK
// already conveys.
//
// Method on Server kept (not bare function) so future authenticated
// telemetry endpoints can reach store state from the same receiver.
func (s *Server) healthHandler(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(w).Encode(map[string]string{"status": "ok"})
}

// (Dead `healthHandler` bare function and `buildHealthResponse` removed
// 2026-06-11 — both leaked version / uptime / events_total but were not
// registered on any mux. The live path is Server.healthHandler which
// returns only {"status":"ok"}. Keeping the dead code risked a future
// refactor wiring it back in by accident.)

// redactTokenForLog returns the first 8 chars of the token followed by an
// ellipsis. Keeps enough for operators to disambiguate sessions in logs
// without printing the full bearer to stdout / stderr.
func redactTokenForLog(token string) string {
	if len(token) > 8 {
		return token[:8]
	}
	return token
}

// emitTokenRotated records a DASHBOARD_TOKEN_ROTATED event (best-effort —
// a store failure logs and never blocks dashboard startup).
func (s *Server) emitTokenRotated(reason, tokenPath string) {
	if s.eventStore == nil {
		return
	}
	evt := state.NewEvent(state.EventDashboardTokenRotated, "dashboard", "", map[string]any{
		"reason":     reason,
		"token_path": tokenPath,
	})
	if err := s.eventStore.Append(evt); err != nil {
		log.Printf("dashboard token rotation event append: %v", err)
		return
	}
	if s.projStore != nil {
		if err := s.projStore.Project(evt); err != nil {
			log.Printf("dashboard token rotation event project: %v", err)
		}
	}
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

