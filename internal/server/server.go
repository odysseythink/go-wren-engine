package server

import (
	"fmt"
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
)

// Server holds the HTTP server and dependencies.
type Server struct {
	router *chi.Mux
	addr   string
}

// NewServer creates a new HTTP server.
func NewServer(addr string) *Server {
	s := &Server{
		router: chi.NewRouter(),
		addr:   addr,
	}
	s.router.Use(middleware.Logger)
	s.router.Use(middleware.Recoverer)
	s.router.Use(middleware.RequestID)
	return s
}

// Router returns the chi router for registering handlers.
func (s *Server) Router() *chi.Mux {
	return s.router
}

// Start starts the HTTP server.
func (s *Server) Start() error {
	fmt.Printf("Server starting on %s\n", s.addr)
	return http.ListenAndServe(s.addr, s.router)
}
