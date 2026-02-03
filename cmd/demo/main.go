package main

import (
	"context"
	"fmt"
	"log"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/Agnikulu/WikiSurge/internal/config"
	"github.com/Agnikulu/WikiSurge/internal/metrics"
)

func main() {
	// Load configuration
	configPath := "configs/config.dev.yaml"
	if len(os.Args) > 1 {
		configPath = os.Args[1]
	}

	cfg, err := config.LoadConfig(configPath)
	if err != nil {
		log.Fatalf("Failed to load configuration: %v", err)
	}

	fmt.Printf("Configuration loaded successfully!\n")
	fmt.Printf("Features: ES=%v, Trending=%v, EditWars=%v, WebSocket=%v\n",
		cfg.Features.ElasticsearchIndexing,
		cfg.Features.Trending,
		cfg.Features.EditWars,
		cfg.Features.Websockets)
	fmt.Printf("API Port: %d, Rate Limit: %d\n", cfg.API.Port, cfg.API.RateLimit)
	fmt.Printf("Redis: %s (Max Memory: %s)\n", cfg.Redis.URL, cfg.Redis.MaxMemory)
	fmt.Printf("Hot Pages Tracked: %d\n", cfg.Redis.HotPages.MaxTracked)
	fmt.Printf("Logging: %s level, %s format\n", cfg.Logging.Level, cfg.Logging.Format)

	// Start metrics server
	metricsServer := metrics.NewServer(2112)
	if err := metricsServer.Start(); err != nil {
		log.Fatalf("Failed to start metrics server: %v", err)
	}

	fmt.Printf("Metrics server started on port 2112\n")
	fmt.Printf("Visit http://localhost:2112/metrics to see metrics\n")

	// Simulate some metrics
	fmt.Printf("Simulating metrics...\n")
	
	// Increment some counters
	for i := 0; i < 5; i++ {
		metrics.IncrementCounter("edits_ingested_total", map[string]string{})
		metrics.IncrementCounter("edits_processed_total", map[string]string{"consumer": "indexer"})
		metrics.SetGauge("hot_pages_tracked", float64(100+i*10), map[string]string{})
		metrics.ObserveHistogram("processing_duration_seconds", float64(i)*0.1, map[string]string{"consumer": "indexer"})
		time.Sleep(1 * time.Second)
	}

	fmt.Printf("Metrics simulation complete. Check http://localhost:2112/metrics\n")

	// Set up signal handling for graceful shutdown
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)

	fmt.Printf("Press Ctrl+C to stop...\n")
	<-sigChan

	fmt.Printf("Shutting down...\n")
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	if err := metricsServer.Stop(ctx); err != nil {
		log.Printf("Error stopping metrics server: %v", err)
	}

	fmt.Printf("Shutdown complete\n")
}