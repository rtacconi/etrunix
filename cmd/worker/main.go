package main

import (
	"context"
	"flag"
	"log"
	"os"
	"os/signal"
	"syscall"
	
	"etrunix/internal/worker"
	"etrunix/internal/worker/config"
)

func main() {
	log.Println("Starting Etrunix Worker...")

	// Parse command line flags
	flag.Parse()

	// Load configuration
	cfg, err := config.LoadFromEnv()
	if err != nil {
		log.Fatalf("Failed to load configuration: %v", err)
	}

	// Create worker
	w, err := worker.NewWorker(cfg)
	if err != nil {
		log.Fatalf("Failed to create worker: %v", err)
	}

	// Set up signal handling
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	signalCh := make(chan os.Signal, 1)
	signal.Notify(signalCh, syscall.SIGINT, syscall.SIGTERM)

	go func() {
		sig := <-signalCh
		log.Printf("Received signal: %v, shutting down...", sig)
		cancel()
	}()

	// Start worker
	log.Printf("Worker %s starting...", cfg.WorkerID)
	if err := w.Start(ctx); err != nil {
		log.Fatalf("Worker failed: %v", err)
	}

	// Clean shutdown
	w.Stop()
	log.Println("Worker stopped gracefully")
}
