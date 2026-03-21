package api

import (
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"strings"

	"github.com/aichatlog/aichatlog/server/internal/config"
	"github.com/aichatlog/aichatlog/server/internal/storage"
)

// Extractor is the interface for manual extraction triggers.
type Extractor interface {
	ExtractOne(conversationID string) error
}

// Handler is the main HTTP handler for the aichatlog-server API.
type Handler struct {
	store     *storage.Store
	token     string
	mux       *http.ServeMux
	dashboard []byte
	cfgMgr    *config.Manager
	extractor Extractor
}

// NewHandler creates a new API handler.
func NewHandler(store *storage.Store, token string, dashboardHTML []byte, cfgMgr *config.Manager, extractor Extractor) *Handler {
	h := &Handler{store: store, token: token, dashboard: dashboardHTML, cfgMgr: cfgMgr, extractor: extractor}
	mux := http.NewServeMux()

	// Dashboard
	if len(dashboardHTML) > 0 {
		mux.HandleFunc("GET /", h.handleDashboard)
	}

	mux.HandleFunc("GET /api/health", h.handleHealth)
	mux.HandleFunc("GET /api/conversations", h.handleListConversations)
	mux.HandleFunc("GET /api/conversations/{id}", h.handleGetConversation)
	mux.HandleFunc("GET /api/conversations/{id}/messages", h.handleGetMessages)
	mux.HandleFunc("POST /api/conversations/sync", h.handleSync)
	mux.HandleFunc("POST /api/conversations", h.handleCreateConversation)
	mux.HandleFunc("POST /api/conversations/batch", h.handleBatchCreate)
	mux.HandleFunc("DELETE /api/conversations/{id}", h.handleDeleteConversation)
	mux.HandleFunc("PATCH /api/conversations/{id}", h.handleUpdateConversation)
	mux.HandleFunc("POST /api/conversations/{id}/extract", h.handleExtractConversation)
	mux.HandleFunc("POST /api/conversations/{id}/reprocess", h.handleReprocess)
	mux.HandleFunc("GET /api/stats", h.handleStats)
	mux.HandleFunc("GET /api/stats/summary", h.handleStatsSummary)
	mux.HandleFunc("GET /api/projects", h.handleListProjects)
	mux.HandleFunc("GET /api/extractions", h.handleListExtractions)
	mux.HandleFunc("GET /api/conversations/{id}/extractions", h.handleGetExtractions)
	mux.HandleFunc("GET /api/config", h.handleGetConfig)
	mux.HandleFunc("POST /api/config", h.handleUpdateConfig)

	h.mux = mux
	return h
}

func (h *Handler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	// CORS
	w.Header().Set("Access-Control-Allow-Origin", "*")
	w.Header().Set("Access-Control-Allow-Methods", "GET, POST, PATCH, DELETE, OPTIONS")
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

func (h *Handler) handleDashboard(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Write(h.dashboard)
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

func (h *Handler) handleSync(w http.ResponseWriter, r *http.Request) {
	var req storage.SyncRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		jsonError(w, "Invalid JSON: "+err.Error(), http.StatusBadRequest)
		return
	}

	if req.SessionID == "" {
		jsonError(w, "session_id is required", http.StatusBadRequest)
		return
	}

	resp, err := h.store.Sync(&req)
	if err != nil {
		log.Printf("Error syncing %s: %v", req.SessionID, err)
		jsonError(w, "Internal error", http.StatusInternalServerError)
		return
	}

	log.Printf("Sync %s: mode=%s action=%s", req.SessionID, req.SyncMode, resp.Action)
	jsonResponse(w, resp)
}

func (h *Handler) handleBatchCreate(w http.ResponseWriter, r *http.Request) {
	var convs []*storage.ConversationObject
	if err := json.NewDecoder(r.Body).Decode(&convs); err != nil {
		jsonError(w, "Invalid JSON: "+err.Error(), http.StatusBadRequest)
		return
	}

	count, err := h.store.UpsertBatch(convs)
	if err != nil {
		log.Printf("Error in batch upsert at item %d: %v", count, err)
		jsonError(w, fmt.Sprintf("Error at item %d: %s", count, err.Error()), http.StatusInternalServerError)
		return
	}

	log.Printf("Batch received: %d conversations", count)
	jsonResponse(w, map[string]interface{}{
		"ok":    true,
		"count": count,
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

	// If full=true, return conversation with messages
	if r.URL.Query().Get("full") == "true" {
		detail, err := h.store.GetDetail(id)
		if err != nil {
			jsonError(w, "Internal error", http.StatusInternalServerError)
			return
		}
		if detail == nil {
			jsonError(w, "Not found", http.StatusNotFound)
			return
		}
		jsonResponse(w, detail)
		return
	}

	conv, err := h.store.Get(id)
	if err != nil {
		jsonError(w, "Internal error", http.StatusInternalServerError)
		return
	}
	if conv == nil {
		jsonError(w, "Not found", http.StatusNotFound)
		return
	}
	jsonResponse(w, conv)
}

func (h *Handler) handleGetMessages(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")

	// Resolve conversation ID (might be session_id)
	conv, err := h.store.Get(id)
	if err != nil {
		jsonError(w, "Internal error", http.StatusInternalServerError)
		return
	}
	if conv == nil {
		jsonError(w, "Not found", http.StatusNotFound)
		return
	}

	messages, err := h.store.GetMessages(conv.ID)
	if err != nil {
		jsonError(w, "Internal error", http.StatusInternalServerError)
		return
	}
	if messages == nil {
		messages = []storage.MessageRow{}
	}
	jsonResponse(w, messages)
}

func (h *Handler) handleListExtractions(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	extractions, err := h.store.ListExtractions(
		q.Get("type"),
		intParam(q.Get("limit"), 100),
		intParam(q.Get("offset"), 0),
	)
	if err != nil {
		jsonError(w, "Internal error", http.StatusInternalServerError)
		return
	}
	if extractions == nil {
		extractions = []storage.ExtractionRow{}
	}
	jsonResponse(w, extractions)
}

func (h *Handler) handleGetExtractions(w http.ResponseWriter, r *http.Request) {
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
	extractions, err := h.store.GetExtractions(conv.ID)
	if err != nil {
		jsonError(w, "Internal error", http.StatusInternalServerError)
		return
	}
	if extractions == nil {
		extractions = []storage.ExtractionRow{}
	}
	jsonResponse(w, extractions)
}

func (h *Handler) handleDeleteConversation(w http.ResponseWriter, r *http.Request) {
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
	if err := h.store.SoftDelete(conv.ID); err != nil {
		jsonError(w, "Internal error", http.StatusInternalServerError)
		return
	}
	log.Printf("Soft-deleted conversation %s (%s)", conv.ID, conv.Title)
	jsonResponse(w, map[string]interface{}{"ok": true, "message": "Conversation deleted"})
}

func (h *Handler) handleUpdateConversation(w http.ResponseWriter, r *http.Request) {
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

	var params storage.UpdateFieldsParams
	if err := json.NewDecoder(r.Body).Decode(&params); err != nil {
		jsonError(w, "Invalid JSON: "+err.Error(), http.StatusBadRequest)
		return
	}
	if err := h.store.UpdateFields(conv.ID, params); err != nil {
		jsonError(w, "Internal error", http.StatusInternalServerError)
		return
	}
	jsonResponse(w, map[string]interface{}{"ok": true})
}

func (h *Handler) handleExtractConversation(w http.ResponseWriter, r *http.Request) {
	if h.extractor == nil {
		jsonError(w, "LLM extraction not configured", http.StatusNotImplemented)
		return
	}
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
	// Clear existing extractions to allow re-extraction
	h.store.ClearExtractions(conv.ID)
	if err := h.extractor.ExtractOne(conv.ID); err != nil {
		log.Printf("Manual extraction error for %s: %v", conv.ID, err)
		jsonError(w, "Extraction failed: "+err.Error(), http.StatusInternalServerError)
		return
	}
	jsonResponse(w, map[string]interface{}{"ok": true, "message": "Extraction complete"})
}

func (h *Handler) handleReprocess(w http.ResponseWriter, r *http.Request) {
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
	if err := h.store.UpdateStatus(conv.ID, "received"); err != nil {
		jsonError(w, "Internal error", http.StatusInternalServerError)
		return
	}
	jsonResponse(w, map[string]interface{}{"ok": true, "message": "Conversation queued for reprocessing"})
}

func (h *Handler) handleStatsSummary(w http.ResponseWriter, r *http.Request) {
	summary, err := h.store.GetStatsSummary()
	if err != nil {
		jsonError(w, "Internal error", http.StatusInternalServerError)
		return
	}
	jsonResponse(w, summary)
}

func (h *Handler) handleListProjects(w http.ResponseWriter, r *http.Request) {
	projects, err := h.store.ListProjects()
	if err != nil {
		jsonError(w, "Internal error", http.StatusInternalServerError)
		return
	}
	if projects == nil {
		projects = []storage.ProjectRow{}
	}
	jsonResponse(w, projects)
}

func (h *Handler) handleStats(w http.ResponseWriter, r *http.Request) {
	stats, err := h.store.Stats()
	if err != nil {
		jsonError(w, "Internal error", http.StatusInternalServerError)
		return
	}
	jsonResponse(w, stats)
}

func (h *Handler) handleGetConfig(w http.ResponseWriter, r *http.Request) {
	if h.cfgMgr == nil {
		jsonError(w, "Config not available", http.StatusNotImplemented)
		return
	}
	jsonResponse(w, h.cfgMgr.Get())
}

func (h *Handler) handleUpdateConfig(w http.ResponseWriter, r *http.Request) {
	if h.cfgMgr == nil {
		jsonError(w, "Config not available", http.StatusNotImplemented)
		return
	}

	var cfg config.ServerConfig
	if err := json.NewDecoder(r.Body).Decode(&cfg); err != nil {
		jsonError(w, "Invalid JSON: "+err.Error(), http.StatusBadRequest)
		return
	}

	if err := h.cfgMgr.Update(cfg); err != nil {
		log.Printf("Error updating config: %v", err)
		jsonError(w, "Failed to save config", http.StatusInternalServerError)
		return
	}

	log.Printf("Config updated (adapter=%s)", cfg.Output.Adapter)
	jsonResponse(w, map[string]interface{}{
		"ok":      true,
		"message": "Config updated. Restart server to apply output adapter changes.",
	})
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
