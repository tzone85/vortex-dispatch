package web

import (
	"context"
	"net/http"
)

type Hub struct {
	server *Server
}

func NewHub(s *Server) *Hub {
	return &Hub{server: s}
}

func (h *Hub) HandleWebSocket(w http.ResponseWriter, r *http.Request) {
	// Stub — full implementation in Task 6
	http.Error(w, "not implemented", http.StatusNotImplemented)
}

func (h *Hub) Run(ctx context.Context) {
	// Stub — full implementation in Task 6
	<-ctx.Done()
}
