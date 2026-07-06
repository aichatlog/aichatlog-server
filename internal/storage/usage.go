package storage

import (
	"fmt"
	"strings"
)

// UsageQuery describes a token-usage aggregation over conversations. GroupBy is
// any subset of {"day","week","month","project","device","model"}; at most one
// time granularity is honored (the API layer folds day->week/month). From/To
// are inclusive YYYY-MM-DD bounds on the conversation start day. Project/Device/
// Model are exact-match filters (empty = no filter).
type UsageQuery struct {
	GroupBy []string
	From    string
	To      string
	Project string
	Device  string
	Model   string
}

// UsageBucket is one raw aggregation row. Day is the conversation start day
// (YYYY-MM-DD), populated whenever a time granularity is requested. Model is
// ALWAYS populated so the caller can apply per-model pricing even when the
// response is not grouped by model. Cost is not computed here.
type UsageBucket struct {
	Day                   string
	Project               string
	Device                string
	Model                 string
	Conversations         int64
	InputTokens           int64
	OutputTokens          int64
	CacheReadTokens       int64
	CacheCreation5mTokens int64
	CacheCreation1hTokens int64
}

func (q UsageQuery) wantsTime() bool {
	for _, g := range q.GroupBy {
		switch g {
		case "day", "week", "month":
			return true
		}
	}
	return false
}

func (q UsageQuery) wants(dim string) bool {
	for _, g := range q.GroupBy {
		if g == dim {
			return true
		}
	}
	return false
}

// whereClause builds the shared filter conditions (+ bound args) for usage
// queries over the conversation_model_usage (u) ⋈ conversations (c) join. All
// conditions reference the c./u. aliases, so callers must have them in scope
// (the u.model condition is only emitted when a model filter is set).
func (q UsageQuery) whereClause() (conds []string, args []interface{}) {
	conds = []string{"c.status != 'deleted'"}
	if q.From != "" {
		conds = append(conds, "c.usage_date >= ?")
		args = append(args, q.From)
	}
	if q.To != "" {
		conds = append(conds, "c.usage_date <= ?")
		args = append(args, q.To)
	}
	if q.Project != "" {
		conds = append(conds, "c.project = ?")
		args = append(args, q.Project)
	}
	if q.Device != "" {
		conds = append(conds, "c.device = ?")
		args = append(args, q.Device)
	}
	if q.Model != "" {
		conds = append(conds, "u.model = ?")
		args = append(args, q.Model)
	}
	return conds, args
}

// CountConversations returns the number of DISTINCT conversations matching the
// query's filters. It exists because the rollup query splits a conversation
// into one row per model, so summing per-model bucket counts would double-count
// multi-model conversations.
func (s *Store) CountConversations(q UsageQuery) (int64, error) {
	conds, args := q.whereClause()
	var n int64
	if q.Model != "" {
		err := s.db.QueryRow("SELECT COUNT(DISTINCT c.id) FROM conversation_model_usage u "+
			"JOIN conversations c ON c.id = u.conversation_id WHERE "+strings.Join(conds, " AND "), args...).Scan(&n)
		return n, err
	}
	err := s.db.QueryRow("SELECT COUNT(*) FROM conversations c WHERE "+strings.Join(conds, " AND "), args...).Scan(&n)
	return n, err
}

// UsageBuckets aggregates token usage over non-deleted conversations at the
// granularity of the requested dimensions plus model (always). It reads the
// per-(conversation,model) rollup table (populated with the cache 5m/1h split
// and correct per-model attribution), joined to conversations for the
// time/project/device dimensions. SELECT/GROUP BY columns come from a fixed
// whitelist (never user strings); only filter values are bound as parameters.
func (s *Store) UsageBuckets(q UsageQuery) ([]UsageBucket, error) {
	timeDim := q.wantsTime()
	wantProject := q.wants("project")
	wantDevice := q.wants("device")

	selects := make([]string, 0, 10)
	groups := make([]string, 0, 4)
	if timeDim {
		selects = append(selects, "c.usage_date AS day")
		groups = append(groups, "day")
	}
	if wantProject {
		selects = append(selects, "c.project AS project")
		groups = append(groups, "project")
	}
	if wantDevice {
		selects = append(selects, "c.device AS device")
		groups = append(groups, "device")
	}
	// model is always grouped so cost can be attributed per model.
	selects = append(selects, "u.model AS model")
	groups = append(groups, "u.model")

	selects = append(selects,
		// Each conversation contributes at most one rollup row per (dims, model)
		// group (PK is conversation_id+model), so COUNT(*) == COUNT(DISTINCT c.id)
		// here without the distinct-set overhead.
		"COUNT(*) AS conversations",
		"COALESCE(SUM(u.input_tokens),0) AS input_tokens",
		"COALESCE(SUM(u.output_tokens),0) AS output_tokens",
		"COALESCE(SUM(u.cache_read_tokens),0) AS cache_read_tokens",
		"COALESCE(SUM(u.cache_creation_5m_tokens),0) AS cc5m",
		"COALESCE(SUM(u.cache_creation_1h_tokens),0) AS cc1h",
	)

	where, args := q.whereClause()

	query := "SELECT " + strings.Join(selects, ", ") +
		" FROM conversation_model_usage u JOIN conversations c ON c.id = u.conversation_id" +
		" WHERE " + strings.Join(where, " AND ") +
		" GROUP BY " + strings.Join(groups, ", ")
	if timeDim {
		query += " ORDER BY day DESC"
	}

	rows, err := s.db.Query(query, args...)
	if err != nil {
		return nil, fmt.Errorf("usage query: %w", err)
	}
	defer rows.Close()

	var out []UsageBucket
	for rows.Next() {
		var b UsageBucket
		// Scan targets must match the dynamic SELECT order above.
		dest := make([]interface{}, 0, 10)
		if timeDim {
			dest = append(dest, &b.Day)
		}
		if wantProject {
			dest = append(dest, &b.Project)
		}
		if wantDevice {
			dest = append(dest, &b.Device)
		}
		dest = append(dest, &b.Model,
			&b.Conversations, &b.InputTokens, &b.OutputTokens,
			&b.CacheReadTokens, &b.CacheCreation5mTokens, &b.CacheCreation1hTokens)
		if err := rows.Scan(dest...); err != nil {
			return nil, fmt.Errorf("usage scan: %w", err)
		}
		out = append(out, b)
	}
	return out, rows.Err()
}
