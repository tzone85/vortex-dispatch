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
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sort"
	"time"

	"github.com/coder/websocket"
	"github.com/coder/websocket/wsjson"

	"github.com/tzone85/vortex-dispatch/internal/web"
)

//go:embed static/*
var staticFiles embed.FS

// Server serves the memory timeline dashboard.
type Server struct {
	auditDir         string
	repoDir          string
	port             int
	httpServer       *http.Server
	opportunitiesDir string
	NoOpen           bool   // skip opening browser on start
	TokenPath        string // dashboard auth token path; empty = default ~/.vxd/dashboard.token
}

// NewServer creates a new memory dashboard server.
func NewServer(auditDir, repoDir string, port int) *Server {
	return &Server{
		auditDir:         auditDir,
		repoDir:          repoDir,
		port:             port,
		opportunitiesDir: filepath.Join(repoDir, "docs", "opportunities"),
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
	addr := fmt.Sprintf("localhost:%d", s.port)
	listener, err := net.Listen("tcp", addr)
	if err != nil {
		return fmt.Errorf("port %d is already in use. Try: vxd memory --web --port %d", s.port, s.port+1)
	}

	// Auth: persistent bearer token shared with the main dashboard, plus
	// a per-process single-use bootstrap nonce for the auto-opened
	// browser. The persistent token never appears in any URL.
	tokenPath := s.TokenPath
	if tokenPath == "" {
		tokenPath = defaultMemoryTokenPath()
	}
	token, err := web.LoadOrGenerateToken(tokenPath)
	if err != nil {
		return fmt.Errorf("memory dashboard token: %w", err)
	}
	nonce, err := web.GenerateBootstrapNonce()
	if err != nil {
		return fmt.Errorf("memory dashboard bootstrap nonce: %w", err)
	}
	handler := web.NewAuthMiddleware(web.AuthOptions{
		Token:          token,
		BootstrapNonce: nonce,
	})(s.Handler())

	s.httpServer = &http.Server{Handler: handler}

	url := fmt.Sprintf("http://%s", addr)
	browserURL := fmt.Sprintf("%s/?%s=%s", url, web.NonceQueryParam, nonce)
	log.Printf("Memory dashboard running at %s", url)
	log.Printf("Memory dashboard auth token: %s (paste with `Authorization: Bearer ...`)", token)
	if !s.NoOpen {
		openBrowser(browserURL)
	}

	go func() {
		<-ctx.Done()
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()
		s.httpServer.Shutdown(shutdownCtx) //nolint:errcheck
	}()

	return s.httpServer.Serve(listener)
}

// defaultMemoryTokenPath shares the dashboard token file with the main
// `vxd dashboard`. One token unlocks both surfaces.
func defaultMemoryTokenPath() string {
	home, err := os.UserHomeDir()
	if err != nil || home == "" {
		return filepath.Join(os.TempDir(), "vxd-dashboard.token")
	}
	return filepath.Join(home, ".vxd", "dashboard.token")
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
	Type   string  `json:"type"`
	Date   string  `json:"date,omitempty"`
	Query  string  `json:"query,omitempty"`
	Filter string  `json:"filter,omitempty"`
	Sort   string  `json:"sort,omitempty"`
	ID     string  `json:"id,omitempty"`
	Status string  `json:"status,omitempty"`
	Amount float64 `json:"amount,omitempty"`
	URL    string  `json:"url,omitempty"`
}

// ServerMessage represents a message sent to the browser.
type ServerMessage struct {
	Type              string                   `json:"type"`
	Timeline          []TimelineEntry          `json:"timeline,omitempty"`
	Range             *DateRange               `json:"range,omitempty"`
	Date              string                   `json:"date,omitempty"`
	PRs               []PRDetail               `json:"prs,omitempty"`
	Findings          []FindingDetail          `json:"findings,omitempty"`
	Commits           []CommitDetail           `json:"commits,omitempty"`
	RunSummary        *RunSummaryDetail        `json:"run_summary,omitempty"`
	Query             string                   `json:"query,omitempty"`
	Results           []SearchResult           `json:"results,omitempty"`
	Opportunities     []OpportunityDetail      `json:"opportunities,omitempty"`
	OpportunityStats  *OpportunityStatsDetail  `json:"opportunity_stats,omitempty"`
	DiscoveredSources []DiscoveredSourceDetail `json:"discovered_sources,omitempty"`
	ProposalDraft     string                   `json:"proposal_draft,omitempty"`
	Milestone         string                   `json:"milestone,omitempty"`
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
	case "list_findings":
		s.handleListFindings(ctx, conn)
	case "search":
		s.handleSearch(ctx, conn, msg.Query, msg.Date)
	case "list_opportunities":
		s.handleListOpportunities(ctx, conn, msg.Filter, msg.Sort)
	case "update_opportunity":
		s.handleUpdateOpportunity(ctx, conn, msg.ID, msg.Status)
	case "log_revenue":
		s.handleLogRevenue(ctx, conn, msg.ID, msg.Amount)
	case "approve_source":
		s.handleApproveSource(ctx, conn, msg.URL)
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

func (s *Server) handleListFindings(ctx context.Context, conn *websocket.Conn) {
	findings, err := ListFindings(s.auditDir)
	if err != nil {
		log.Printf("[memory/ws] list findings: %v", err)
		return
	}

	resp := ServerMessage{
		Type:     "findings_library",
		Findings: findings,
	}
	wsjson.Write(ctx, conn, resp) //nolint:errcheck
}

// searchTimeout caps how long handleSearch waits for MemPalace. Bounded so the
// WebSocket client always receives a `search_results` frame before its own
// deadline elapses, even if MemPalace is slow or unresponsive.
const searchTimeout = 2 * time.Second

// handleSearch runs a MemPalace search and returns results. The search is
// bounded by searchTimeout so a slow index never blocks the WebSocket loop;
// on timeout/error an empty result set is returned to the client.
func (s *Server) handleSearch(ctx context.Context, conn *websocket.Conn, query, _ string) {
	searchCtx, cancel := context.WithTimeout(ctx, searchTimeout)
	defer cancel()

	results, err := SearchMemPalace(searchCtx, query)
	if err != nil {
		log.Printf("[memory/ws] mempalace search: %v", err)
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

func (s *Server) handleListOpportunities(ctx context.Context, conn *websocket.Conn, filter, sortBy string) {
	opps, err := LoadOpportunities(s.opportunitiesDir)
	if err != nil {
		log.Printf("[memory/ws] load opportunities: %v", err)
		return
	}

	// Filter
	if filter != "" && filter != "all" {
		var filtered []OpportunityDetail
		for _, o := range opps {
			if o.Status == filter {
				filtered = append(filtered, o)
			}
		}
		opps = filtered
	}

	// Sort by rank descending (default)
	sort.Slice(opps, func(i, j int) bool {
		return opps[i].Rank > opps[j].Rank
	})

	revenue := LoadTotalRevenue(s.opportunitiesDir)
	stats := ComputeOpportunityStats(opps, revenue)

	sources, _ := LoadDiscoveredSources(s.opportunitiesDir)

	resp := ServerMessage{
		Type:              "opportunities",
		Opportunities:     opps,
		OpportunityStats:  &stats,
		DiscoveredSources: sources,
	}
	wsjson.Write(ctx, conn, resp) //nolint:errcheck
}

func (s *Server) handleUpdateOpportunity(ctx context.Context, conn *websocket.Conn, id, status string) {
	pipelinePath := filepath.Join(s.opportunitiesDir, "pipeline.jsonl")
	opps, err := LoadOpportunities(s.opportunitiesDir)
	if err != nil {
		log.Printf("[memory/ws] load for update: %v", err)
		return
	}

	f, err := os.Create(pipelinePath)
	if err != nil {
		log.Printf("[memory/ws] create pipeline: %v", err)
		return
	}
	defer f.Close()

	for _, opp := range opps {
		if opp.ID == id {
			opp.Status = status
		}
		data, _ := json.Marshal(opp)
		f.Write(append(data, '\n'))
	}
	f.Sync()

	// Send updated list
	s.handleListOpportunities(ctx, conn, "", "rank")
}

func (s *Server) handleLogRevenue(ctx context.Context, conn *websocket.Conn, id string, amount float64) {
	revenuePath := filepath.Join(s.opportunitiesDir, "revenue.jsonl")

	existingTotal := LoadTotalRevenue(s.opportunitiesDir)
	newTotal := existingTotal + amount

	entry := struct {
		OpportunityID   string  `json:"opportunity_id"`
		Amount          float64 `json:"amount"`
		Currency        string  `json:"currency"`
		Date            string  `json:"date"`
		Status          string  `json:"status"`
		CumulativeTotal float64 `json:"cumulative_total"`
	}{
		OpportunityID:   id,
		Amount:          amount,
		Currency:        "USD",
		Date:            time.Now().Format("2006-01-02"),
		Status:          "received",
		CumulativeTotal: newTotal,
	}

	os.MkdirAll(filepath.Dir(revenuePath), 0o755)
	f, err := os.OpenFile(revenuePath, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		log.Printf("[memory/ws] open revenue: %v", err)
		return
	}
	data, _ := json.Marshal(entry)
	f.Write(append(data, '\n'))
	f.Close()

	// Update opportunity status to won
	s.handleUpdateOpportunity(ctx, conn, id, "won")

	// Check milestone
	milestones := []float64{1000, 5000, 10000, 25000, 50000, 100000, 250000, 500000, 1000000}
	var milestone string
	for _, m := range milestones {
		if newTotal >= m && existingTotal < m {
			milestone = fmt.Sprintf("$%.0f", m)
		}
	}

	if milestone != "" {
		resp := ServerMessage{
			Type:      "revenue_update",
			Milestone: milestone,
		}
		wsjson.Write(ctx, conn, resp) //nolint:errcheck
	}
}

func (s *Server) handleApproveSource(ctx context.Context, conn *websocket.Conn, url string) {
	sourcesPath := filepath.Join(s.opportunitiesDir, "discovered_sources.jsonl")
	sources, err := LoadDiscoveredSources(s.opportunitiesDir)
	if err != nil {
		log.Printf("[memory/ws] load sources: %v", err)
		return
	}

	f, err := os.Create(sourcesPath)
	if err != nil {
		log.Printf("[memory/ws] create sources: %v", err)
		return
	}
	defer f.Close()

	for _, src := range sources {
		if src.URL == url {
			src.Status = "approved"
		}
		data, _ := json.Marshal(src)
		f.Write(append(data, '\n'))
	}
	f.Sync()

	// Send updated list
	s.handleListOpportunities(ctx, conn, "", "rank")
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
	if err := cmd.Start(); err != nil {
		log.Printf("Failed to open browser: %v — open %s manually", err, url)
	}
}
