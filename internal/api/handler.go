package api

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/aichatlog/aichatlog/server/internal/config"
	"github.com/aichatlog/aichatlog/server/internal/storage"
)

// Extractor is the interface for manual extraction triggers.
type Extractor interface {
	ExtractOne(conversationID string) error
}

// ExtractorFactory creates an Extractor from LLM config. Used for hot-reload.
type ExtractorFactory func(llmCfg *config.ServerConfig) Extractor

// Handler is the main HTTP handler for the aichatlog-server API.
type Handler struct {
	store            *storage.Store
	token            string
	mux              *http.ServeMux
	dashboard        []byte
	cfgMgr           *config.Manager
	extractor        Extractor
	extractorFactory ExtractorFactory
	mu               sync.RWMutex
}

// NewHandler creates a new API handler.
func NewHandler(store *storage.Store, token string, dashboardHTML []byte, faviconICO []byte, cfgMgr *config.Manager, extractor Extractor, extractorFactory ExtractorFactory) *Handler {
	h := &Handler{store: store, token: token, dashboard: dashboardHTML, cfgMgr: cfgMgr, extractor: extractor, extractorFactory: extractorFactory}
	mux := http.NewServeMux()

	// Dashboard
	if len(dashboardHTML) > 0 {
		mux.HandleFunc("GET /", h.handleDashboard)
	}
	if len(faviconICO) > 0 {
		mux.HandleFunc("GET /favicon.ico", func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "image/x-icon")
			w.Header().Set("Cache-Control", "public, max-age=86400")
			w.Write(faviconICO)
		})
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
	mux.HandleFunc("GET /api/timeline", h.handleTimeline)
	mux.HandleFunc("POST /api/llm/test", h.handleLLMTest)
	mux.HandleFunc("POST /api/llm/models", h.handleLLMModels)

	// Auth endpoints
	mux.HandleFunc("GET /api/auth/status", h.handleAuthStatus)
	mux.HandleFunc("POST /api/auth/setup", h.handleAuthSetup)
	mux.HandleFunc("POST /api/auth/login", h.handleAuthLogin)
	mux.HandleFunc("POST /api/auth/logout", h.handleAuthLogout)
	mux.HandleFunc("GET /api/keys", h.handleListKeys)
	mux.HandleFunc("POST /api/keys", h.handleCreateKey)
	mux.HandleFunc("DELETE /api/keys/{id}", h.handleDeleteKey)
	mux.HandleFunc("GET /api/users", h.handleListUsers)
	mux.HandleFunc("POST /api/users", h.handleCreateUser)
	mux.HandleFunc("DELETE /api/users/{id}", h.handleDeleteUser)

	h.mux = mux
	return h
}

// authWhitelist paths that never require authentication.
var authWhitelist = map[string]bool{
	"/api/health":      true,
	"/api/auth/status": true,
	"/api/auth/setup":  true,
	"/api/auth/login":  true,
}

// contextKey is a custom type for request context keys.
type contextKey string

const ctxUser contextKey = "user"

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
	path := r.URL.Path

	// Whitelist: always allow
	if authWhitelist[path] || path == "/" || path == "/favicon.ico" {
		h.mux.ServeHTTP(w, r)
		return
	}

	// Check if user system is set up
	hasUsers, _ := h.store.HasAnyUser()
	if !hasUsers {
		// No users yet: if legacy token is set, use that; otherwise allow all
		if h.token == "" {
			h.mux.ServeHTTP(w, r)
			return
		}
		auth := r.Header.Get("Authorization")
		if strings.HasPrefix(auth, "Bearer ") && strings.TrimPrefix(auth, "Bearer ") == h.token {
			h.mux.ServeHTTP(w, r)
			return
		}
		jsonError(w, "Unauthorized", http.StatusUnauthorized)
		return
	}

	// Try authenticate: Bearer token (API key or legacy token)
	if auth := r.Header.Get("Authorization"); strings.HasPrefix(auth, "Bearer ") {
		key := strings.TrimPrefix(auth, "Bearer ")

		// Legacy token fallback
		if h.token != "" && key == h.token {
			h.mux.ServeHTTP(w, r)
			return
		}

		// API key
		if user, err := h.store.ValidateAPIKey(key); err == nil {
			ctx := r.Context()
			r = r.WithContext(context.WithValue(ctx, ctxUser, user))
			h.mux.ServeHTTP(w, r)
			return
		}
	}

	// Try authenticate: Session cookie
	if cookie, err := r.Cookie("aichatlog_session"); err == nil {
		if user, err := h.store.ValidateSession(cookie.Value); err == nil {
			h.store.RefreshSession(cookie.Value)
			ctx := r.Context()
			r = r.WithContext(context.WithValue(ctx, ctxUser, user))
			h.mux.ServeHTTP(w, r)
			return
		}
	}

	jsonError(w, "Unauthorized", http.StatusUnauthorized)
}

// getUser returns the authenticated user from request context, or nil.
func getUser(r *http.Request) *storage.User {
	if u, ok := r.Context().Value(ctxUser).(*storage.User); ok {
		return u
	}
	return nil
}

// requireAdmin checks that the authenticated user has admin role.
func requireAdmin(w http.ResponseWriter, r *http.Request) *storage.User {
	u := getUser(r)
	if u == nil || u.Role != "admin" {
		jsonError(w, "Admin access required", http.StatusForbidden)
		return nil
	}
	return u
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

	// When searching, attach matching turn snippets
	if params.Query != "" {
		snippetMap := h.store.SearchSnippets(params.Query, params.Limit*3)
		if len(snippetMap) > 0 {
			type convWithSnippets struct {
				storage.ConversationRow
				Snippets []storage.TurnSnippet `json:"snippets,omitempty"`
			}
			results := make([]convWithSnippets, len(conversations))
			for i, c := range conversations {
				results[i] = convWithSnippets{ConversationRow: c, Snippets: snippetMap[c.ID]}
			}
			jsonResponse(w, results)
			return
		}
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
	h.mu.RLock()
	ext := h.extractor
	h.mu.RUnlock()
	if ext == nil {
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
	if err := ext.ExtractOne(conv.ID); err != nil {
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

func (h *Handler) handleTimeline(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	limit := intParam(q.Get("limit"), 50)
	offset := intParam(q.Get("offset"), 0)
	turns, err := h.store.ListTurns(limit, offset)
	if err != nil {
		log.Printf("Error listing turns: %v", err)
		jsonError(w, "Internal error", http.StatusInternalServerError)
		return
	}
	if turns == nil {
		turns = []storage.TurnRow{}
	}
	jsonResponse(w, turns)
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

	// Hot-reload LLM extractor
	if h.extractorFactory != nil {
		newExt := h.extractorFactory(&cfg)
		h.mu.Lock()
		h.extractor = newExt
		h.mu.Unlock()
		if newExt != nil {
			log.Printf("LLM extractor reloaded (adapter=%s)", cfg.LLM.Adapter)
		} else {
			log.Printf("LLM extractor disabled")
		}
	}

	log.Printf("Config updated (adapter=%s)", cfg.Output.Adapter)
	jsonResponse(w, map[string]interface{}{
		"ok":      true,
		"message": "Config updated. LLM changes applied immediately.",
	})
}

// llmTestRequest is the request body for POST /api/llm/test and /api/llm/models.
type llmTestRequest struct {
	Adapter string `json:"adapter"`
	BaseURL string `json:"base_url"`
	APIKey  string `json:"api_key"`
	Model   string `json:"model"`
}

// normalizeLLMBaseURL strips trailing slashes and /v1 suffix to get the raw host URL.
func normalizeLLMBaseURL(raw string) string {
	u := strings.TrimRight(raw, "/")
	u = strings.TrimSuffix(u, "/v1")
	u = strings.TrimRight(u, "/")
	return u
}

func (h *Handler) handleLLMTest(w http.ResponseWriter, r *http.Request) {
	var req llmTestRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		jsonError(w, "Invalid JSON: "+err.Error(), http.StatusBadRequest)
		return
	}

	client := &http.Client{Timeout: 10 * time.Second}
	var testURL string
	var httpReq *http.Request
	var err error

	switch req.Adapter {
	case "ollama":
		base := normalizeLLMBaseURL(req.BaseURL)
		if base == "" {
			base = "http://localhost:11434"
		}
		testURL = base + "/api/tags"
		httpReq, err = http.NewRequest("GET", testURL, nil)
	case "openai":
		base := normalizeLLMBaseURL(req.BaseURL)
		if base == "" {
			base = "https://api.openai.com"
		}
		testURL = base + "/v1/models"
		httpReq, err = http.NewRequest("GET", testURL, nil)
		if err == nil && req.APIKey != "" {
			httpReq.Header.Set("Authorization", "Bearer "+req.APIKey)
		}
	case "anthropic":
		testURL = "https://api.anthropic.com/v1/models"
		httpReq, err = http.NewRequest("GET", testURL, nil)
		if err == nil {
			httpReq.Header.Set("x-api-key", req.APIKey)
			httpReq.Header.Set("anthropic-version", "2023-06-01")
		}
	default:
		jsonError(w, "Unsupported adapter: "+req.Adapter, http.StatusBadRequest)
		return
	}

	if err != nil {
		jsonResponse(w, map[string]interface{}{"ok": false, "error": "Failed to create request: " + err.Error()})
		return
	}

	resp, err := client.Do(httpReq)
	if err != nil {
		jsonResponse(w, map[string]interface{}{"ok": false, "error": err.Error()})
		return
	}
	defer resp.Body.Close()
	io.ReadAll(resp.Body) // drain

	if resp.StatusCode >= 400 {
		jsonResponse(w, map[string]interface{}{"ok": false, "error": fmt.Sprintf("HTTP %d from %s", resp.StatusCode, testURL)})
		return
	}

	jsonResponse(w, map[string]interface{}{"ok": true, "message": "Connection successful"})
}

func (h *Handler) handleLLMModels(w http.ResponseWriter, r *http.Request) {
	var req llmTestRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		jsonError(w, "Invalid JSON: "+err.Error(), http.StatusBadRequest)
		return
	}

	client := &http.Client{Timeout: 10 * time.Second}
	var models []string

	switch req.Adapter {
	case "ollama":
		base := normalizeLLMBaseURL(req.BaseURL)
		if base == "" {
			base = "http://localhost:11434"
		}
		httpReq, err := http.NewRequest("GET", base+"/api/tags", nil)
		if err != nil {
			jsonResponse(w, map[string]interface{}{"ok": false, "error": err.Error()})
			return
		}
		resp, err := client.Do(httpReq)
		if err != nil {
			jsonResponse(w, map[string]interface{}{"ok": false, "error": err.Error()})
			return
		}
		defer resp.Body.Close()
		body, _ := io.ReadAll(resp.Body)
		if resp.StatusCode >= 400 {
			jsonResponse(w, map[string]interface{}{"ok": false, "error": fmt.Sprintf("HTTP %d: %s", resp.StatusCode, string(body[:min(len(body), 200)]))})
			return
		}
		var result struct {
			Models []struct {
				Name string `json:"name"`
			} `json:"models"`
		}
		if err := json.Unmarshal(body, &result); err != nil {
			jsonResponse(w, map[string]interface{}{"ok": false, "error": "Failed to parse response"})
			return
		}
		for _, m := range result.Models {
			models = append(models, m.Name)
		}

	case "openai":
		base := normalizeLLMBaseURL(req.BaseURL)
		if base == "" {
			base = "https://api.openai.com"
		}
		httpReq, err := http.NewRequest("GET", base+"/v1/models", nil)
		if err != nil {
			jsonResponse(w, map[string]interface{}{"ok": false, "error": err.Error()})
			return
		}
		if req.APIKey != "" {
			httpReq.Header.Set("Authorization", "Bearer "+req.APIKey)
		}
		resp, err := client.Do(httpReq)
		if err != nil {
			jsonResponse(w, map[string]interface{}{"ok": false, "error": err.Error()})
			return
		}
		defer resp.Body.Close()
		body, _ := io.ReadAll(resp.Body)
		if resp.StatusCode >= 400 {
			jsonResponse(w, map[string]interface{}{"ok": false, "error": fmt.Sprintf("HTTP %d: %s", resp.StatusCode, string(body[:min(len(body), 200)]))})
			return
		}
		var result struct {
			Data []struct {
				ID string `json:"id"`
			} `json:"data"`
		}
		if err := json.Unmarshal(body, &result); err != nil {
			jsonResponse(w, map[string]interface{}{"ok": false, "error": "Failed to parse response"})
			return
		}
		for _, m := range result.Data {
			models = append(models, m.ID)
		}

	case "anthropic":
		models = []string{
			"claude-haiku-4-5-20251001",
			"claude-sonnet-4-20250514",
			"claude-opus-4-20250514",
		}

	default:
		jsonError(w, "Unsupported adapter: "+req.Adapter, http.StatusBadRequest)
		return
	}

	jsonResponse(w, map[string]interface{}{"ok": true, "models": models})
}

// ── Auth Handlers ──

func (h *Handler) handleAuthStatus(w http.ResponseWriter, r *http.Request) {
	hasUsers, _ := h.store.HasAnyUser()
	authenticated := false
	var user *storage.User

	// Check session cookie
	if cookie, err := r.Cookie("aichatlog_session"); err == nil {
		if u, err := h.store.ValidateSession(cookie.Value); err == nil {
			authenticated = true
			user = u
		}
	}

	resp := map[string]interface{}{
		"setup_required": !hasUsers,
		"authenticated":  authenticated,
	}
	if user != nil {
		resp["user"] = user
	}
	jsonResponse(w, resp)
}

func (h *Handler) handleAuthSetup(w http.ResponseWriter, r *http.Request) {
	// Only allow if no users exist
	hasUsers, _ := h.store.HasAnyUser()
	if hasUsers {
		jsonError(w, "Setup already completed", http.StatusConflict)
		return
	}

	var req struct {
		Username string `json:"username"`
		Password string `json:"password"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		jsonError(w, "Invalid request", http.StatusBadRequest)
		return
	}
	if req.Username == "" || len(req.Password) < 6 {
		jsonError(w, "Username required, password must be at least 6 characters", http.StatusBadRequest)
		return
	}

	user, err := h.store.CreateUser(req.Username, req.Password, "admin")
	if err != nil {
		jsonError(w, err.Error(), http.StatusInternalServerError)
		return
	}

	// Auto-login: create session
	sid, err := h.store.CreateSession(user.ID)
	if err != nil {
		jsonError(w, err.Error(), http.StatusInternalServerError)
		return
	}
	http.SetCookie(w, &http.Cookie{
		Name:     "aichatlog_session",
		Value:    sid,
		Path:     "/",
		HttpOnly: true,
		SameSite: http.SameSiteStrictMode,
		MaxAge:   7 * 24 * 3600,
	})

	jsonResponse(w, map[string]interface{}{"ok": true, "user": user})
}

func (h *Handler) handleAuthLogin(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Username string `json:"username"`
		Password string `json:"password"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		jsonError(w, "Invalid request", http.StatusBadRequest)
		return
	}

	user, err := h.store.AuthenticateUser(req.Username, req.Password)
	if err != nil {
		jsonError(w, "Invalid username or password", http.StatusUnauthorized)
		return
	}

	sid, err := h.store.CreateSession(user.ID)
	if err != nil {
		jsonError(w, err.Error(), http.StatusInternalServerError)
		return
	}
	http.SetCookie(w, &http.Cookie{
		Name:     "aichatlog_session",
		Value:    sid,
		Path:     "/",
		HttpOnly: true,
		SameSite: http.SameSiteStrictMode,
		MaxAge:   7 * 24 * 3600,
	})

	jsonResponse(w, map[string]interface{}{"ok": true, "user": user})
}

func (h *Handler) handleAuthLogout(w http.ResponseWriter, r *http.Request) {
	if cookie, err := r.Cookie("aichatlog_session"); err == nil {
		h.store.DeleteSession(cookie.Value)
	}
	http.SetCookie(w, &http.Cookie{
		Name:     "aichatlog_session",
		Value:    "",
		Path:     "/",
		HttpOnly: true,
		MaxAge:   -1,
	})
	jsonResponse(w, map[string]interface{}{"ok": true})
}

// ── API Key Handlers ──

func (h *Handler) handleListKeys(w http.ResponseWriter, r *http.Request) {
	user := getUser(r)
	if user == nil {
		jsonError(w, "Unauthorized", http.StatusUnauthorized)
		return
	}
	keys, err := h.store.ListAPIKeys(user.ID)
	if err != nil {
		jsonError(w, err.Error(), http.StatusInternalServerError)
		return
	}
	if keys == nil {
		keys = []storage.APIKey{}
	}
	jsonResponse(w, keys)
}

func (h *Handler) handleCreateKey(w http.ResponseWriter, r *http.Request) {
	user := getUser(r)
	if user == nil {
		jsonError(w, "Unauthorized", http.StatusUnauthorized)
		return
	}
	var req struct {
		Name string `json:"name"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.Name == "" {
		jsonError(w, "Name is required", http.StatusBadRequest)
		return
	}

	rawKey, keyInfo, err := h.store.CreateAPIKey(user.ID, req.Name)
	if err != nil {
		jsonError(w, err.Error(), http.StatusInternalServerError)
		return
	}
	jsonResponse(w, map[string]interface{}{
		"ok":  true,
		"key": rawKey,
		"info": keyInfo,
	})
}

func (h *Handler) handleDeleteKey(w http.ResponseWriter, r *http.Request) {
	user := getUser(r)
	if user == nil {
		jsonError(w, "Unauthorized", http.StatusUnauthorized)
		return
	}
	id := r.PathValue("id")
	if err := h.store.DeleteAPIKey(id, user.ID); err != nil {
		jsonError(w, err.Error(), http.StatusNotFound)
		return
	}
	jsonResponse(w, map[string]interface{}{"ok": true})
}

// ── User Management Handlers ──

func (h *Handler) handleListUsers(w http.ResponseWriter, r *http.Request) {
	if requireAdmin(w, r) == nil {
		return
	}
	users, err := h.store.ListUsers()
	if err != nil {
		jsonError(w, err.Error(), http.StatusInternalServerError)
		return
	}
	if users == nil {
		users = []storage.User{}
	}
	jsonResponse(w, users)
}

func (h *Handler) handleCreateUser(w http.ResponseWriter, r *http.Request) {
	if requireAdmin(w, r) == nil {
		return
	}
	var req struct {
		Username string `json:"username"`
		Password string `json:"password"`
		Role     string `json:"role"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		jsonError(w, "Invalid request", http.StatusBadRequest)
		return
	}
	if req.Username == "" || len(req.Password) < 6 {
		jsonError(w, "Username required, password must be at least 6 characters", http.StatusBadRequest)
		return
	}
	if req.Role == "" {
		req.Role = "user"
	}
	user, err := h.store.CreateUser(req.Username, req.Password, req.Role)
	if err != nil {
		jsonError(w, err.Error(), http.StatusConflict)
		return
	}
	jsonResponse(w, map[string]interface{}{"ok": true, "user": user})
}

func (h *Handler) handleDeleteUser(w http.ResponseWriter, r *http.Request) {
	admin := requireAdmin(w, r)
	if admin == nil {
		return
	}
	id := r.PathValue("id")
	if id == admin.ID {
		jsonError(w, "Cannot delete yourself", http.StatusBadRequest)
		return
	}
	if err := h.store.DeleteUser(id); err != nil {
		jsonError(w, err.Error(), http.StatusNotFound)
		return
	}
	jsonResponse(w, map[string]interface{}{"ok": true})
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
