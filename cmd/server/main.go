package main

import (
	"flag"
	"fmt"
	"log"
	"net/http"
	"os"

	"github.com/aichatlog/aichatlog/server/internal/api"
	"github.com/aichatlog/aichatlog/server/internal/storage"
)

func main() {
	port := flag.Int("port", 8080, "server port")
	dbPath := flag.String("db", "aichatlog.db", "SQLite database path")
	dataDir := flag.String("data", "data", "directory for storing markdown files")
	token := flag.String("token", "", "bearer token for authentication (optional)")
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

	store, err := storage.New(*dbPath, *dataDir)
	if err != nil {
		log.Fatalf("Failed to initialize storage: %v", err)
	}
	defer store.Close()

	handler := api.NewHandler(store, *token)

	addr := fmt.Sprintf(":%d", *port)
	log.Printf("aichatlog-server starting on %s", addr)
	log.Printf("  Database: %s", *dbPath)
	log.Printf("  Data dir: %s", *dataDir)
	if *token != "" {
		log.Printf("  Auth: Bearer token required")
	} else {
		log.Printf("  Auth: disabled (no token set)")
	}

	if err := http.ListenAndServe(addr, handler); err != nil {
		log.Fatalf("Server error: %v", err)
	}
}
