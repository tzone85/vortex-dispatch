// internal/web/ws.go
package web

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/coder/websocket"
	"github.com/coder/websocket/wsjson"

	"github.com/tzone85/vortex-dispatch/internal/state"
)

type Hub struct {
	server         *Server
	clients        map[*websocket.Conn]bool
	mu             sync.Mutex
	lastEventCount int
}

func NewHub(s *Server) *Hub {
	return &Hub{
		server:  s,
		clients: make(map[*websocket.Conn]bool),
	}
}

type WSMessage struct {
	Type    string          `json:"type"`
	Action  string          `json:"action,omitempty"`
	Payload json.RawMessage `json:"payload,omitempty"`
}

type WSResponse struct {
	Type    string      `json:"type"`
	Action  string      `json:"action,omitempty"`
	Success bool        `json:"success,omitempty"`
	Message string      `json:"message,omitempty"`
	Data    interface{} `json:"data,omitempty"`
}

// StrictSameOriginWS is the exported alias used by sibling packages
// (e.g. internal/memory) that serve their own WebSocket endpoints and
// share the dashboard's auth token. They must apply the same Origin
// check to avoid the same cross-port CSRF described below.
func StrictSameOriginWS(r *http.Request) error { return strictSameOrigin(r) }

// strictSameOrigin rejects WebSocket upgrades whose Origin header points
// to a different host:port than the dashboard's own listener. Without
// this check, the prior `OriginPatterns: []string{"localhost:*", ...}`
// rule accepted any localhost port — so a page running at
// http://localhost:3000 (any local dev server, port-forwarded tunnel,
// or proxy a developer happens to have running) could open
// ws://localhost:8080/ws with credentials:'include'. The browser's
// SameSite=Strict cookie attribute does NOT block this because
// `localhost` is a single registrable site, so the cookie ships across
// ports — yielding cross-port CSRF into mutating WS commands
// (pause/resume/retry/reassign/escalate/kill_agent/edit_story).
//
// The fix: in addition to the auth cookie, require the Origin host to
// equal `r.Host` (the dashboard's own host:port). The OriginPatterns in
// websocket.Accept is left intentionally permissive so non-browser
// clients (curl, native dashboard, tests) without an Origin still work
// — those clients must still authenticate via Bearer header.
//
// Empty Origin (non-browser clients): allowed. The auth middleware has
// already validated Bearer/cookie before this code runs.
func strictSameOrigin(r *http.Request) error {
	origin := r.Header.Get("Origin")
	if origin == "" {
		return nil
	}
	u, err := url.Parse(origin)
	if err != nil {
		return fmt.Errorf("malformed Origin %q: %w", origin, err)
	}
	if u.Host == "" {
		return fmt.Errorf("origin %q has no host component", origin)
	}
	if !strings.EqualFold(u.Host, r.Host) {
		return fmt.Errorf("origin host %q does not match dashboard host %q (cross-origin WebSocket rejected)", u.Host, r.Host)
	}
	return nil
}

func (h *Hub) HandleWebSocket(w http.ResponseWriter, r *http.Request) {
	// Enforce strict same-origin BEFORE upgrade. Returning 403 keeps the
	// rejection at the HTTP layer so the malicious page sees a real
	// failure rather than a "connection established but commands fail"
	// gray zone that could mask the attack.
	if err := strictSameOrigin(r); err != nil {
		log.Printf("[ws] reject upgrade: %v", err)
		http.Error(w, "forbidden: cross-origin WebSocket not permitted", http.StatusForbidden)
		return
	}

	conn, err := websocket.Accept(w, r, &websocket.AcceptOptions{
		// OriginPatterns still narrows the browser-side check by
		// host shape — InsecureSkipVerify would disable the library's
		// own guard and rely entirely on strictSameOrigin above. We
		// want both layers.
		OriginPatterns: []string{"localhost:*", "127.0.0.1:*"},
	})
	if err != nil {
		log.Printf("[ws] accept error: %v", err)
		return
	}
	defer func() { _ = conn.CloseNow() }() // best-effort connection close

	h.addClient(conn)
	defer h.removeClient(conn)

	// Send initial state
	h.sendState(r.Context(), conn)

	// Read commands
	for {
		var msg WSMessage
		err := wsjson.Read(r.Context(), conn, &msg)
		if err != nil {
			break
		}
		if msg.Type == "command" {
			result := h.server.HandleCommand(msg.Action, msg.Payload)
			wsjson.Write(r.Context(), conn, result) //nolint:errcheck
		}
	}
}

func (h *Hub) Run(ctx context.Context) {
	ticker := time.NewTicker(2 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			h.closeAll()
			return
		case <-ticker.C:
			h.broadcast(ctx)
		}
	}
}

func (h *Hub) broadcast(ctx context.Context) {
	// Event diff: detect and push new events before state snapshot.
	// FileStore.List reads from the start of the file, so we fetch ALL
	// events and slice from the previous offset to get only the new ones.
	allEvents, _ := h.server.eventStore.List(state.EventFilter{})
	currentCount := len(allEvents)
	if currentCount > h.lastEventCount && h.lastEventCount > 0 {
		newEvents := allEvents[h.lastEventCount:]
		for _, evt := range newEvents {
			evtMsg := WSResponse{Type: "event", Data: EventSummary{
				Type:      string(evt.Type),
				Timestamp: evt.Timestamp.Format("15:04:05"),
				AgentID:   evt.AgentID,
				StoryID:   evt.StoryID,
			}}
			h.mu.Lock()
			for conn := range h.clients {
				wsjson.Write(ctx, conn, evtMsg) //nolint:errcheck
			}
			h.mu.Unlock()
		}
	}
	h.lastEventCount = currentCount

	// Full state snapshot
	snap, err := h.server.BuildSnapshot()
	if err != nil {
		return
	}

	msg := WSResponse{Type: "state", Data: snap}

	h.mu.Lock()
	defer h.mu.Unlock()

	for conn := range h.clients {
		if err := wsjson.Write(ctx, conn, msg); err != nil {
			_ = conn.CloseNow() // best-effort; we're already removing the client
			delete(h.clients, conn)
		}
	}
}

func (h *Hub) sendState(ctx context.Context, conn *websocket.Conn) {
	snap, err := h.server.BuildSnapshot()
	if err != nil {
		return
	}
	wsjson.Write(ctx, conn, WSResponse{Type: "state", Data: snap}) //nolint:errcheck
}

func (h *Hub) addClient(conn *websocket.Conn) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.clients[conn] = true
	log.Printf("[ws] client connected (%d total)", len(h.clients))
}

func (h *Hub) removeClient(conn *websocket.Conn) {
	h.mu.Lock()
	defer h.mu.Unlock()
	delete(h.clients, conn)
	log.Printf("[ws] client disconnected (%d remaining)", len(h.clients))
}

func (h *Hub) closeAll() {
	h.mu.Lock()
	defer h.mu.Unlock()
	for conn := range h.clients {
		_ = conn.Close(websocket.StatusGoingAway, "server shutting down") // best-effort on shutdown
		delete(h.clients, conn)
	}
}
