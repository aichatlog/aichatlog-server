package storage

import (
	"crypto/sha256"
	"database/sql"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"sort"
	"path/filepath"
	"strings"
	"time"

	_ "github.com/mattn/go-sqlite3"
)

// ── Data Types ──

// ConversationObject matches the plugin's ConversationObject protocol.
type ConversationObject struct {
	Version           int                    `json:"version"`
	Source            string                 `json:"source"`
	Device            string                 `json:"device"`
	SessionID         string                 `json:"session_id"`
	Title             string                 `json:"title"`
	Date              string                 `json:"date"`
	StartedAt         string                 `json:"started_at,omitempty"`
	EndedAt           string                 `json:"ended_at,omitempty"`
	Project           string                 `json:"project"`
	ProjectPath       string                 `json:"project_path"`
	Model             string                 `json:"model,omitempty"`
	MessageCount      int                    `json:"message_count"`
	WordCount         int                    `json:"word_count"`
	ContentHash       string                 `json:"content_hash"`
	HasCode           bool                   `json:"has_code,omitempty"`
	TotalInputTokens  int                    `json:"total_input_tokens,omitempty"`
	TotalOutputTokens int                    `json:"total_output_tokens,omitempty"`
	Metadata          map[string]interface{} `json:"metadata,omitempty"`
	Messages          []Message              `json:"messages"`
}

// Message represents a single message in a conversation.
type Message struct {
	Role         string `json:"role"`
	Content      string `json:"content"`
	TimeStr      string `json:"time_str"`
	Timestamp    string `json:"timestamp,omitempty"`
	IsContext    bool   `json:"is_context"`
	Seq          int    `json:"seq"`
	Model        string `json:"model,omitempty"`
	InputTokens  int    `json:"input_tokens,omitempty"`
	OutputTokens int    `json:"output_tokens,omitempty"`
}

// ConversationRow represents a row from the conversations table.
type ConversationRow struct {
	ID                string  `json:"id"`
	SessionID         string  `json:"session_id"`
	SourceType        string  `json:"source_type"`
	Device            string  `json:"device"`
	Title             string  `json:"title"`
	Project           string  `json:"project"`
	ProjectPath       string  `json:"project_path"`
	Model             string  `json:"model"`
	StartedAt         string  `json:"started_at"`
	EndedAt           string  `json:"ended_at"`
	WordCount         int     `json:"word_count"`
	MessageCount      int     `json:"message_count"`
	ContentHash       string  `json:"content_hash"`
	HasCode           bool    `json:"has_code"`
	TotalInputTokens  int     `json:"total_input_tokens"`
	TotalOutputTokens int     `json:"total_output_tokens"`
	Metadata          string  `json:"metadata"`
	Status            string  `json:"status"`
	CreatedAt         string  `json:"created_at"`
	UpdatedAt         string  `json:"updated_at"`
	SyncedAt          *string `json:"synced_at,omitempty"`
}

// MessageRow represents a row from the messages table.
type MessageRow struct {
	ID             int    `json:"id"`
	ConversationID string `json:"conversation_id"`
	Role           string `json:"role"`
	Content        string `json:"content"`
	Timestamp      string `json:"timestamp"`
	Seq            int    `json:"seq"`
	IsContext      bool   `json:"is_context"`
	Model          string `json:"model"`
	InputTokens    int    `json:"input_tokens"`
	OutputTokens   int    `json:"output_tokens"`
}

// ConversationDetail is a conversation with its messages.
type ConversationDetail struct {
	ConversationRow
	Messages []MessageRow `json:"messages"`
}

// QueryParams for listing conversations.
type QueryParams struct {
	Status  string
	Query   string
	Project string
	Source  string
	Sort    string
	Order   string
	Limit   int
	Offset  int
}

// Stats returns conversation counts by status.
type Stats struct {
	Received int `json:"received"`
	Synced   int `json:"synced"`
	Failed   int `json:"failed"`
	Ignored  int `json:"ignored"`
	Total    int `json:"total"`
}

// SyncRequest is the v2 sync protocol request.
type SyncRequest struct {
	Version           int                    `json:"version"`
	SyncMode          string                 `json:"sync_mode"` // "full", "delta", "check"
	Source            string                 `json:"source"`
	Device            string                 `json:"device"`
	SessionID         string                 `json:"session_id"`
	Title             string                 `json:"title"`
	Date              string                 `json:"date"`
	StartedAt         string                 `json:"started_at,omitempty"`
	EndedAt           string                 `json:"ended_at,omitempty"`
	Project           string                 `json:"project"`
	ProjectPath       string                 `json:"project_path"`
	Model             string                 `json:"model,omitempty"`
	MessageCount      int                    `json:"message_count"`
	WordCount         int                    `json:"word_count"`
	ContentHash       string                 `json:"content_hash"`
	HasCode           *bool                  `json:"has_code,omitempty"`
	TotalInputTokens  int                    `json:"total_input_tokens,omitempty"`
	TotalOutputTokens int                    `json:"total_output_tokens,omitempty"`
	Metadata          map[string]interface{} `json:"metadata,omitempty"`
	DeltaFromSeq      int                    `json:"delta_from_seq,omitempty"`
	Messages          []Message              `json:"messages,omitempty"`
}

// SyncResponse is the v2 sync protocol response.
type SyncResponse struct {
	OK                 bool   `json:"ok"`
	Action             string `json:"action"` // "created", "updated", "unchanged", "need_full"
	SessionID          string `json:"session_id"`
	ServerHash         string `json:"server_hash,omitempty"`
	ServerMessageCount int    `json:"server_message_count"`
}

// ToConversationObject converts a SyncRequest to a ConversationObject for Upsert.
func (r *SyncRequest) ToConversationObject() *ConversationObject {
	hasCode := false
	if r.HasCode != nil {
		hasCode = *r.HasCode
	}
	return &ConversationObject{
		Version:           r.Version,
		Source:            r.Source,
		Device:            r.Device,
		SessionID:         r.SessionID,
		Title:             r.Title,
		Date:              r.Date,
		StartedAt:         r.StartedAt,
		EndedAt:           r.EndedAt,
		Project:           r.Project,
		ProjectPath:       r.ProjectPath,
		Model:             r.Model,
		MessageCount:      r.MessageCount,
		WordCount:         r.WordCount,
		ContentHash:       r.ContentHash,
		HasCode:           hasCode,
		TotalInputTokens:  r.TotalInputTokens,
		TotalOutputTokens: r.TotalOutputTokens,
		Metadata:          r.Metadata,
		Messages:          r.Messages,
	}
}

// ── Store ──

// Store manages SQLite storage.
type Store struct {
	db      *sql.DB
	dataDir string
}

// New creates a new Store with the given database path and data directory.
func New(dbPath, dataDir string) (*Store, error) {
	if err := os.MkdirAll(filepath.Dir(dbPath), 0o755); err != nil {
		return nil, fmt.Errorf("create db directory: %w", err)
	}
	if err := os.MkdirAll(dataDir, 0o755); err != nil {
		return nil, fmt.Errorf("create data directory: %w", err)
	}

	db, err := sql.Open("sqlite3", dbPath+"?_journal_mode=WAL&_foreign_keys=on")
	if err != nil {
		return nil, fmt.Errorf("open database: %w", err)
	}

	s := &Store{db: db, dataDir: dataDir}
	if err := s.migrate(); err != nil {
		db.Close()
		return nil, fmt.Errorf("migrate: %w", err)
	}
	return s, nil
}

// Close closes the database connection.
func (s *Store) Close() error {
	return s.db.Close()
}

// ── Migrations ──

func (s *Store) migrate() error {
	// Ensure schema_version table exists
	if _, err := s.db.Exec(`
		CREATE TABLE IF NOT EXISTS schema_version (
			version INTEGER NOT NULL
		)
	`); err != nil {
		return fmt.Errorf("create schema_version: %w", err)
	}

	var current int
	err := s.db.QueryRow("SELECT COALESCE(MAX(version), 0) FROM schema_version").Scan(&current)
	if err != nil {
		return fmt.Errorf("read schema version: %w", err)
	}

	migrations := []func(*sql.Tx) error{
		s.migrateV1,
		s.migrateV2,
		s.migrateV3,
		s.migrateV4,
	}

	for i := current; i < len(migrations); i++ {
		tx, err := s.db.Begin()
		if err != nil {
			return fmt.Errorf("begin migration %d: %w", i+1, err)
		}
		if err := migrations[i](tx); err != nil {
			tx.Rollback()
			return fmt.Errorf("migration %d: %w", i+1, err)
		}
		if _, err := tx.Exec("DELETE FROM schema_version"); err != nil {
			tx.Rollback()
			return fmt.Errorf("update schema version %d: %w", i+1, err)
		}
		if _, err := tx.Exec("INSERT INTO schema_version (version) VALUES (?)", i+1); err != nil {
			tx.Rollback()
			return fmt.Errorf("insert schema version %d: %w", i+1, err)
		}
		if err := tx.Commit(); err != nil {
			return fmt.Errorf("commit migration %d: %w", i+1, err)
		}
	}

	// Post-migration: ensure 'deleted' status is accepted.
	// For databases created before v3, the CHECK constraint blocks it.
	// Recreate the table outside any transaction (FK pragma requires it).
	if err := s.ensureDeletedStatus(); err != nil {
		return fmt.Errorf("ensure deleted status: %w", err)
	}

	return nil
}

// ensureDeletedStatus checks if the conversations table accepts 'deleted' status.
// If not (old CHECK constraint), recreates the table outside a transaction.
func (s *Store) ensureDeletedStatus() error {
	// Test if 'deleted' is accepted
	_, err := s.db.Exec("INSERT INTO conversations (id, session_id, title, started_at, status) VALUES ('__test_del__','__test_del__','t','t','deleted')")
	if err == nil {
		s.db.Exec("DELETE FROM conversations WHERE id = '__test_del__'")
		return nil // Already supports 'deleted'
	}

	// Need to recreate table. Disable FK checks.
	s.db.Exec("PRAGMA foreign_keys = OFF")
	defer s.db.Exec("PRAGMA foreign_keys = ON")

	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()

	stmts := []string{
		`ALTER TABLE conversations RENAME TO _conv_old`,
		`CREATE TABLE conversations (
			id TEXT PRIMARY KEY, session_id TEXT NOT NULL,
			source_type TEXT NOT NULL DEFAULT 'claude-code', device TEXT NOT NULL DEFAULT '',
			title TEXT NOT NULL, project TEXT DEFAULT '', project_path TEXT DEFAULT '',
			model TEXT DEFAULT '', started_at TEXT NOT NULL, ended_at TEXT DEFAULT '',
			word_count INTEGER DEFAULT 0, message_count INTEGER DEFAULT 0,
			content_hash TEXT, has_code INTEGER DEFAULT 0,
			total_input_tokens INTEGER DEFAULT 0, total_output_tokens INTEGER DEFAULT 0,
			metadata TEXT DEFAULT '{}',
			status TEXT DEFAULT 'received' CHECK(status IN ('received','synced','failed','ignored','deleted')),
			created_at TEXT DEFAULT (datetime('now','localtime')),
			updated_at TEXT DEFAULT (datetime('now','localtime')),
			UNIQUE(source_type, device, session_id)
		)`,
		`INSERT INTO conversations SELECT * FROM _conv_old`,
		`DROP TABLE _conv_old`,
	}
	for _, stmt := range stmts {
		if _, err := tx.Exec(stmt); err != nil {
			return err
		}
	}
	return tx.Commit()
}

// migrateV1 creates the full schema from scratch.
func (s *Store) migrateV1(tx *sql.Tx) error {
	stmts := []string{
		// 1. conversations
		`CREATE TABLE IF NOT EXISTS conversations (
			id             TEXT PRIMARY KEY,
			session_id     TEXT NOT NULL,
			source_type    TEXT NOT NULL DEFAULT 'claude-code',
			device         TEXT NOT NULL DEFAULT '',
			title          TEXT NOT NULL,
			project        TEXT DEFAULT '',
			project_path   TEXT DEFAULT '',
			model          TEXT DEFAULT '',
			started_at     TEXT NOT NULL,
			word_count     INTEGER DEFAULT 0,
			message_count  INTEGER DEFAULT 0,
			content_hash   TEXT,
			has_code       INTEGER DEFAULT 0,
			status         TEXT DEFAULT 'received' CHECK(status IN ('received','synced','failed','ignored','deleted')),
			created_at     TEXT DEFAULT (datetime('now','localtime')),
			updated_at     TEXT DEFAULT (datetime('now','localtime')),
			UNIQUE(source_type, device, session_id)
		)`,
		`CREATE INDEX IF NOT EXISTS idx_conv_status ON conversations(status)`,
		`CREATE INDEX IF NOT EXISTS idx_conv_started ON conversations(started_at)`,
		`CREATE INDEX IF NOT EXISTS idx_conv_source ON conversations(source_type)`,
		`CREATE INDEX IF NOT EXISTS idx_conv_project ON conversations(project)`,

		// 2. messages
		`CREATE TABLE IF NOT EXISTS messages (
			id              INTEGER PRIMARY KEY AUTOINCREMENT,
			conversation_id TEXT NOT NULL REFERENCES conversations(id) ON DELETE CASCADE,
			role            TEXT NOT NULL,
			content         TEXT NOT NULL,
			timestamp       TEXT DEFAULT '',
			seq             INTEGER NOT NULL,
			is_context      INTEGER DEFAULT 0
		)`,
		`CREATE INDEX IF NOT EXISTS idx_msg_conv ON messages(conversation_id)`,

		// 3. tags
		`CREATE TABLE IF NOT EXISTS tags (
			id              INTEGER PRIMARY KEY AUTOINCREMENT,
			conversation_id TEXT NOT NULL REFERENCES conversations(id) ON DELETE CASCADE,
			tag             TEXT NOT NULL,
			auto_generated  INTEGER DEFAULT 1,
			UNIQUE(conversation_id, tag)
		)`,

		// 4. extractions
		`CREATE TABLE IF NOT EXISTS extractions (
			id              INTEGER PRIMARY KEY AUTOINCREMENT,
			conversation_id TEXT NOT NULL REFERENCES conversations(id) ON DELETE CASCADE,
			type            TEXT NOT NULL,
			title           TEXT NOT NULL,
			content         TEXT NOT NULL,
			metadata        TEXT DEFAULT '{}',
			output_path     TEXT DEFAULT '',
			extracted_at    TEXT DEFAULT (datetime('now','localtime')),
			model_used      TEXT DEFAULT ''
		)`,
		`CREATE INDEX IF NOT EXISTS idx_ext_conv ON extractions(conversation_id)`,
		`CREATE INDEX IF NOT EXISTS idx_ext_type ON extractions(type)`,

		// 5. output_sync
		`CREATE TABLE IF NOT EXISTS output_sync (
			id              INTEGER PRIMARY KEY AUTOINCREMENT,
			conversation_id TEXT REFERENCES conversations(id) ON DELETE SET NULL,
			adapter         TEXT NOT NULL,
			path            TEXT NOT NULL,
			content_hash    TEXT NOT NULL,
			synced_at       TEXT DEFAULT (datetime('now','localtime')),
			status          TEXT DEFAULT 'ok'
		)`,

		// 6. process_queue
		`CREATE TABLE IF NOT EXISTS process_queue (
			id              INTEGER PRIMARY KEY AUTOINCREMENT,
			conversation_id TEXT NOT NULL REFERENCES conversations(id) ON DELETE CASCADE,
			task_type       TEXT NOT NULL,
			priority        INTEGER DEFAULT 0,
			status          TEXT DEFAULT 'pending',
			attempts        INTEGER DEFAULT 0,
			last_error      TEXT DEFAULT '',
			created_at      TEXT DEFAULT (datetime('now','localtime')),
			started_at      TEXT,
			completed_at    TEXT
		)`,
		`CREATE INDEX IF NOT EXISTS idx_pq_status ON process_queue(status, priority DESC)`,

		// 7. FTS5 for conversations
		`CREATE VIRTUAL TABLE IF NOT EXISTS conversations_fts USING fts5(
			id UNINDEXED,
			title,
			project
		)`,

		// FTS triggers
		`CREATE TRIGGER IF NOT EXISTS conversations_ai AFTER INSERT ON conversations BEGIN
			INSERT INTO conversations_fts(id, title, project) VALUES (new.id, new.title, new.project);
		END`,
		`CREATE TRIGGER IF NOT EXISTS conversations_au AFTER UPDATE OF title, project ON conversations BEGIN
			DELETE FROM conversations_fts WHERE id = old.id;
			INSERT INTO conversations_fts(id, title, project) VALUES (new.id, new.title, new.project);
		END`,
		`CREATE TRIGGER IF NOT EXISTS conversations_ad AFTER DELETE ON conversations BEGIN
			DELETE FROM conversations_fts WHERE id = old.id;
		END`,
	}

	for _, stmt := range stmts {
		if _, err := tx.Exec(stmt); err != nil {
			return fmt.Errorf("exec %q: %w", stmt[:60], err)
		}
	}
	return nil
}

// migrateV2 adds metadata, token, and timestamp columns.
func (s *Store) migrateV2(tx *sql.Tx) error {
	// conversations: add ended_at, total_input_tokens, total_output_tokens, metadata
	convCols := []string{
		"ALTER TABLE conversations ADD COLUMN ended_at TEXT DEFAULT ''",
		"ALTER TABLE conversations ADD COLUMN total_input_tokens INTEGER DEFAULT 0",
		"ALTER TABLE conversations ADD COLUMN total_output_tokens INTEGER DEFAULT 0",
		"ALTER TABLE conversations ADD COLUMN metadata TEXT DEFAULT '{}'",
	}
	for _, stmt := range convCols {
		if _, err := tx.Exec(stmt); err != nil {
			// Column may already exist if re-running
			if !strings.Contains(err.Error(), "duplicate column") {
				return err
			}
		}
	}

	// messages: add model, input_tokens, output_tokens
	msgCols := []string{
		"ALTER TABLE messages ADD COLUMN model TEXT DEFAULT ''",
		"ALTER TABLE messages ADD COLUMN input_tokens INTEGER DEFAULT 0",
		"ALTER TABLE messages ADD COLUMN output_tokens INTEGER DEFAULT 0",
	}
	for _, stmt := range msgCols {
		if _, err := tx.Exec(stmt); err != nil {
			if !strings.Contains(err.Error(), "duplicate column") {
				return err
			}
		}
	}

	return nil
}

// migrateV3 is a no-op for new databases (migrateV1 already includes 'deleted' in CHECK).
// For existing databases, the old CHECK constraint prevents writing 'deleted' status.
// SQLite doesn't support ALTER CHECK, so we recreate the table outside the transaction.
// This is handled in migrate() with a post-migration step.
func (s *Store) migrateV3(tx *sql.Tx) error {
	// Check if we need to recreate (old CHECK without 'deleted')
	// Try inserting and rolling back to test
	_, err := tx.Exec("INSERT INTO conversations (id, session_id, title, started_at, status) VALUES ('__test__','__test__','t','t','deleted')")
	if err != nil {
		// Old CHECK constraint blocks 'deleted' — need table recreation.
		// This must be done outside the transaction. Mark for post-migration.
		// For now, just record the version. The post-migration in migrate() will handle it.
		return nil
	}
	// Clean up test row
	tx.Exec("DELETE FROM conversations WHERE id = '__test__'")
	return nil
}

// migrateV4 replaces conversations_fts with turn-level FTS5 indexing.
func (s *Store) migrateV4(tx *sql.Tx) error {
	stmts := []string{
		"DROP TRIGGER IF EXISTS conversations_ai",
		"DROP TRIGGER IF EXISTS conversations_au",
		"DROP TRIGGER IF EXISTS conversations_ad",
		"DROP TABLE IF EXISTS conversations_fts",
		`CREATE VIRTUAL TABLE IF NOT EXISTS turns_fts USING fts5(
			conversation_id UNINDEXED,
			turn_seq UNINDEXED,
			title UNINDEXED,
			user_content,
			assistant_content,
			tokenize='unicode61 remove_diacritics 2'
		)`,
	}
	for _, stmt := range stmts {
		if _, err := tx.Exec(stmt); err != nil {
			return fmt.Errorf("migrateV4: %w", err)
		}
	}

	// Rebuild FTS for all existing conversations
	rows, err := tx.Query("SELECT id FROM conversations WHERE status != 'deleted'")
	if err != nil {
		return fmt.Errorf("migrateV4 list convs: %w", err)
	}
	var ids []string
	for rows.Next() {
		var id string
		rows.Scan(&id)
		ids = append(ids, id)
	}
	rows.Close()

	for _, id := range ids {
		if err := s.rebuildTurnsFTSWithTx(tx, id); err != nil {
			log.Printf("migrateV4: rebuild FTS for %s: %v", id, err)
		}
	}
	log.Printf("migrateV4: rebuilt FTS index for %d conversations", len(ids))
	return nil
}

// turnData represents a single Q&A turn for FTS indexing.
type turnData struct {
	UserContent      string
	AssistantContent string
}

// groupIntoTurns groups messages into Q&A turns.
// Each turn = consecutive user messages + following assistant messages.
func groupIntoTurns(msgs []MessageRow) []turnData {
	var turns []turnData
	var cur *turnData

	for _, m := range msgs {
		if m.IsContext {
			continue
		}
		switch m.Role {
		case "user":
			if cur != nil && (cur.UserContent != "" || cur.AssistantContent != "") {
				turns = append(turns, *cur)
			}
			cur = &turnData{}
			cur.UserContent = m.Content
		case "assistant":
			if cur == nil {
				cur = &turnData{}
			}
			if cur.AssistantContent != "" {
				cur.AssistantContent += "\n"
			}
			content := m.Content
			if len(content) > 5000 {
				content = content[:5000]
			}
			cur.AssistantContent += content
		}
	}
	if cur != nil && (cur.UserContent != "" || cur.AssistantContent != "") {
		turns = append(turns, *cur)
	}
	return turns
}

// rebuildTurnsFTS rebuilds FTS entries for a single conversation.
func (s *Store) rebuildTurnsFTS(convID string) {
	tx, err := s.db.Begin()
	if err != nil {
		return
	}
	defer tx.Rollback()
	if err := s.rebuildTurnsFTSWithTx(tx, convID); err != nil {
		log.Printf("rebuildTurnsFTS %s: %v", convID, err)
		return
	}
	tx.Commit()
}

func (s *Store) rebuildTurnsFTSWithTx(tx *sql.Tx, convID string) error {
	// Delete old entries
	if _, err := tx.Exec("DELETE FROM turns_fts WHERE conversation_id = ?", convID); err != nil {
		return err
	}

	// Get conversation title
	var title string
	tx.QueryRow("SELECT title FROM conversations WHERE id = ?", convID).Scan(&title)

	// Get messages
	rows, err := tx.Query(`
		SELECT id, conversation_id, role, content, timestamp, seq, is_context,
			model, input_tokens, output_tokens
		FROM messages WHERE conversation_id = ? ORDER BY seq`, convID)
	if err != nil {
		return err
	}
	var msgs []MessageRow
	for rows.Next() {
		var m MessageRow
		var isCtx int
		if err := rows.Scan(&m.ID, &m.ConversationID, &m.Role, &m.Content,
			&m.Timestamp, &m.Seq, &isCtx, &m.Model, &m.InputTokens, &m.OutputTokens); err != nil {
			rows.Close()
			return err
		}
		m.IsContext = isCtx != 0
		msgs = append(msgs, m)
	}
	rows.Close()

	turns := groupIntoTurns(msgs)
	for i, t := range turns {
		if _, err := tx.Exec(
			"INSERT INTO turns_fts(conversation_id, turn_seq, title, user_content, assistant_content) VALUES(?,?,?,?,?)",
			convID, i, title, t.UserContent, t.AssistantContent); err != nil {
			return err
		}
	}
	return nil
}

// TurnSnippet represents a matched turn in search results.
type TurnSnippet struct {
	TurnSeq    int    `json:"turn_seq"`
	UserSnip   string `json:"user_snippet"`
	AssistSnip string `json:"assistant_snippet"`
}

// SearchSnippets returns matching turn snippets grouped by conversation ID.
func (s *Store) SearchSnippets(query string, limit int) map[string][]TurnSnippet {
	if query == "" {
		return nil
	}
	escaped := ftsEscape(query)
	rows, err := s.db.Query(`
		SELECT conversation_id, CAST(turn_seq AS INTEGER),
			snippet(turns_fts, 3, '<mark>', '</mark>', '…', 30),
			snippet(turns_fts, 4, '<mark>', '</mark>', '…', 30)
		FROM turns_fts
		WHERE turns_fts MATCH ?
		ORDER BY rank
		LIMIT ?
	`, escaped, limit)
	if err != nil {
		log.Printf("SearchSnippets error: %v", err)
		return nil
	}
	defer rows.Close()

	result := make(map[string][]TurnSnippet)
	for rows.Next() {
		var convID string
		var sn TurnSnippet
		if err := rows.Scan(&convID, &sn.TurnSeq, &sn.UserSnip, &sn.AssistSnip); err != nil {
			continue
		}
		result[convID] = append(result[convID], sn)
	}
	return result
}

// TurnRow represents a single Q&A turn with conversation context for timeline display.
type TurnRow struct {
	ConversationID string `json:"conversation_id"`
	Title          string `json:"title"`
	Project        string `json:"project"`
	SourceType     string `json:"source_type"`
	TurnSeq        int    `json:"turn_seq"`
	UserContent    string `json:"user_content"`
	AssistContent  string `json:"assistant_content"`
	Timestamp      string `json:"timestamp"`
}

// ListTurns returns recent Q&A turns across all conversations, ordered by timestamp DESC.
func (s *Store) ListTurns(limit, offset int) ([]TurnRow, error) {
	if limit <= 0 {
		limit = 50
	}
	// Get recent conversations with messages
	convRows, err := s.db.Query(`
		SELECT id, title, project, source_type FROM conversations
		WHERE status != 'deleted'
		ORDER BY started_at DESC
		LIMIT 50
	`)
	if err != nil {
		return nil, err
	}
	type convMeta struct {
		ID, Title, Project, Source string
	}
	var convs []convMeta
	for convRows.Next() {
		var cm convMeta
		convRows.Scan(&cm.ID, &cm.Title, &cm.Project, &cm.Source)
		convs = append(convs, cm)
	}
	convRows.Close()

	// For each conversation, get messages and group into turns
	var allTurns []TurnRow
	for _, cm := range convs {
		msgs, err := s.GetMessages(cm.ID)
		if err != nil {
			continue
		}
		turns := groupIntoTurns(msgs)
		for i, t := range turns {
			// Find timestamp from first user message of this turn
			ts := ""
			for _, m := range msgs {
				if m.IsContext {
					continue
				}
				if m.Role == "user" && m.Content == t.UserContent {
					ts = m.Timestamp
					break
				}
			}
			// Truncate content for API response
			uc := t.UserContent
			if len(uc) > 200 {
				uc = uc[:200]
			}
			ac := t.AssistantContent
			if len(ac) > 200 {
				ac = ac[:200]
			}
			allTurns = append(allTurns, TurnRow{
				ConversationID: cm.ID,
				Title:          cm.Title,
				Project:        cm.Project,
				SourceType:     cm.Source,
				TurnSeq:        i,
				UserContent:    uc,
				AssistContent:  ac,
				Timestamp:      ts,
			})
		}
	}

	// Sort by timestamp DESC
	sort.Slice(allTurns, func(i, j int) bool {
		return allTurns[i].Timestamp > allTurns[j].Timestamp
	})

	// Apply offset and limit
	if offset >= len(allTurns) {
		return []TurnRow{}, nil
	}
	end := offset + limit
	if end > len(allTurns) {
		end = len(allTurns)
	}
	return allTurns[offset:end], nil
}

// ── Conversation Operations ──

// generateID creates a deterministic conversation ID from source, device, and session_id.
func generateID(source, device, sessionID string) string {
	h := sha256.Sum256([]byte(source + "|" + device + "|" + sessionID))
	return fmt.Sprintf("conv_%x", h[:6])
}

// detectHasCode checks if any message contains code blocks.
func detectHasCode(messages []Message) bool {
	for _, m := range messages {
		if strings.Contains(m.Content, "```") {
			return true
		}
	}
	return false
}

// Upsert inserts or updates a conversation from a ConversationObject.
// Writes to both conversations and messages tables in a single transaction.
func (s *Store) Upsert(conv *ConversationObject) error {
	source := conv.Source
	if source == "" {
		source = "claude-code"
	}

	id := generateID(source, conv.Device, conv.SessionID)
	now := time.Now().Format("2006-01-02T15:04:05")

	hasCode := conv.HasCode
	if !hasCode {
		hasCode = detectHasCode(conv.Messages)
	}

	startedAt := conv.StartedAt
	if startedAt == "" {
		startedAt = conv.Date
	}
	if startedAt == "" {
		startedAt = now
	}

	endedAt := conv.EndedAt

	metadataJSON := "{}"
	if conv.Metadata != nil {
		if b, err := json.Marshal(conv.Metadata); err == nil {
			metadataJSON = string(b)
		}
	}

	tx, err := s.db.Begin()
	if err != nil {
		return fmt.Errorf("begin: %w", err)
	}
	defer tx.Rollback()

	// Upsert conversation
	_, err = tx.Exec(`
		INSERT INTO conversations
			(id, session_id, source_type, device, title, project, project_path,
			 model, started_at, ended_at, word_count, message_count, content_hash,
			 has_code, total_input_tokens, total_output_tokens, metadata,
			 created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(source_type, device, session_id) DO UPDATE SET
			title=excluded.title,
			project=excluded.project,
			project_path=excluded.project_path,
			model=excluded.model,
			started_at=excluded.started_at,
			ended_at=excluded.ended_at,
			word_count=excluded.word_count,
			message_count=excluded.message_count,
			content_hash=excluded.content_hash,
			has_code=excluded.has_code,
			total_input_tokens=excluded.total_input_tokens,
			total_output_tokens=excluded.total_output_tokens,
			metadata=excluded.metadata,
			status=CASE
				WHEN conversations.status IN ('ignored','deleted') THEN conversations.status
				WHEN conversations.content_hash != excluded.content_hash THEN 'received'
				ELSE conversations.status
			END,
			updated_at=excluded.updated_at
	`, id, conv.SessionID, source, conv.Device, conv.Title,
		conv.Project, conv.ProjectPath, conv.Model, startedAt, endedAt,
		conv.WordCount, conv.MessageCount, conv.ContentHash, boolToInt(hasCode),
		conv.TotalInputTokens, conv.TotalOutputTokens, metadataJSON,
		now, now)
	if err != nil {
		return fmt.Errorf("upsert conversation: %w", err)
	}

	// Replace messages: delete old, insert new
	if _, err := tx.Exec("DELETE FROM messages WHERE conversation_id = ?", id); err != nil {
		return fmt.Errorf("delete old messages: %w", err)
	}

	if len(conv.Messages) > 0 {
		stmt, err := tx.Prepare(`
			INSERT INTO messages (conversation_id, role, content, timestamp, seq, is_context,
				model, input_tokens, output_tokens)
			VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)
		`)
		if err != nil {
			return fmt.Errorf("prepare message insert: %w", err)
		}
		defer stmt.Close()

		for _, m := range conv.Messages {
			isCtx := 0
			if m.IsContext {
				isCtx = 1
			}
			ts := m.Timestamp
			if ts == "" {
				ts = m.TimeStr
			}
			if _, err := stmt.Exec(id, m.Role, m.Content, ts, m.Seq, isCtx,
				m.Model, m.InputTokens, m.OutputTokens); err != nil {
				return fmt.Errorf("insert message seq %d: %w", m.Seq, err)
			}
		}
	}

	if err := tx.Commit(); err != nil {
		return err
	}
	s.rebuildTurnsFTS(id)
	return nil
}

// Sync handles the v2 conditional sync protocol.
// Returns a SyncResponse indicating what action was taken.
func (s *Store) Sync(req *SyncRequest) (*SyncResponse, error) {
	source := req.Source
	if source == "" {
		source = "claude-code"
	}
	id := generateID(source, req.Device, req.SessionID)

	existing, err := s.Get(id)
	if err != nil {
		return nil, fmt.Errorf("lookup existing: %w", err)
	}

	resp := &SyncResponse{OK: true, SessionID: req.SessionID}

	switch req.SyncMode {
	case "check":
		if existing == nil {
			resp.Action = "need_full"
			return resp, nil
		}
		if existing.ContentHash == req.ContentHash {
			resp.Action = "unchanged"
			resp.ServerHash = existing.ContentHash
			resp.ServerMessageCount = existing.MessageCount
			return resp, nil
		}
		resp.Action = "need_full"
		resp.ServerHash = existing.ContentHash
		resp.ServerMessageCount = existing.MessageCount
		return resp, nil

	case "delta":
		if existing == nil {
			resp.Action = "need_full"
			return resp, nil
		}
		if err := s.applyDelta(req, id); err != nil {
			return nil, fmt.Errorf("apply delta: %w", err)
		}
		resp.Action = "updated"
		resp.ServerHash = req.ContentHash
		resp.ServerMessageCount = req.MessageCount
		return resp, nil

	default: // "full" or empty
		if err := s.upsertFromSync(req, id); err != nil {
			return nil, fmt.Errorf("full upsert: %w", err)
		}
		resp.ServerHash = req.ContentHash
		resp.ServerMessageCount = req.MessageCount
		if existing == nil {
			resp.Action = "created"
		} else {
			resp.Action = "updated"
		}
		return resp, nil
	}
}

// upsertFromSync performs a full upsert from a SyncRequest, using has_code from plugin if available.
func (s *Store) upsertFromSync(req *SyncRequest, id string) error {
	conv := req.ToConversationObject()
	// If plugin provided has_code, we still go through Upsert which calls detectHasCode.
	// We'll override has_code after if plugin provided it.
	if err := s.Upsert(conv); err != nil {
		return err
	}
	if req.HasCode != nil {
		s.db.Exec("UPDATE conversations SET has_code = ? WHERE id = ?", boolToInt(*req.HasCode), id)
	}
	return nil
}

// applyDelta applies incremental messages to an existing conversation.
func (s *Store) applyDelta(req *SyncRequest, id string) error {
	now := time.Now().Format("2006-01-02T15:04:05")

	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()

	// Delete messages from delta_from_seq onward
	if _, err := tx.Exec("DELETE FROM messages WHERE conversation_id = ? AND seq >= ?",
		id, req.DeltaFromSeq); err != nil {
		return fmt.Errorf("delete old messages: %w", err)
	}

	// Insert new messages
	if len(req.Messages) > 0 {
		stmt, err := tx.Prepare(`
			INSERT INTO messages (conversation_id, role, content, timestamp, seq, is_context,
				model, input_tokens, output_tokens)
			VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)
		`)
		if err != nil {
			return err
		}
		defer stmt.Close()

		for _, m := range req.Messages {
			isCtx := 0
			if m.IsContext {
				isCtx = 1
			}
			ts := m.Timestamp
			if ts == "" {
				ts = m.TimeStr
			}
			if _, err := stmt.Exec(id, m.Role, m.Content, ts, m.Seq, isCtx,
				m.Model, m.InputTokens, m.OutputTokens); err != nil {
				return fmt.Errorf("insert message seq %d: %w", m.Seq, err)
			}
		}
	}

	// Update conversation metadata
	hasCode := detectHasCode(req.Messages)
	if req.HasCode != nil {
		hasCode = *req.HasCode
	}

	startedAt := req.StartedAt
	if startedAt == "" {
		startedAt = req.Date
	}
	if startedAt == "" {
		startedAt = now
	}

	metadataJSON := "{}"
	if req.Metadata != nil {
		if b, err := json.Marshal(req.Metadata); err == nil {
			metadataJSON = string(b)
		}
	}

	_, err = tx.Exec(`
		UPDATE conversations SET
			title=?, project=?, project_path=?, model=?, started_at=?, ended_at=?,
			word_count=?, message_count=?, content_hash=?, has_code=?,
			total_input_tokens=?, total_output_tokens=?, metadata=?,
			status=CASE WHEN status IN ('ignored','deleted') THEN status ELSE 'received' END,
			updated_at=?
		WHERE id=?
	`, req.Title, req.Project, req.ProjectPath, req.Model, startedAt, req.EndedAt,
		req.WordCount, req.MessageCount, req.ContentHash, boolToInt(hasCode),
		req.TotalInputTokens, req.TotalOutputTokens, metadataJSON,
		now, id)
	if err != nil {
		return fmt.Errorf("update conversation: %w", err)
	}

	if err := tx.Commit(); err != nil {
		return err
	}
	s.rebuildTurnsFTS(id)
	return nil
}

func boolToInt(b bool) int {
	if b {
		return 1
	}
	return 0
}

// convColumns is the standard SELECT column list for conversations.
const convColumns = `id, session_id, source_type, device, title, project, project_path,
	model, started_at, ended_at, word_count, message_count, content_hash, has_code,
	total_input_tokens, total_output_tokens, metadata, status, created_at, updated_at`

func scanConversationRow(scanner interface{ Scan(...interface{}) error }) (*ConversationRow, error) {
	var c ConversationRow
	var hasCode int
	err := scanner.Scan(&c.ID, &c.SessionID, &c.SourceType, &c.Device, &c.Title,
		&c.Project, &c.ProjectPath, &c.Model, &c.StartedAt, &c.EndedAt,
		&c.WordCount, &c.MessageCount, &c.ContentHash, &hasCode,
		&c.TotalInputTokens, &c.TotalOutputTokens, &c.Metadata,
		&c.Status, &c.CreatedAt, &c.UpdatedAt)
	if err != nil {
		return nil, err
	}
	c.HasCode = hasCode != 0
	return &c, nil
}

// Get retrieves a single conversation by ID (or session_id).
func (s *Store) Get(id string) (*ConversationRow, error) {
	row := s.db.QueryRow(
		"SELECT "+convColumns+" FROM conversations WHERE id = ? OR session_id = ?", id, id)
	c, err := scanConversationRow(row)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	return c, err
}

// GetDetail retrieves a conversation with all its messages.
func (s *Store) GetDetail(id string) (*ConversationDetail, error) {
	conv, err := s.Get(id)
	if err != nil || conv == nil {
		return nil, err
	}

	messages, err := s.GetMessages(conv.ID)
	if err != nil {
		return nil, err
	}

	return &ConversationDetail{
		ConversationRow: *conv,
		Messages:        messages,
	}, nil
}

// GetMessages retrieves all messages for a conversation, ordered by seq.
func (s *Store) GetMessages(conversationID string) ([]MessageRow, error) {
	rows, err := s.db.Query(`
		SELECT id, conversation_id, role, content, timestamp, seq, is_context,
			model, input_tokens, output_tokens
		FROM messages
		WHERE conversation_id = ?
		ORDER BY seq
	`, conversationID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var messages []MessageRow
	for rows.Next() {
		var m MessageRow
		var isCtx int
		if err := rows.Scan(&m.ID, &m.ConversationID, &m.Role, &m.Content,
			&m.Timestamp, &m.Seq, &isCtx,
			&m.Model, &m.InputTokens, &m.OutputTokens); err != nil {
			return nil, err
		}
		m.IsContext = isCtx != 0
		messages = append(messages, m)
	}
	return messages, rows.Err()
}

// List returns conversations matching the query parameters.
func (s *Store) List(p QueryParams) ([]ConversationRow, error) {
	query := `SELECT ` + convColumns + ` FROM conversations`
	var args []interface{}
	var conditions []string

	if p.Status != "" {
		conditions = append(conditions, "status = ?")
		args = append(args, p.Status)
	} else {
		// Exclude deleted by default unless explicitly requested
		conditions = append(conditions, "status != 'deleted'")
	}
	if p.Project != "" {
		conditions = append(conditions, "project = ?")
		args = append(args, p.Project)
	}
	if p.Source != "" {
		conditions = append(conditions, "source_type = ?")
		args = append(args, p.Source)
	}

	// Full-text search via turns FTS5
	if p.Query != "" {
		conditions = append(conditions, "id IN (SELECT DISTINCT conversation_id FROM turns_fts WHERE turns_fts MATCH ?)")
		args = append(args, ftsEscape(p.Query))
	}

	if len(conditions) > 0 {
		query += " WHERE " + strings.Join(conditions, " AND ")
	}

	// Sort
	sortCol := "started_at"
	switch p.Sort {
	case "title", "project", "word_count", "message_count", "created_at":
		sortCol = p.Sort
	}
	order := "DESC"
	if p.Order == "asc" {
		order = "ASC"
	}
	query += fmt.Sprintf(" ORDER BY %s %s", sortCol, order)

	if p.Limit <= 0 {
		p.Limit = 200
	}
	query += fmt.Sprintf(" LIMIT %d OFFSET %d", p.Limit, p.Offset)

	rows, err := s.db.Query(query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var results []ConversationRow
	for rows.Next() {
		c, err := scanConversationRow(rows)
		if err != nil {
			return nil, err
		}
		results = append(results, *c)
	}
	return results, rows.Err()
}

// Stats returns conversation counts by status.
func (s *Store) Stats() (*Stats, error) {
	rows, err := s.db.Query("SELECT status, count(*) FROM conversations GROUP BY status")
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	st := &Stats{}
	for rows.Next() {
		var status string
		var count int
		if err := rows.Scan(&status, &count); err != nil {
			return nil, err
		}
		switch status {
		case "received":
			st.Received = count
		case "synced":
			st.Synced = count
		case "failed":
			st.Failed = count
		case "ignored":
			st.Ignored = count
		}
		st.Total += count
	}
	return st, rows.Err()
}

// ── Batch Operations ──

// UpsertBatch inserts or updates multiple conversations.
func (s *Store) UpsertBatch(convs []*ConversationObject) (int, error) {
	count := 0
	for _, conv := range convs {
		if err := s.Upsert(conv); err != nil {
			return count, fmt.Errorf("batch item %d (%s): %w", count, conv.SessionID, err)
		}
		count++
	}
	return count, nil
}

// ── Tag Operations ──

// AddTag adds a tag to a conversation.
func (s *Store) AddTag(conversationID, tag string, autoGenerated bool) error {
	auto := 0
	if autoGenerated {
		auto = 1
	}
	_, err := s.db.Exec(`
		INSERT OR IGNORE INTO tags (conversation_id, tag, auto_generated)
		VALUES (?, ?, ?)
	`, conversationID, tag, auto)
	return err
}

// GetTags returns all tags for a conversation.
func (s *Store) GetTags(conversationID string) ([]string, error) {
	rows, err := s.db.Query("SELECT tag FROM tags WHERE conversation_id = ? ORDER BY tag", conversationID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var tags []string
	for rows.Next() {
		var tag string
		if err := rows.Scan(&tag); err != nil {
			return nil, err
		}
		tags = append(tags, tag)
	}
	return tags, rows.Err()
}

// ── Status Operations ──

// ListByStatus returns conversation IDs with the given status, limited to n.
func (s *Store) ListByStatus(status string, limit int) ([]string, error) {
	rows, err := s.db.Query(
		"SELECT id FROM conversations WHERE status = ? ORDER BY created_at ASC LIMIT ?",
		status, limit,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var ids []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		ids = append(ids, id)
	}
	return ids, rows.Err()
}

// UpdateStatus updates the status of a conversation.
func (s *Store) UpdateStatus(id, status string) error {
	now := time.Now().Format("2006-01-02T15:04:05")
	_, err := s.db.Exec(
		"UPDATE conversations SET status = ?, updated_at = ? WHERE id = ?",
		status, now, id,
	)
	return err
}

// RecordSync records a successful output sync in the output_sync table.
func (s *Store) RecordSync(conversationID, adapter, path, contentHash string) error {
	_, err := s.db.Exec(`
		INSERT INTO output_sync (conversation_id, adapter, path, content_hash)
		VALUES (?, ?, ?, ?)
	`, conversationID, adapter, path, contentHash)
	return err
}

// SoftDelete marks a conversation as deleted (soft delete).
// The record remains in DB to prevent re-sync from plugin.
func (s *Store) SoftDelete(id string) error {
	if err := s.UpdateStatus(id, "deleted"); err != nil {
		return err
	}
	s.db.Exec("DELETE FROM turns_fts WHERE conversation_id = ?", id)
	return nil
}

// UpdateFields updates specific conversation fields (title, project, status).
type UpdateFieldsParams struct {
	Title   *string `json:"title,omitempty"`
	Project *string `json:"project,omitempty"`
	Status  *string `json:"status,omitempty"`
}

func (s *Store) UpdateFields(id string, params UpdateFieldsParams) error {
	now := time.Now().Format("2006-01-02T15:04:05")
	var sets []string
	var args []interface{}

	if params.Title != nil {
		sets = append(sets, "title = ?")
		args = append(args, *params.Title)
	}
	if params.Project != nil {
		sets = append(sets, "project = ?")
		args = append(args, *params.Project)
	}
	if params.Status != nil {
		sets = append(sets, "status = ?")
		args = append(args, *params.Status)
	}
	if len(sets) == 0 {
		return nil
	}

	sets = append(sets, "updated_at = ?")
	args = append(args, now, id)
	query := "UPDATE conversations SET " + strings.Join(sets, ", ") + " WHERE id = ?"
	_, err := s.db.Exec(query, args...)
	return err
}

// ClearExtractions removes all extractions for a conversation (to re-extract).
func (s *Store) ClearExtractions(conversationID string) error {
	_, err := s.db.Exec("DELETE FROM extractions WHERE conversation_id = ?", conversationID)
	return err
}

// StatsSummary returns extended stats including token and extraction counts.
type StatsSummary struct {
	Stats
	TotalInputTokens  int64 `json:"total_input_tokens"`
	TotalOutputTokens int64 `json:"total_output_tokens"`
	TotalWords        int64 `json:"total_words"`
	ExtractionCount   int   `json:"extraction_count"`
	ProjectCount      int   `json:"project_count"`
}

func (s *Store) GetStatsSummary() (*StatsSummary, error) {
	st, err := s.Stats()
	if err != nil {
		return nil, err
	}
	summary := &StatsSummary{Stats: *st}

	s.db.QueryRow(`SELECT COALESCE(SUM(total_input_tokens),0), COALESCE(SUM(total_output_tokens),0),
		COALESCE(SUM(word_count),0) FROM conversations WHERE status != 'deleted'`).
		Scan(&summary.TotalInputTokens, &summary.TotalOutputTokens, &summary.TotalWords)

	s.db.QueryRow("SELECT COUNT(*) FROM extractions").Scan(&summary.ExtractionCount)
	s.db.QueryRow("SELECT COUNT(DISTINCT project) FROM conversations WHERE project != '' AND status != 'deleted'").
		Scan(&summary.ProjectCount)

	return summary, nil
}

// ── Project Operations ──

// ProjectRow represents aggregated project statistics.
type ProjectRow struct {
	Project      string `json:"project"`
	Conversations int   `json:"conversations"`
	TotalWords   int    `json:"total_words"`
	TotalTokensIn  int  `json:"total_tokens_in"`
	TotalTokensOut int  `json:"total_tokens_out"`
	LastActivity string `json:"last_activity"`
}

// ListProjects returns all projects with conversation counts and last activity.
func (s *Store) ListProjects() ([]ProjectRow, error) {
	rows, err := s.db.Query(`
		SELECT project,
			COUNT(*) as conversations,
			SUM(word_count) as total_words,
			SUM(total_input_tokens) as total_tokens_in,
			SUM(total_output_tokens) as total_tokens_out,
			MAX(started_at) as last_activity
		FROM conversations
		WHERE project != ''
		GROUP BY project
		ORDER BY last_activity DESC
	`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var results []ProjectRow
	for rows.Next() {
		var p ProjectRow
		if err := rows.Scan(&p.Project, &p.Conversations, &p.TotalWords,
			&p.TotalTokensIn, &p.TotalTokensOut, &p.LastActivity); err != nil {
			return nil, err
		}
		results = append(results, p)
	}
	return results, rows.Err()
}

// ── Extraction Operations ──

// ExtractionRow represents a row from the extractions table.
type ExtractionRow struct {
	ID             int    `json:"id"`
	ConversationID string `json:"conversation_id"`
	Type           string `json:"type"`
	Title          string `json:"title"`
	Content        string `json:"content"`
	Metadata       string `json:"metadata"`
	OutputPath     string `json:"output_path"`
	ExtractedAt    string `json:"extracted_at"`
	ModelUsed      string `json:"model_used"`
}

// MonthlyExtractionCount returns the number of extractions performed this month.
func (s *Store) MonthlyExtractionCount() (int, error) {
	var count int
	err := s.db.QueryRow(`
		SELECT COUNT(*) FROM extractions
		WHERE extracted_at >= date('now','start of month')
	`).Scan(&count)
	return count, err
}

// InsertExtraction stores an extraction result.
func (s *Store) InsertExtraction(conversationID, extractionType, title, content, metadata, outputPath, modelUsed string) error {
	_, err := s.db.Exec(`
		INSERT INTO extractions (conversation_id, type, title, content, metadata, output_path, model_used)
		VALUES (?, ?, ?, ?, ?, ?, ?)
	`, conversationID, extractionType, title, content, metadata, outputPath, modelUsed)
	return err
}

// GetExtractions returns all extractions for a conversation.
func (s *Store) GetExtractions(conversationID string) ([]ExtractionRow, error) {
	rows, err := s.db.Query(`
		SELECT id, conversation_id, type, title, content, metadata, output_path, extracted_at, model_used
		FROM extractions WHERE conversation_id = ? ORDER BY id
	`, conversationID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var results []ExtractionRow
	for rows.Next() {
		var e ExtractionRow
		if err := rows.Scan(&e.ID, &e.ConversationID, &e.Type, &e.Title, &e.Content,
			&e.Metadata, &e.OutputPath, &e.ExtractedAt, &e.ModelUsed); err != nil {
			return nil, err
		}
		results = append(results, e)
	}
	return results, rows.Err()
}

// ListExtractions returns extractions with optional type filter.
func (s *Store) ListExtractions(extractionType string, limit, offset int) ([]ExtractionRow, error) {
	query := `SELECT id, conversation_id, type, title, content, metadata, output_path, extracted_at, model_used
		FROM extractions`
	var args []interface{}
	if extractionType != "" {
		query += " WHERE type = ?"
		args = append(args, extractionType)
	}
	query += " ORDER BY extracted_at DESC"
	if limit <= 0 {
		limit = 100
	}
	query += fmt.Sprintf(" LIMIT %d OFFSET %d", limit, offset)

	rows, err := s.db.Query(query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var results []ExtractionRow
	for rows.Next() {
		var e ExtractionRow
		if err := rows.Scan(&e.ID, &e.ConversationID, &e.Type, &e.Title, &e.Content,
			&e.Metadata, &e.OutputPath, &e.ExtractedAt, &e.ModelUsed); err != nil {
			return nil, err
		}
		results = append(results, e)
	}
	return results, rows.Err()
}

// HasExtraction checks if a conversation already has extractions.
func (s *Store) HasExtraction(conversationID string) (bool, error) {
	var count int
	err := s.db.QueryRow("SELECT COUNT(*) FROM extractions WHERE conversation_id = ?", conversationID).Scan(&count)
	return count > 0, err
}

// ── Helpers ──

// ftsEscape escapes special FTS5 characters and adds prefix matching.
// Each term matches exact OR prefix; multiple terms are AND-ed.
func ftsEscape(q string) string {
	terms := strings.Fields(q)
	for i, t := range terms {
		t = strings.ReplaceAll(t, `"`, `""`)
		terms[i] = `("` + t + `" OR "` + t + `"*)`
	}
	return strings.Join(terms, " AND ")
}

// ToJSON serializes a ConversationObject to JSON bytes.
func ToJSON(conv *ConversationObject) ([]byte, error) {
	return json.Marshal(conv)
}
