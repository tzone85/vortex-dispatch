// internal/memory/server.go
package memory

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

	"nhooyr.io/websocket"
	"nhooyr.io/websocket/wsjson"
)

//go:embed static/*
var staticFiles embed.FS

// Server serves the memory timeline dashboard.
type Server struct {
	auditDir   string
	repoDir    string
	port       int
	httpServer *http.Server
}

// NewServer creates a new memory dashboard server.
func NewServer(auditDir, repoDir string, port int) *Server {
	return &Server{
		auditDir: auditDir,
		repoDir:  repoDir,
		port:     port,
	}
}

// Handler returns the HTTP handler for the memory dashboard.
func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()

	staticFS, err := fs.Sub(staticFiles, "static")
	if err != nil {
		log.Printf("[memory] static files error: %v", err)
		return mux
	}
	mux.Handle("/", http.FileServer(http.FS(staticFS)))
	mux.HandleFunc("/ws", s.handleWebSocket)

	return mux
}

// Start listens on the configured port, opens a browser, and blocks until ctx is cancelled.
func (s *Server) Start(ctx context.Context) error {
	handler := s.Handler()

	addr := fmt.Sprintf("localhost:%d", s.port)
	listener, err := net.Listen("tcp", addr)
	if err != nil {
		return fmt.Errorf("port %d is already in use. Try: vxd memory --web --port %d", s.port, s.port+1)
	}

	s.httpServer = &http.Server{Handler: handler}

	url := fmt.Sprintf("http://%s", addr)
	log.Printf("Memory dashboard running at %s", url)
	openBrowser(url)

	go func() {
		<-ctx.Done()
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()
		s.httpServer.Shutdown(shutdownCtx) //nolint:errcheck
	}()

	return s.httpServer.Serve(listener)
}

// handleWebSocket handles WebSocket connections for the memory dashboard.
func (s *Server) handleWebSocket(w http.ResponseWriter, r *http.Request) {
	conn, err := websocket.Accept(w, r, &websocket.AcceptOptions{
		OriginPatterns: []string{"localhost:*", "127.0.0.1:*"},
	})
	if err != nil {
		log.Printf("[memory/ws] accept error: %v", err)
		return
	}
	defer conn.CloseNow()

	// Send initial timeline data
	s.sendInit(r.Context(), conn)

	// Read client messages
	for {
		var msg ClientMessage
		err := wsjson.Read(r.Context(), conn, &msg)
		if err != nil {
			break
		}
		s.handleMessage(r.Context(), conn, msg)
	}
}

// ClientMessage represents a message from the browser.
type ClientMessage struct {
	Type  string `json:"type"`
	Date  string `json:"date,omitempty"`
	Query string `json:"query,omitempty"`
}

// ServerMessage represents a message sent to the browser.
type ServerMessage struct {
	Type       string          `json:"type"`
	Timeline   []TimelineEntry `json:"timeline,omitempty"`
	Range      *DateRange      `json:"range,omitempty"`
	Date       string          `json:"date,omitempty"`
	PRs        []PRDetail      `json:"prs,omitempty"`
	Findings   []FindingDetail `json:"findings,omitempty"`
	Commits    []CommitDetail  `json:"commits,omitempty"`
	RunSummary *RunSummaryDetail `json:"run_summary,omitempty"`
	Query      string           `json:"query,omitempty"`
	Results    []SearchResult   `json:"results,omitempty"`
}

// DateRange holds the min/max/today for the timeline slider.
type DateRange struct {
	Min   string `json:"min"`
	Max   string `json:"max"`
	Today string `json:"today"`
}

// sendInit sends the initial timeline data to a newly connected client.
func (s *Server) sendInit(ctx context.Context, conn *websocket.Conn) {
	tl, err := BuildTimeline(s.auditDir)
	if err != nil {
		log.Printf("[memory/ws] build timeline: %v", err)
		return
	}

	msg := ServerMessage{
		Type:     "init",
		Timeline: tl.Entries,
		Range: &DateRange{
			Min:   tl.Min,
			Max:   tl.Max,
			Today: tl.Today,
		},
	}
	wsjson.Write(ctx, conn, msg) //nolint:errcheck
}

// handleMessage dispatches a client message to the appropriate handler.
func (s *Server) handleMessage(ctx context.Context, conn *websocket.Conn, msg ClientMessage) {
	switch msg.Type {
	case "select_date":
		s.handleSelectDate(ctx, conn, msg.Date)
	case "search":
		s.handleSearch(ctx, conn, msg.Query, msg.Date)
	default:
		log.Printf("[memory/ws] unknown message type: %s", msg.Type)
	}
}

// handleSelectDate returns detailed data for a specific date.
func (s *Server) handleSelectDate(ctx context.Context, conn *websocket.Conn, date string) {
	dd, err := GetDayDetail(s.auditDir, date)
	if err != nil {
		log.Printf("[memory/ws] get day detail: %v", err)
		return
	}

	commits := GetCommitsForDate(s.repoDir, date)

	resp := ServerMessage{
		Type:       "day_detail",
		Date:       date,
		PRs:        dd.PRs,
		Findings:   dd.Findings,
		Commits:    commits,
		RunSummary: dd.RunSummary,
	}
	wsjson.Write(ctx, conn, resp) //nolint:errcheck
}

// handleSearch runs a MemPalace search and returns results.
func (s *Server) handleSearch(ctx context.Context, conn *websocket.Conn, query, date string) {
	results, err := SearchMemPalace(query)
	if err != nil {
		log.Printf("[memory/ws] mempalace search: %v", err)
		// Return empty results on error
		results = nil
	}

	resp := ServerMessage{
		Type:    "search_results",
		Query:   query,
		Results: results,
	}
	wsjson.Write(ctx, conn, resp) //nolint:errcheck
}

// MarshalInit builds the init message as JSON bytes (useful for testing).
func (s *Server) MarshalInit() (json.RawMessage, error) {
	tl, err := BuildTimeline(s.auditDir)
	if err != nil {
		return nil, err
	}
	msg := ServerMessage{
		Type:     "init",
		Timeline: tl.Entries,
		Range: &DateRange{
			Min:   tl.Min,
			Max:   tl.Max,
			Today: tl.Today,
		},
	}
	return json.Marshal(msg)
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
		log.Printf("Cannot open browser on %s -- open %s manually", runtime.GOOS, url)
		return
	}
	cmd.Start() //nolint:errcheck
}
