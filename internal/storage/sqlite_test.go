package storage

import (
	"os"
	"path/filepath"
	"testing"
)

func newTestStore(t *testing.T) *Store {
	t.Helper()
	dir := t.TempDir()
	s, err := New(filepath.Join(dir, "test.db"), filepath.Join(dir, "data"))
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	t.Cleanup(func() { s.Close() })
	return s
}

func testConv(sessionID string) *ConversationObject {
	return &ConversationObject{
		Version:           1,
		Source:            "claude-code",
		Device:            "test-mac",
		SessionID:         sessionID,
		Title:             "Test conversation " + sessionID,
		Date:              "2026-03-21",
		StartedAt:         "2026-03-21T10:00:00.000Z",
		EndedAt:           "2026-03-21T10:15:00.000Z",
		Project:           "test-project",
		ProjectPath:       "/Users/test/project",
		Model:             "claude-opus-4-6",
		MessageCount:      2,
		WordCount:         50,
		ContentHash:       "hash-" + sessionID,
		HasCode:           true,
		TotalInputTokens:  1000,
		TotalOutputTokens: 200,
		Metadata:          map[string]interface{}{"git_branch": "main"},
		Messages: []Message{
			{Role: "user", Content: "Hello", Timestamp: "2026-03-21T10:00:00.000Z", Seq: 0, Model: ""},
			{Role: "assistant", Content: "Hi there!", Timestamp: "2026-03-21T10:00:05.000Z", Seq: 1, Model: "claude-opus-4-6", InputTokens: 1000, OutputTokens: 200},
		},
	}
}

func TestUpsertAndGet(t *testing.T) {
	s := newTestStore(t)
	conv := testConv("upsert-1")

	if err := s.Upsert(conv); err != nil {
		t.Fatalf("Upsert: %v", err)
	}

	row, err := s.Get("upsert-1")
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if row == nil {
		t.Fatal("Get returned nil")
	}
	if row.Title != "Test conversation upsert-1" {
		t.Errorf("Title = %q, want %q", row.Title, "Test conversation upsert-1")
	}
	if row.Model != "claude-opus-4-6" {
		t.Errorf("Model = %q, want %q", row.Model, "claude-opus-4-6")
	}
	if row.TotalInputTokens != 1000 {
		t.Errorf("TotalInputTokens = %d, want 1000", row.TotalInputTokens)
	}
	if row.Status != "received" {
		t.Errorf("Status = %q, want %q", row.Status, "received")
	}
}

func TestGetMessages(t *testing.T) {
	s := newTestStore(t)
	conv := testConv("msg-1")
	s.Upsert(conv)

	row, _ := s.Get("msg-1")
	msgs, err := s.GetMessages(row.ID)
	if err != nil {
		t.Fatalf("GetMessages: %v", err)
	}
	if len(msgs) != 2 {
		t.Fatalf("got %d messages, want 2", len(msgs))
	}
	if msgs[1].Model != "claude-opus-4-6" {
		t.Errorf("msgs[1].Model = %q, want %q", msgs[1].Model, "claude-opus-4-6")
	}
	if msgs[1].InputTokens != 1000 {
		t.Errorf("msgs[1].InputTokens = %d, want 1000", msgs[1].InputTokens)
	}
}

func TestGetDetail(t *testing.T) {
	s := newTestStore(t)
	s.Upsert(testConv("detail-1"))

	detail, err := s.GetDetail("detail-1")
	if err != nil {
		t.Fatalf("GetDetail: %v", err)
	}
	if detail == nil {
		t.Fatal("GetDetail returned nil")
	}
	if len(detail.Messages) != 2 {
		t.Errorf("got %d messages, want 2", len(detail.Messages))
	}
}

func TestUpsertDedup(t *testing.T) {
	s := newTestStore(t)

	conv := testConv("dedup-1")
	s.Upsert(conv)

	row, _ := s.Get("dedup-1")
	id := row.ID

	// Same hash → status should not change after marking synced
	s.UpdateStatus(id, "synced")
	s.Upsert(conv) // same content_hash
	row2, _ := s.Get(id)
	if row2.Status != "synced" {
		t.Errorf("Status should stay synced, got %q", row2.Status)
	}

	// Different hash → status should reset to received
	conv.ContentHash = "hash-changed"
	s.Upsert(conv)
	row3, _ := s.Get(id)
	if row3.Status != "received" {
		t.Errorf("Status should reset to received, got %q", row3.Status)
	}
}

func TestList(t *testing.T) {
	s := newTestStore(t)
	s.Upsert(testConv("list-1"))
	s.Upsert(testConv("list-2"))

	rows, err := s.List(QueryParams{Limit: 10})
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(rows) != 2 {
		t.Errorf("got %d rows, want 2", len(rows))
	}

	// Filter by project
	rows, _ = s.List(QueryParams{Project: "test-project", Limit: 10})
	if len(rows) != 2 {
		t.Errorf("project filter: got %d, want 2", len(rows))
	}

	rows, _ = s.List(QueryParams{Project: "nonexistent", Limit: 10})
	if len(rows) != 0 {
		t.Errorf("nonexistent project: got %d, want 0", len(rows))
	}
}

func TestStats(t *testing.T) {
	s := newTestStore(t)
	s.Upsert(testConv("stat-1"))
	s.Upsert(testConv("stat-2"))

	row, _ := s.Get("stat-1")
	s.UpdateStatus(row.ID, "synced")

	stats, err := s.Stats()
	if err != nil {
		t.Fatalf("Stats: %v", err)
	}
	if stats.Total != 2 {
		t.Errorf("Total = %d, want 2", stats.Total)
	}
	if stats.Synced != 1 {
		t.Errorf("Synced = %d, want 1", stats.Synced)
	}
	if stats.Received != 1 {
		t.Errorf("Received = %d, want 1", stats.Received)
	}
}

func TestListProjects(t *testing.T) {
	s := newTestStore(t)
	s.Upsert(testConv("proj-1"))

	conv2 := testConv("proj-2")
	conv2.Project = "other-project"
	s.Upsert(conv2)

	projects, err := s.ListProjects()
	if err != nil {
		t.Fatalf("ListProjects: %v", err)
	}
	if len(projects) != 2 {
		t.Fatalf("got %d projects, want 2", len(projects))
	}
}

func TestSync(t *testing.T) {
	s := newTestStore(t)

	// Full sync — new conversation
	req := &SyncRequest{
		Version: 2, SyncMode: "full",
		Source: "claude-code", Device: "mac", SessionID: "sync-1",
		Title: "Sync test", Date: "2026-03-21",
		MessageCount: 2, WordCount: 50, ContentHash: "sync-hash-1",
		Messages: []Message{
			{Role: "user", Content: "Hi", Seq: 0},
			{Role: "assistant", Content: "Hello", Seq: 1},
		},
	}
	resp, err := s.Sync(req)
	if err != nil {
		t.Fatalf("Sync full: %v", err)
	}
	if resp.Action != "created" {
		t.Errorf("Action = %q, want created", resp.Action)
	}

	// Check — same hash
	req2 := &SyncRequest{
		Version: 2, SyncMode: "check",
		Source: "claude-code", Device: "mac", SessionID: "sync-1",
		ContentHash: "sync-hash-1",
	}
	resp2, _ := s.Sync(req2)
	if resp2.Action != "unchanged" {
		t.Errorf("Check same hash: Action = %q, want unchanged", resp2.Action)
	}

	// Check — different hash
	req3 := &SyncRequest{
		Version: 2, SyncMode: "check",
		Source: "claude-code", Device: "mac", SessionID: "sync-1",
		ContentHash: "sync-hash-2",
	}
	resp3, _ := s.Sync(req3)
	if resp3.Action != "need_full" {
		t.Errorf("Check diff hash: Action = %q, want need_full", resp3.Action)
	}

	// Delta — append messages
	req4 := &SyncRequest{
		Version: 2, SyncMode: "delta",
		Source: "claude-code", Device: "mac", SessionID: "sync-1",
		Title: "Sync test updated", Date: "2026-03-21",
		MessageCount: 3, WordCount: 70, ContentHash: "sync-hash-2",
		DeltaFromSeq: 2,
		Messages: []Message{
			{Role: "user", Content: "Follow up", Seq: 2},
		},
	}
	resp4, err := s.Sync(req4)
	if err != nil {
		t.Fatalf("Sync delta: %v", err)
	}
	if resp4.Action != "updated" {
		t.Errorf("Delta: Action = %q, want updated", resp4.Action)
	}

	// Verify 3 messages
	row, _ := s.Get("sync-1")
	msgs, _ := s.GetMessages(row.ID)
	if len(msgs) != 3 {
		t.Errorf("After delta: got %d messages, want 3", len(msgs))
	}

	// Check on nonexistent
	req5 := &SyncRequest{
		Version: 2, SyncMode: "check",
		Source: "claude-code", Device: "mac", SessionID: "nonexistent",
		ContentHash: "xxx",
	}
	resp5, _ := s.Sync(req5)
	if resp5.Action != "need_full" {
		t.Errorf("Check nonexistent: Action = %q, want need_full", resp5.Action)
	}
}

func TestTags(t *testing.T) {
	s := newTestStore(t)
	s.Upsert(testConv("tag-1"))
	row, _ := s.Get("tag-1")

	s.AddTag(row.ID, "has-code", true)
	s.AddTag(row.ID, "project:test", true)

	tags, err := s.GetTags(row.ID)
	if err != nil {
		t.Fatalf("GetTags: %v", err)
	}
	if len(tags) != 2 {
		t.Errorf("got %d tags, want 2", len(tags))
	}
}

func TestExtractions(t *testing.T) {
	s := newTestStore(t)
	s.Upsert(testConv("ext-1"))
	row, _ := s.Get("ext-1")

	s.InsertExtraction(row.ID, "work_log", "Summary", "Built OAuth flow", "{}", "", "claude-haiku")

	has, _ := s.HasExtraction(row.ID)
	if !has {
		t.Error("HasExtraction should be true")
	}

	exts, _ := s.GetExtractions(row.ID)
	if len(exts) != 1 {
		t.Fatalf("got %d extractions, want 1", len(exts))
	}
	if exts[0].Type != "work_log" {
		t.Errorf("Type = %q, want work_log", exts[0].Type)
	}
}

func TestBatchUpsert(t *testing.T) {
	s := newTestStore(t)
	convs := []*ConversationObject{testConv("batch-1"), testConv("batch-2")}
	count, err := s.UpsertBatch(convs)
	if err != nil {
		t.Fatalf("UpsertBatch: %v", err)
	}
	if count != 2 {
		t.Errorf("count = %d, want 2", count)
	}

	stats, _ := s.Stats()
	if stats.Total != 2 {
		t.Errorf("Total = %d, want 2", stats.Total)
	}
}

func TestMigrationIdempotent(t *testing.T) {
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "test.db")
	dataDir := filepath.Join(dir, "data")

	// Open and close twice — migrations should be idempotent
	s1, err := New(dbPath, dataDir)
	if err != nil {
		t.Fatalf("First open: %v", err)
	}
	s1.Close()

	s2, err := New(dbPath, dataDir)
	if err != nil {
		t.Fatalf("Second open: %v", err)
	}
	s2.Close()
}

func TestFTS5Search(t *testing.T) {
	s := newTestStore(t)

	conv1 := testConv("fts-1")
	conv1.Title = "Fix OAuth PKCE flow"
	s.Upsert(conv1)

	conv2 := testConv("fts-2")
	conv2.Title = "Add dark mode toggle"
	s.Upsert(conv2)

	// Search should find OAuth
	rows, err := s.List(QueryParams{Query: "OAuth", Limit: 10})
	if err != nil {
		t.Fatalf("FTS search: %v", err)
	}
	if len(rows) != 1 {
		t.Errorf("FTS 'OAuth': got %d, want 1", len(rows))
	}

	// Search should find dark mode
	rows, _ = s.List(QueryParams{Query: "dark mode", Limit: 10})
	if len(rows) != 1 {
		t.Errorf("FTS 'dark mode': got %d, want 1", len(rows))
	}
}

func TestSoftDelete(t *testing.T) {
	s := newTestStore(t)
	s.Upsert(testConv("del-1"))
	s.Upsert(testConv("del-2"))

	row, _ := s.Get("del-1")
	s.SoftDelete(row.ID)

	// Deleted conversation should be excluded from List by default
	rows, _ := s.List(QueryParams{Limit: 10})
	if len(rows) != 1 {
		t.Errorf("List after delete: got %d, want 1", len(rows))
	}

	// But still findable by direct Get
	deleted, _ := s.Get(row.ID)
	if deleted == nil || deleted.Status != "deleted" {
		t.Error("Get should still return deleted conversation")
	}

	// Re-upsert should NOT resurrect deleted conversation
	s.Upsert(testConv("del-1"))
	row2, _ := s.Get(row.ID)
	if row2.Status != "deleted" {
		t.Errorf("Status after re-upsert should be deleted, got %q", row2.Status)
	}
}

func TestUpdateFields(t *testing.T) {
	s := newTestStore(t)
	s.Upsert(testConv("upd-1"))
	row, _ := s.Get("upd-1")

	newTitle := "Updated title"
	s.UpdateFields(row.ID, UpdateFieldsParams{Title: &newTitle})

	row2, _ := s.Get(row.ID)
	if row2.Title != "Updated title" {
		t.Errorf("Title = %q, want 'Updated title'", row2.Title)
	}
}

func TestClearExtractions(t *testing.T) {
	s := newTestStore(t)
	s.Upsert(testConv("clr-1"))
	row, _ := s.Get("clr-1")

	s.InsertExtraction(row.ID, "work_log", "S", "C", "{}", "", "m")
	has, _ := s.HasExtraction(row.ID)
	if !has {
		t.Fatal("Should have extraction")
	}

	s.ClearExtractions(row.ID)
	has2, _ := s.HasExtraction(row.ID)
	if has2 {
		t.Error("Should have no extractions after clear")
	}
}

func TestStatsSummary(t *testing.T) {
	s := newTestStore(t)
	s.Upsert(testConv("sum-1"))
	s.Upsert(testConv("sum-2"))

	summary, err := s.GetStatsSummary()
	if err != nil {
		t.Fatalf("GetStatsSummary: %v", err)
	}
	if summary.Total != 2 {
		t.Errorf("Total = %d, want 2", summary.Total)
	}
	if summary.TotalInputTokens != 2000 {
		t.Errorf("TotalInputTokens = %d, want 2000", summary.TotalInputTokens)
	}
}

func init() {
	os.Setenv("TMPDIR", os.TempDir())
}
