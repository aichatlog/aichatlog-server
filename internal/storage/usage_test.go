package storage

import "testing"

func usageConv(session, project, device, model, startedAt string, in, out, cacheRead, cacheCreate int) *ConversationObject {
	md := map[string]interface{}{}
	if cacheRead > 0 {
		md["cache_read_tokens"] = cacheRead
	}
	if cacheCreate > 0 {
		md["cache_creation_tokens"] = cacheCreate
	}
	return &ConversationObject{
		Version:           1,
		Source:            "claude-code",
		Device:            device,
		SessionID:         session,
		Title:             "t-" + session,
		Date:              startedAt[:10],
		StartedAt:         startedAt,
		Project:           project,
		ProjectPath:       "/x/" + project,
		Model:             model,
		MessageCount:      1,
		TotalInputTokens:  in,
		TotalOutputTokens: out,
		Metadata:          md,
		Messages:          []Message{{Role: "user", Content: "hi", Seq: 0}},
	}
}

func findBucket(bs []UsageBucket, pred func(UsageBucket) bool) *UsageBucket {
	for i := range bs {
		if pred(bs[i]) {
			return &bs[i]
		}
	}
	return nil
}

func TestUsageBucketsGroupByDayProjectDevice(t *testing.T) {
	s := newTestStore(t)
	convs := []*ConversationObject{
		usageConv("s1", "proj-a", "srv-1", "claude-opus-4-8", "2026-07-06T10:00:00.000Z", 1000, 200, 5000, 300),
		usageConv("s2", "proj-a", "srv-1", "claude-opus-4-8", "2026-07-06T12:00:00.000Z", 500, 100, 2000, 0),
		usageConv("s3", "proj-b", "srv-2", "claude-haiku-4-5", "2026-07-05T09:00:00.000Z", 800, 400, 0, 0),
	}
	for _, c := range convs {
		if err := s.Upsert(c); err != nil {
			t.Fatalf("Upsert %s: %v", c.SessionID, err)
		}
	}

	got, err := s.UsageBuckets(UsageQuery{GroupBy: []string{"day", "project", "device"}})
	if err != nil {
		t.Fatalf("UsageBuckets: %v", err)
	}
	// s1+s2 share day/project/device/model -> one folded row; s3 -> another.
	if len(got) != 2 {
		t.Fatalf("rows = %d, want 2: %+v", len(got), got)
	}

	a := findBucket(got, func(b UsageBucket) bool { return b.Project == "proj-a" })
	if a == nil {
		t.Fatal("no proj-a row")
	}
	if a.Conversations != 2 || a.InputTokens != 1500 || a.OutputTokens != 300 ||
		a.CacheReadTokens != 7000 || a.CacheCreation5mTokens != 300 {
		t.Fatalf("proj-a aggregation wrong: %+v", a)
	}
	if a.Day != "2026-07-06" || a.Device != "srv-1" || a.Model != "claude-opus-4-8" {
		t.Fatalf("proj-a dimensions wrong: %+v", a)
	}
}

func TestUsageBucketsModelAlwaysGrouped(t *testing.T) {
	s := newTestStore(t)
	// Same day/project/device, different models -> must NOT collapse (cost is per-model).
	_ = s.Upsert(usageConv("s1", "proj-a", "srv-1", "claude-opus-4-8", "2026-07-06T10:00:00.000Z", 1000, 200, 5000, 300))
	_ = s.Upsert(usageConv("s2", "proj-a", "srv-1", "claude-haiku-4-5", "2026-07-06T11:00:00.000Z", 100, 50, 0, 0))

	got, err := s.UsageBuckets(UsageQuery{}) // no group_by
	if err != nil {
		t.Fatalf("UsageBuckets: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("want 2 per-model buckets, got %d: %+v", len(got), got)
	}
}

func TestUsageBucketsFilters(t *testing.T) {
	s := newTestStore(t)
	_ = s.Upsert(usageConv("s1", "proj-a", "srv-1", "claude-opus-4-8", "2026-07-06T10:00:00.000Z", 1000, 200, 5000, 300))
	_ = s.Upsert(usageConv("s2", "proj-a", "srv-2", "claude-opus-4-8", "2026-07-06T11:00:00.000Z", 100, 50, 0, 0))
	_ = s.Upsert(usageConv("s3", "proj-a", "srv-1", "claude-opus-4-8", "2026-07-01T11:00:00.000Z", 999, 0, 0, 0))

	// Device filter.
	got, _ := s.UsageBuckets(UsageQuery{GroupBy: []string{"project"}, Device: "srv-1"})
	var in int64
	for _, b := range got {
		in += b.InputTokens
	}
	if in != 1999 {
		t.Fatalf("device-filtered input = %d, want 1999", in)
	}

	// Date range filter (inclusive, on start day).
	got, _ = s.UsageBuckets(UsageQuery{From: "2026-07-05", To: "2026-07-06"})
	var in2 int64
	for _, b := range got {
		in2 += b.InputTokens
	}
	if in2 != 1100 {
		t.Fatalf("date-filtered input = %d, want 1100 (excludes 07-01)", in2)
	}
}

func TestUsageBucketsModelsUsageRollup(t *testing.T) {
	s := newTestStore(t)
	c := &ConversationObject{
		Version: 1, Source: "claude-code", Device: "srv-1", SessionID: "m1", Title: "m",
		Date: "2026-07-06", StartedAt: "2026-07-06T10:00:00.000Z", Project: "proj-x",
		Model:            "claude-opus-4-8",
		MessageCount:     1,
		TotalInputTokens: 150000, TotalOutputTokens: 60000,
		CacheReadTokens: 3000000, CacheCreation5mTokens: 200000, CacheCreation1hTokens: 100000,
		ModelsUsage: []ModelUsage{
			{Model: "claude-opus-4-8", InputTokens: 100000, OutputTokens: 40000, CacheReadTokens: 2000000, CacheCreation5mTokens: 200000, CacheCreation1hTokens: 100000},
			{Model: "claude-haiku-4-5", InputTokens: 50000, OutputTokens: 20000, CacheReadTokens: 1000000},
		},
		Messages: []Message{{Role: "user", Content: "hi", Seq: 0}},
	}
	if err := s.Upsert(c); err != nil {
		t.Fatalf("Upsert: %v", err)
	}

	got, err := s.UsageBuckets(UsageQuery{GroupBy: []string{"model"}})
	if err != nil {
		t.Fatalf("UsageBuckets: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("want 2 per-model rows, got %d: %+v", len(got), got)
	}
	opus := findBucket(got, func(b UsageBucket) bool { return b.Model == "claude-opus-4-8" })
	haiku := findBucket(got, func(b UsageBucket) bool { return b.Model == "claude-haiku-4-5" })
	if opus == nil || haiku == nil {
		t.Fatalf("missing model rows: %+v", got)
	}
	if opus.InputTokens != 100000 || opus.CacheReadTokens != 2000000 ||
		opus.CacheCreation5mTokens != 200000 || opus.CacheCreation1hTokens != 100000 {
		t.Fatalf("opus rollup wrong: %+v", opus)
	}
	if haiku.InputTokens != 50000 || haiku.CacheReadTokens != 1000000 {
		t.Fatalf("haiku rollup wrong: %+v", haiku)
	}

	// Re-sync must be idempotent (delete+reinsert), not additive.
	if err := s.Upsert(c); err != nil {
		t.Fatalf("re-Upsert: %v", err)
	}
	got2, _ := s.UsageBuckets(UsageQuery{GroupBy: []string{"model"}})
	o2 := findBucket(got2, func(b UsageBucket) bool { return b.Model == "claude-opus-4-8" })
	if o2 == nil || o2.InputTokens != 100000 {
		t.Fatalf("re-sync double-counted: %+v", got2)
	}
}

func TestMigrateV6Backfill(t *testing.T) {
	s := newTestStore(t) // already migrated to v6

	// Simulate a pre-v6 conversation: metadata-blob cache, no promoted columns,
	// no rollup row.
	if _, err := s.db.Exec(`INSERT INTO conversations
		(id, session_id, source_type, device, title, project, started_at, model,
		 total_input_tokens, total_output_tokens, metadata, status)
		VALUES ('old','old','claude-code','srv','t','proj','2026-07-01T10:00:00.000Z',
		 'claude-opus-4-8', 1000, 500,
		 '{"cache_read_tokens":40000,"cache_creation_tokens":5000}', 'received')`); err != nil {
		t.Fatalf("seed: %v", err)
	}
	// Wipe the v6-populated state to mimic a row that existed before the migration.
	s.db.Exec(`UPDATE conversations SET cache_read_tokens=0, cache_creation_5m_tokens=0,
		cache_creation_1h_tokens=0, usage_date='' WHERE id='old'`)
	s.db.Exec(`DELETE FROM conversation_model_usage WHERE conversation_id='old'`)

	// Re-run the migration backfill (idempotent).
	tx, err := s.db.Begin()
	if err != nil {
		t.Fatalf("begin: %v", err)
	}
	if err := s.migrateV6(tx); err != nil {
		tx.Rollback()
		t.Fatalf("migrateV6: %v", err)
	}
	if err := tx.Commit(); err != nil {
		t.Fatalf("commit: %v", err)
	}

	got, err := s.UsageBuckets(UsageQuery{GroupBy: []string{"day", "model"}, Project: "proj"})
	if err != nil {
		t.Fatalf("UsageBuckets: %v", err)
	}
	b := findBucket(got, func(b UsageBucket) bool { return b.Model == "claude-opus-4-8" })
	if b == nil {
		t.Fatalf("no rollup row after backfill: %+v", got)
	}
	if b.Day != "2026-07-01" || b.InputTokens != 1000 || b.OutputTokens != 500 ||
		b.CacheReadTokens != 40000 || b.CacheCreation5mTokens != 5000 || b.CacheCreation1hTokens != 0 {
		t.Fatalf("backfill wrong: %+v", b)
	}
}

func TestUsageBucketsExcludesDeleted(t *testing.T) {
	s := newTestStore(t)
	c := usageConv("s1", "proj-a", "srv-1", "claude-opus-4-8", "2026-07-06T10:00:00.000Z", 1000, 200, 0, 0)
	if err := s.Upsert(c); err != nil {
		t.Fatalf("Upsert: %v", err)
	}
	id := generateID(c.Source, c.Device, c.SessionID)
	if err := s.UpdateStatus(id, "deleted"); err != nil {
		t.Fatalf("UpdateStatus: %v", err)
	}
	got, _ := s.UsageBuckets(UsageQuery{})
	if len(got) != 0 {
		t.Fatalf("deleted conversation should be excluded, got %+v", got)
	}
}
