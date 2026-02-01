package main

import (
	"log"
	"os"
	"os/signal"
	"syscall"
)

func main() {
	log.Println("Starting WikiSurge Ingestor...")
	
	// TODO: Initialize configuration
	// TODO: Setup Wikipedia SSE client
	// TODO: Setup Kafka producer
	// TODO: Start ingestion loop
	
	// Graceful shutdown
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)
	
	log.Println("Ingestor started. Press Ctrl+C to stop.")
	<-sigChan
	
	log.Println("Shutting down WikiSurge Ingestor...")
}