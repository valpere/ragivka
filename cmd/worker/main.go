package main

import (
	"context"
	"log"
	"os"
	"os/signal"
	"syscall"
)

// NFR-9: Deployment Modes (Worker Mode)
func main() {
	log.Println("Starting Ragivka Background Worker Mode...")

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	// 1. Load config
	// 2. Initialize pgxpool
	// 3. Setup OpenTelemetry
	// 4. Start River worker pool

	<-ctx.Done()
	log.Println("Shutting down Background Worker gracefully...")
}
