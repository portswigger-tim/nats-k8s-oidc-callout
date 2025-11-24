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
	healthChecks HealthChecks
}

// HealthChecks holds functions to check various component health.
type HealthChecks struct {
	NatsConnected  func() bool
	K8sConnected   func() bool
	CacheInitialized func() bool
}

// HealthResponse represents the JSON response from the health endpoint.
type HealthResponse struct {
	Status string            `json:"status"`
	Checks map[string]bool   `json:"checks"`
}

// New creates a new HTTP server with health and metrics endpoints.
func New(port int, logger *zap.Logger, checks HealthChecks) *Server {
	mux := http.NewServeMux()

	s := &Server{
		httpServer: &http.Server{
			Addr:         fmt.Sprintf(":%d", port),
			Handler:      mux,
			ReadTimeout:  5 * time.Second,
			WriteTimeout: 10 * time.Second,
			IdleTimeout:  120 * time.Second,
		},
		logger:       logger,
		healthChecks: checks,
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

// handleHealth returns the health status of the service.
func (s *Server) handleHealth(w http.ResponseWriter, r *http.Request) {
	checks := map[string]bool{
		"nats_connected":    true, // Default to true for now
		"k8s_connected":     true,
		"cache_initialized": true,
	}

	// Call health check functions if they exist
	if s.healthChecks.NatsConnected != nil {
		checks["nats_connected"] = s.healthChecks.NatsConnected()
	}
	if s.healthChecks.K8sConnected != nil {
		checks["k8s_connected"] = s.healthChecks.K8sConnected()
	}
	if s.healthChecks.CacheInitialized != nil {
		checks["cache_initialized"] = s.healthChecks.CacheInitialized()
	}

	// Determine overall status
	healthy := true
	for _, v := range checks {
		if !v {
			healthy = false
			break
		}
	}

	status := "healthy"
	statusCode := http.StatusOK
	if !healthy {
		status = "unhealthy"
		statusCode = http.StatusServiceUnavailable
	}

	response := HealthResponse{
		Status: status,
		Checks: checks,
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(statusCode)

	if err := json.NewEncoder(w).Encode(response); err != nil {
		s.logger.Error("failed to encode health response", zap.Error(err))
	}
}
