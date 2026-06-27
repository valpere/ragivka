package main

import (
	"context"
	"log"
	"os"
	"os/signal"
	"syscall"
)

// NFR-9: Deployment Modes (API Server)
func main() {
	log.Println("Starting Ragivka API Server Mode...")

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	// 1. Load config
	// 2. Initialize pgxpool
	// 3. Setup OpenTelemetry
	// 4. Start HTTP / Webhook server

	<-ctx.Done()
	log.Println("Shutting down API Server gracefully...")
}
