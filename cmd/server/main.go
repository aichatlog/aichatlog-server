package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"

	"github.com/aichatlog/aichatlog/server/internal/api"
	"github.com/aichatlog/aichatlog/server/internal/config"
	"github.com/aichatlog/aichatlog/server/internal/llm"
	"github.com/aichatlog/aichatlog/server/internal/output"
	"github.com/aichatlog/aichatlog/server/internal/processor"
	"github.com/aichatlog/aichatlog/server/internal/storage"
	"github.com/aichatlog/aichatlog/server/web"
)

func main() {
	port := flag.Int("port", 8080, "server port")
	dbPath := flag.String("db", "aichatlog.db", "SQLite database path")
	dataDir := flag.String("data", "data", "directory for storing files")
	token := flag.String("token", "", "bearer token for authentication (optional)")
	configPath := flag.String("config", "", "path to config.json (default: <data>/config.json)")
	flag.Parse()

	// Allow env var overrides
	if v := os.Getenv("AICHATLOG_PORT"); v != "" {
		fmt.Sscanf(v, "%d", port)
	}
	if v := os.Getenv("AICHATLOG_DB"); v != "" {
		*dbPath = v
	}
	if v := os.Getenv("AICHATLOG_DATA"); v != "" {
		*dataDir = v
	}
	if v := os.Getenv("AICHATLOG_TOKEN"); v != "" {
		*token = v
	}

	if *configPath == "" {
		*configPath = filepath.Join(*dataDir, "config.json")
	}

	// Initialize storage
	store, err := storage.New(*dbPath, *dataDir)
	if err != nil {
		log.Fatalf("Failed to initialize storage: %v", err)
	}
	defer store.Close()

	// Initialize config
	cfgMgr, err := config.NewManager(*configPath)
	if err != nil {
		log.Fatalf("Failed to load config: %v", err)
	}
	cfg := cfgMgr.Get()

	// Initialize output adapter
	adapter, err := output.NewAdapter(&cfg.Output)
	if err != nil {
		log.Printf("WARNING: output adapter error: %v (processing disabled)", err)
	}

	// Initialize LLM adapter and extractor
	llmAdapter, err := llm.NewAdapter(&cfg.LLM)
	if err != nil {
		log.Printf("WARNING: LLM adapter error: %v (extraction disabled)", err)
	}
	var extractor *processor.Extractor
	if llmAdapter != nil {
		extractor = processor.NewExtractor(store, llmAdapter, adapter, &processor.ExtractorConfig{
			MinWords: cfg.LLM.MinWords,
			SyncDir:  cfg.Processor.SyncDir,
		})
		log.Printf("  LLM: %s (extraction enabled)", llmAdapter.Name())
	}

	// Initialize processor
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	proc := processor.New(store, adapter, extractor, &processor.Config{
		Interval:  time.Duration(cfg.Processor.Interval) * time.Second,
		BatchSize: cfg.Processor.BatchSize,
		SyncDir:   cfg.Processor.SyncDir,
	})

	if cfg.Processor.Enabled {
		go proc.Run(ctx)
	}

	// Initialize handler
	handler := api.NewHandler(store, *token, web.DashboardHTML, cfgMgr)

	// Start server
	addr := fmt.Sprintf(":%d", *port)
	server := &http.Server{Addr: addr, Handler: handler}

	log.Printf("aichatlog-server starting on %s", addr)
	log.Printf("  Database: %s", *dbPath)
	log.Printf("  Data dir: %s", *dataDir)
	log.Printf("  Config:   %s", *configPath)
	log.Printf("  Dashboard: http://localhost:%d", *port)
	if adapter != nil {
		log.Printf("  Output: %s", adapter.Name())
	} else {
		log.Printf("  Output: none (configure via /api/config)")
	}
	if *token != "" {
		log.Printf("  Auth: Bearer token required")
	} else {
		log.Printf("  Auth: disabled (no token set)")
	}

	// Graceful shutdown
	go func() {
		sigCh := make(chan os.Signal, 1)
		signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
		<-sigCh
		log.Printf("Shutting down...")
		cancel()
		ctx, c := context.WithTimeout(context.Background(), 5*time.Second)
		defer c()
		server.Shutdown(ctx)
	}()

	if err := server.ListenAndServe(); err != http.ErrServerClosed {
		log.Fatalf("Server error: %v", err)
	}
}
