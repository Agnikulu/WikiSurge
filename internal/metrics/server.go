package metrics

import (
	"context"
	"fmt"
	"log"
	"net/http"
	"time"

	"github.com/prometheus/client_golang/prometheus/promhttp"
)

// Server represents the metrics HTTP server
type Server struct {
	server *http.Server
	port   int
}

// NewServer creates a new metrics server
func NewServer(port int) *Server {
	if port == 0 {
		port = 2112 // Default Prometheus metrics port
	}

	mux := http.NewServeMux()
	mux.Handle("/metrics", promhttp.Handler())
	
	server := &http.Server{
		Addr:    fmt.Sprintf(":%d", port),
		Handler: mux,
	}

	return &Server{
		server: server,
		port:   port,
	}
}

// Start starts the metrics server in a goroutine
func (s *Server) Start() error {
	log.Printf("Starting metrics server on port %d", s.port)
	
	go func() {
		if err := s.server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Printf("Metrics server failed to start: %v", err)
		}
	}()

	// Give the server a moment to start
	time.Sleep(100 * time.Millisecond)
	
	return nil
}

// Stop gracefully shuts down the metrics server
func (s *Server) Stop(ctx context.Context) error {
	log.Println("Shutting down metrics server...")
	return s.server.Shutdown(ctx)
}

// IsHealthy checks if the metrics server is responding
func (s *Server) IsHealthy() bool {
	client := &http.Client{
		Timeout: 5 * time.Second,
	}
	
	resp, err := client.Get(fmt.Sprintf("http://localhost:%d/metrics", s.port))
	if err != nil {
		return false
	}
	defer resp.Body.Close()
	
	return resp.StatusCode == http.StatusOK
}