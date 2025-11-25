package http

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	"github.com/prometheus/client_golang/prometheus/promhttp"
	"go.uber.org/zap"
)

// Server provides HTTP endpoints for health checks and metrics.
type Server struct {
	httpServer *http.Server
	logger     *zap.Logger
}

// HealthResponse represents the JSON response from the health endpoint.
type HealthResponse struct {
	Healthy bool `json:"healthy"`
}

// New creates a new HTTP server with health and metrics endpoints.
func New(port int, logger *zap.Logger) *Server {
	mux := http.NewServeMux()

	s := &Server{
		httpServer: &http.Server{
			Addr:         fmt.Sprintf(":%d", port),
			Handler:      mux,
			ReadTimeout:  5 * time.Second,
			WriteTimeout: 10 * time.Second,
			IdleTimeout:  120 * time.Second,
		},
		logger: logger,
	}

	// Register endpoints
	mux.HandleFunc("/health", s.handleHealth)
	mux.Handle("/metrics", promhttp.Handler())

	return s
}

// Start begins listening for HTTP requests.
// This is a blocking call that returns when the server shuts down.
func (s *Server) Start() error {
	s.logger.Info("starting HTTP server", zap.String("addr", s.httpServer.Addr))

	if err := s.httpServer.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		return fmt.Errorf("HTTP server failed: %w", err)
	}

	return nil
}

// Shutdown gracefully shuts down the HTTP server.
func (s *Server) Shutdown(ctx context.Context) error {
	s.logger.Info("shutting down HTTP server")
	return s.httpServer.Shutdown(ctx)
}

// handleHealth returns a simple liveness check.
// Returns 200 OK with {"healthy": true} if the HTTP server is responding.
func (s *Server) handleHealth(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)

	response := HealthResponse{Healthy: true}
	if err := json.NewEncoder(w).Encode(response); err != nil {
		s.logger.Error("failed to encode health response", zap.Error(err))
	}
}
