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
	"time"

	"github.com/tzone85/vortex-dispatch/internal/graph"
	"github.com/tzone85/vortex-dispatch/internal/state"
)

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
}

func NewServer(es state.EventStore, ps *state.SQLiteStore, port int, filter state.ReqFilter) *Server {
	s := &Server{
		eventStore: es,
		projStore:  ps,
		port:       port,
		reqFilter:  filter,
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
	mux.HandleFunc("/health", healthHandler)

	addr := fmt.Sprintf("localhost:%d", s.port)
	listener, err := net.Listen("tcp", addr)
	if err != nil {
		return fmt.Errorf("port %d is already in use. Try: vxd dashboard --web --port %d", s.port, s.port+1)
	}

	s.httpServer = &http.Server{Handler: mux}

	// Open browser
	url := fmt.Sprintf("http://%s", addr)
	log.Printf("Dashboard server running at %s", url)
	openBrowser(url)

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

// healthHandler returns a JSON status response for liveness probes.
// Used by systemd, Docker, Kubernetes for health checks.
func healthHandler(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	resp := map[string]string{
		"status":  "ok",
		"version": "0.1.0",
	}
	_ = json.NewEncoder(w).Encode(resp)
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
	cmd.Start() //nolint:errcheck
}

