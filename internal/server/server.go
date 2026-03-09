// Package server provides the HTTP server for BONNIE.
package server

import (
	"context"
	"encoding/json"
	"log/slog"
	"net"
	"net/http"

	"github.com/flag-ai/commons/health"
)

// Server wraps an HTTP server with health check support.
type Server struct {
	httpServer *http.Server
	logger     *slog.Logger
}

// New creates a Server that serves health checks from the given registry.
func New(addr string, logger *slog.Logger, registry *health.Registry) *Server {
	mux := http.NewServeMux()
	s := &Server{
		httpServer: &http.Server{
			Addr:    addr,
			Handler: mux,
		},
		logger: logger,
	}

	mux.HandleFunc("GET /health", s.handleHealth(registry))

	return s
}

// Start begins listening and serving. It blocks until the server stops.
func (s *Server) Start() error {
	s.logger.Info("server starting", "addr", s.httpServer.Addr)
	ln, err := net.Listen("tcp", s.httpServer.Addr)
	if err != nil {
		return err
	}
	s.logger.Info("server listening", "addr", ln.Addr().String())
	return s.httpServer.Serve(ln)
}

// Shutdown gracefully shuts down the server.
func (s *Server) Shutdown(ctx context.Context) error {
	s.logger.Info("server shutting down")
	return s.httpServer.Shutdown(ctx)
}

func (s *Server) handleHealth(registry *health.Registry) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		report := registry.RunAll(r.Context())

		w.Header().Set("Content-Type", "application/json")
		if !report.Healthy {
			w.WriteHeader(http.StatusServiceUnavailable)
		}
		if err := json.NewEncoder(w).Encode(report); err != nil {
			s.logger.Error("failed to encode health report", "error", err)
		}
	}
}
