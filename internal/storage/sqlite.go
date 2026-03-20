package storage

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"

	_ "github.com/mattn/go-sqlite3"
)

// ConversationObject matches the plugin's ConversationObject protocol.
type ConversationObject struct {
	Version      int       `json:"version"`
	Source       string    `json:"source"`
	Device       string    `json:"device"`
	SessionID    string    `json:"session_id"`
	Title        string    `json:"title"`
	Date         string    `json:"date"`
	Project      string    `json:"project"`
	ProjectPath  string    `json:"project_path"`
	MessageCount int       `json:"message_count"`
	WordCount    int       `json:"word_count"`
	ContentHash  string    `json:"content_hash"`
	Messages     []Message `json:"messages"`
}

type Message struct {
	Role      string `json:"role"`
	Content   string `json:"content"`
	TimeStr   string `json:"time_str"`
	IsContext bool   `json:"is_context"`
	Seq       int    `json:"seq"`
}

// ConversationRow represents a row from the conversations table.
type ConversationRow struct {
	SessionID    string  `json:"session_id"`
	Source       string  `json:"source"`
	Device       string  `json:"device"`
	Title        string  `json:"title"`
	Date         string  `json:"date"`
	Project      string  `json:"project"`
	ProjectPath  string  `json:"project_path"`
	MessageCount int     `json:"message_count"`
	WordCount    int     `json:"word_count"`
	ContentHash  string  `json:"content_hash"`
	Status       string  `json:"status"`
	SyncedAt     *string `json:"synced_at"`
	CreatedAt    string  `json:"created_at"`
	UpdatedAt    string  `json:"updated_at"`
}

// Store manages SQLite storage and file-based markdown storage.
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

func (s *Store) migrate() error {
	_, err := s.db.Exec(`
		CREATE TABLE IF NOT EXISTS conversations (
			session_id    TEXT PRIMARY KEY,
			source        TEXT DEFAULT 'claude-code',
			device        TEXT NOT NULL DEFAULT '',
			title         TEXT NOT NULL,
			date          TEXT NOT NULL,
			project       TEXT DEFAULT '',
			project_path  TEXT DEFAULT '',
			message_count INTEGER DEFAULT 0,
			word_count    INTEGER DEFAULT 0,
			content_hash  TEXT,
			status        TEXT DEFAULT 'received' CHECK(status IN ('received','synced','failed','ignored')),
			raw_json      TEXT,
			synced_at     TEXT,
			created_at    TEXT DEFAULT (datetime('now','localtime')),
			updated_at    TEXT DEFAULT (datetime('now','localtime'))
		);
		CREATE INDEX IF NOT EXISTS idx_conv_status ON conversations(status);
		CREATE INDEX IF NOT EXISTS idx_conv_date ON conversations(date);
		CREATE INDEX IF NOT EXISTS idx_conv_source ON conversations(source);
	`)
	return err
}

// Close closes the database connection.
func (s *Store) Close() error {
	return s.db.Close()
}

// Upsert inserts or updates a conversation from a ConversationObject.
func (s *Store) Upsert(conv *ConversationObject) error {
	rawJSON, err := json.Marshal(conv)
	if err != nil {
		return fmt.Errorf("marshal conversation: %w", err)
	}

	now := time.Now().Format("2006-01-02T15:04:05")

	source := conv.Source
	if source == "" {
		source = "claude-code"
	}

	_, err = s.db.Exec(`
		INSERT INTO conversations
			(session_id, source, device, title, date, project, project_path,
			 message_count, word_count, content_hash, raw_json, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(session_id) DO UPDATE SET
			source=excluded.source, device=excluded.device,
			title=excluded.title, date=excluded.date,
			project=excluded.project, project_path=excluded.project_path,
			message_count=excluded.message_count, word_count=excluded.word_count,
			content_hash=excluded.content_hash, raw_json=excluded.raw_json,
			status=CASE WHEN conversations.status='ignored' THEN 'ignored'
			            WHEN conversations.content_hash != excluded.content_hash THEN 'received'
			            ELSE conversations.status END,
			updated_at=?
	`, conv.SessionID, source, conv.Device, conv.Title, conv.Date,
		conv.Project, conv.ProjectPath, conv.MessageCount, conv.WordCount,
		conv.ContentHash, string(rawJSON), now, now, now)

	return err
}

// Get retrieves a single conversation by session ID.
func (s *Store) Get(sessionID string) (*ConversationRow, error) {
	row := s.db.QueryRow(
		"SELECT session_id, source, device, title, date, project, project_path, message_count, word_count, content_hash, status, synced_at, created_at, updated_at FROM conversations WHERE session_id = ?",
		sessionID,
	)
	var c ConversationRow
	err := row.Scan(&c.SessionID, &c.Source, &c.Device, &c.Title, &c.Date, &c.Project, &c.ProjectPath,
		&c.MessageCount, &c.WordCount, &c.ContentHash, &c.Status, &c.SyncedAt, &c.CreatedAt, &c.UpdatedAt)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	return &c, err
}

// GetRawJSON retrieves the full ConversationObject JSON for a session.
func (s *Store) GetRawJSON(sessionID string) (string, error) {
	var raw string
	err := s.db.QueryRow("SELECT raw_json FROM conversations WHERE session_id = ?", sessionID).Scan(&raw)
	if err == sql.ErrNoRows {
		return "", nil
	}
	return raw, err
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

// List returns conversations matching the query parameters.
func (s *Store) List(p QueryParams) ([]ConversationRow, error) {
	query := "SELECT session_id, source, device, title, date, project, project_path, message_count, word_count, content_hash, status, synced_at, created_at, updated_at FROM conversations"
	var args []interface{}
	var conditions []string

	if p.Status != "" {
		conditions = append(conditions, "status = ?")
		args = append(args, p.Status)
	}
	if p.Query != "" {
		conditions = append(conditions, "(title LIKE ? OR project LIKE ?)")
		args = append(args, "%"+p.Query+"%", "%"+p.Query+"%")
	}
	if p.Project != "" {
		conditions = append(conditions, "project = ?")
		args = append(args, p.Project)
	}
	if p.Source != "" {
		conditions = append(conditions, "source = ?")
		args = append(args, p.Source)
	}

	if len(conditions) > 0 {
		query += " WHERE "
		for i, c := range conditions {
			if i > 0 {
				query += " AND "
			}
			query += c
		}
	}

	// Sort
	sortCol := "date"
	if p.Sort == "title" || p.Sort == "project" || p.Sort == "word_count" || p.Sort == "created_at" {
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
		if err := rows.Scan(&c.SessionID, &c.Source, &c.Device, &c.Title, &c.Date, &c.Project, &c.ProjectPath,
			&c.MessageCount, &c.WordCount, &c.ContentHash, &c.Status, &c.SyncedAt, &c.CreatedAt, &c.UpdatedAt); err != nil {
			return nil, err
		}
		results = append(results, c)
	}
	return results, rows.Err()
}

// Stats returns conversation counts by status.
type Stats struct {
	Received int `json:"received"`
	Synced   int `json:"synced"`
	Failed   int `json:"failed"`
	Ignored  int `json:"ignored"`
	Total    int `json:"total"`
}

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
