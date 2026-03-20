package api

import (
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"strings"

	"github.com/aichatlog/aichatlog/server/internal/storage"
)

// Handler is the main HTTP handler for the aichatlog-server API.
type Handler struct {
	store *storage.Store
	token string
	mux   *http.ServeMux
}

// NewHandler creates a new API handler.
func NewHandler(store *storage.Store, token string) *Handler {
	h := &Handler{store: store, token: token}
	mux := http.NewServeMux()

	mux.HandleFunc("GET /api/health", h.handleHealth)
	mux.HandleFunc("GET /api/conversations", h.handleListConversations)
	mux.HandleFunc("GET /api/conversations/{id}", h.handleGetConversation)
	mux.HandleFunc("POST /api/conversations", h.handleCreateConversation)
	mux.HandleFunc("GET /api/stats", h.handleStats)

	h.mux = mux
	return h
}

func (h *Handler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	// CORS
	w.Header().Set("Access-Control-Allow-Origin", "*")
	w.Header().Set("Access-Control-Allow-Methods", "GET, POST, OPTIONS")
	w.Header().Set("Access-Control-Allow-Headers", "Content-Type, Authorization")

	if r.Method == "OPTIONS" {
		w.WriteHeader(200)
		return
	}

	// Auth check
	if h.token != "" && r.URL.Path != "/api/health" {
		auth := r.Header.Get("Authorization")
		if !strings.HasPrefix(auth, "Bearer ") || strings.TrimPrefix(auth, "Bearer ") != h.token {
			jsonError(w, "Unauthorized", http.StatusUnauthorized)
			return
		}
	}

	h.mux.ServeHTTP(w, r)
}

func (h *Handler) handleHealth(w http.ResponseWriter, r *http.Request) {
	jsonResponse(w, map[string]string{"status": "ok", "service": "aichatlog-server"})
}

func (h *Handler) handleCreateConversation(w http.ResponseWriter, r *http.Request) {
	var conv storage.ConversationObject
	if err := json.NewDecoder(r.Body).Decode(&conv); err != nil {
		jsonError(w, "Invalid JSON: "+err.Error(), http.StatusBadRequest)
		return
	}

	if conv.SessionID == "" {
		jsonError(w, "session_id is required", http.StatusBadRequest)
		return
	}

	if err := h.store.Upsert(&conv); err != nil {
		log.Printf("Error upserting conversation %s: %v", conv.SessionID, err)
		jsonError(w, "Internal error", http.StatusInternalServerError)
		return
	}

	sidPreview := conv.SessionID
	if len(sidPreview) > 8 {
		sidPreview = sidPreview[:8]
	}
	log.Printf("Received conversation: %s (%s) [%s] from %s", conv.Title, sidPreview, conv.Source, conv.Device)
	jsonResponse(w, map[string]interface{}{
		"ok":         true,
		"session_id": conv.SessionID,
		"message":    "Conversation received",
	})
}

func (h *Handler) handleListConversations(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	params := storage.QueryParams{
		Status:  q.Get("status"),
		Query:   q.Get("q"),
		Project: q.Get("project"),
		Source:  q.Get("source"),
		Sort:    q.Get("sort"),
		Order:   q.Get("order"),
		Limit:   intParam(q.Get("limit"), 200),
		Offset:  intParam(q.Get("offset"), 0),
	}

	conversations, err := h.store.List(params)
	if err != nil {
		log.Printf("Error listing conversations: %v", err)
		jsonError(w, "Internal error", http.StatusInternalServerError)
		return
	}

	if conversations == nil {
		conversations = []storage.ConversationRow{}
	}
	jsonResponse(w, conversations)
}

func (h *Handler) handleGetConversation(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	conv, err := h.store.Get(id)
	if err != nil {
		jsonError(w, "Internal error", http.StatusInternalServerError)
		return
	}
	if conv == nil {
		jsonError(w, "Not found", http.StatusNotFound)
		return
	}

	// Include raw JSON if requested
	if r.URL.Query().Get("full") == "true" {
		raw, err := h.store.GetRawJSON(id)
		if err == nil && raw != "" {
			var full map[string]interface{}
			json.Unmarshal([]byte(raw), &full)
			jsonResponse(w, full)
			return
		}
	}

	jsonResponse(w, conv)
}

func (h *Handler) handleStats(w http.ResponseWriter, r *http.Request) {
	stats, err := h.store.Stats()
	if err != nil {
		jsonError(w, "Internal error", http.StatusInternalServerError)
		return
	}
	jsonResponse(w, stats)
}

// Helper functions

func jsonResponse(w http.ResponseWriter, data interface{}) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	json.NewEncoder(w).Encode(data)
}

func jsonError(w http.ResponseWriter, msg string, status int) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(map[string]interface{}{"ok": false, "error": msg})
}

func intParam(s string, def int) int {
	if s == "" {
		return def
	}
	var v int
	if _, err := fmt.Sscanf(s, "%d", &v); err == nil {
		return v
	}
	return def
}
