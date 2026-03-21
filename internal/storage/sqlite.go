package storage

import (
	"crypto/sha256"
	"database/sql"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	_ "github.com/mattn/go-sqlite3"
)

// ── Data Types ──

// ConversationObject matches the plugin's ConversationObject protocol (v1).
type ConversationObject struct {
	Version      int       `json:"version"`
	Source       string    `json:"source"`
	Device       string    `json:"device"`
	SessionID    string    `json:"session_id"`
	Title        string    `json:"title"`
	Date         string    `json:"date"`
	Project      string    `json:"project"`
	ProjectPath  string    `json:"project_path"`
	Model        string    `json:"model,omitempty"`
	MessageCount int       `json:"message_count"`
	WordCount    int       `json:"word_count"`
	ContentHash  string    `json:"content_hash"`
	Messages     []Message `json:"messages"`
}

// Message represents a single message in a conversation.
type Message struct {
	Role      string `json:"role"`
	Content   string `json:"content"`
	TimeStr   string `json:"time_str"`
	IsContext bool   `json:"is_context"`
	Seq       int    `json:"seq"`
}

// ConversationRow represents a row from the conversations table.
type ConversationRow struct {
	ID           string  `json:"id"`
	SessionID    string  `json:"session_id"`
	SourceType   string  `json:"source_type"`
	Device       string  `json:"device"`
	Title        string  `json:"title"`
	Project      string  `json:"project"`
	ProjectPath  string  `json:"project_path"`
	Model        string  `json:"model"`
	StartedAt    string  `json:"started_at"`
	WordCount    int     `json:"word_count"`
	MessageCount int     `json:"message_count"`
	ContentHash  string  `json:"content_hash"`
	HasCode      bool    `json:"has_code"`
	Status       string  `json:"status"`
	CreatedAt    string  `json:"created_at"`
	UpdatedAt    string  `json:"updated_at"`
	SyncedAt     *string `json:"synced_at,omitempty"`
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
	return nil
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
			status         TEXT DEFAULT 'received' CHECK(status IN ('received','synced','failed','ignored')),
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
	hasCode := detectHasCode(conv.Messages)

	startedAt := conv.Date
	if startedAt == "" {
		startedAt = now
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
			 model, started_at, word_count, message_count, content_hash, has_code,
			 created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(source_type, device, session_id) DO UPDATE SET
			title=excluded.title,
			project=excluded.project,
			project_path=excluded.project_path,
			model=excluded.model,
			started_at=excluded.started_at,
			word_count=excluded.word_count,
			message_count=excluded.message_count,
			content_hash=excluded.content_hash,
			has_code=excluded.has_code,
			status=CASE
				WHEN conversations.status='ignored' THEN 'ignored'
				WHEN conversations.content_hash != excluded.content_hash THEN 'received'
				ELSE conversations.status
			END,
			updated_at=excluded.updated_at
	`, id, conv.SessionID, source, conv.Device, conv.Title,
		conv.Project, conv.ProjectPath, conv.Model, startedAt,
		conv.WordCount, conv.MessageCount, conv.ContentHash, hasCode,
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
			INSERT INTO messages (conversation_id, role, content, timestamp, seq, is_context)
			VALUES (?, ?, ?, ?, ?, ?)
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
			if _, err := stmt.Exec(id, m.Role, m.Content, m.TimeStr, m.Seq, isCtx); err != nil {
				return fmt.Errorf("insert message seq %d: %w", m.Seq, err)
			}
		}
	}

	return tx.Commit()
}

// Get retrieves a single conversation by ID (or session_id).
func (s *Store) Get(id string) (*ConversationRow, error) {
	row := s.db.QueryRow(`
		SELECT id, session_id, source_type, device, title, project, project_path,
		       model, started_at, word_count, message_count, content_hash, has_code,
		       status, created_at, updated_at
		FROM conversations
		WHERE id = ? OR session_id = ?
	`, id, id)

	var c ConversationRow
	var hasCode int
	err := row.Scan(&c.ID, &c.SessionID, &c.SourceType, &c.Device, &c.Title,
		&c.Project, &c.ProjectPath, &c.Model, &c.StartedAt,
		&c.WordCount, &c.MessageCount, &c.ContentHash, &hasCode,
		&c.Status, &c.CreatedAt, &c.UpdatedAt)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	c.HasCode = hasCode != 0
	return &c, nil
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
		SELECT id, conversation_id, role, content, timestamp, seq, is_context
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
			&m.Timestamp, &m.Seq, &isCtx); err != nil {
			return nil, err
		}
		m.IsContext = isCtx != 0
		messages = append(messages, m)
	}
	return messages, rows.Err()
}

// List returns conversations matching the query parameters.
func (s *Store) List(p QueryParams) ([]ConversationRow, error) {
	query := `SELECT id, session_id, source_type, device, title, project, project_path,
		model, started_at, word_count, message_count, content_hash, has_code,
		status, created_at, updated_at FROM conversations`
	var args []interface{}
	var conditions []string

	if p.Status != "" {
		conditions = append(conditions, "status = ?")
		args = append(args, p.Status)
	}
	if p.Project != "" {
		conditions = append(conditions, "project = ?")
		args = append(args, p.Project)
	}
	if p.Source != "" {
		conditions = append(conditions, "source_type = ?")
		args = append(args, p.Source)
	}

	// Full-text search via FTS5
	if p.Query != "" {
		conditions = append(conditions, "id IN (SELECT id FROM conversations_fts WHERE conversations_fts MATCH ?)")
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
		var c ConversationRow
		var hasCode int
		if err := rows.Scan(&c.ID, &c.SessionID, &c.SourceType, &c.Device, &c.Title,
			&c.Project, &c.ProjectPath, &c.Model, &c.StartedAt,
			&c.WordCount, &c.MessageCount, &c.ContentHash, &hasCode,
			&c.Status, &c.CreatedAt, &c.UpdatedAt); err != nil {
			return nil, err
		}
		c.HasCode = hasCode != 0
		results = append(results, c)
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

// ── Helpers ──

// ftsEscape escapes special FTS5 characters for safe MATCH queries.
func ftsEscape(q string) string {
	// Wrap each term in double quotes to treat as literal
	terms := strings.Fields(q)
	for i, t := range terms {
		terms[i] = `"` + strings.ReplaceAll(t, `"`, `""`) + `"`
	}
	return strings.Join(terms, " ")
}

// ToJSON serializes a ConversationObject to JSON bytes.
func ToJSON(conv *ConversationObject) ([]byte, error) {
	return json.Marshal(conv)
}
