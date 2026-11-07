package httpserver

import (
	"context"
	"log"
	"net/http"
	"time"
)

// RouteRegistrar is anything that can mount routes. The server takes a slice of
// these instead of importing concrete handlers, so infra stops depending on the
// domain modules it serves.
type RouteRegistrar interface {
	Register(mux *http.ServeMux)
}

type Server struct {
	addr   string
	server *http.Server
}

func New(addr string, registrars ...RouteRegistrar) *Server {
	mux := http.NewServeMux()
	for _, r := range registrars {
		r.Register(mux)
	}

	return &Server{
		addr: addr,
		server: &http.Server{
			Addr:              addr,
			Handler:           mux,
			ReadHeaderTimeout: 10 * time.Second,
			// No WriteTimeout: a deep-tier consult with streaming can legitimately
			// run for minutes, and a write deadline would truncate it mid-answer.
			IdleTimeout: 120 * time.Second,
		},
	}
}

func (s *Server) Start() error {
	log.Printf("listening on %s", s.addr)
	return s.server.ListenAndServe()
}

func (s *Server) Stop(ctx context.Context) error {
	return s.server.Shutdown(ctx)
}
