package processor

import (
	"encoding/json"
	"fmt"
	"log"
	"strings"

	"github.com/aichatlog/aichatlog-server/internal/llm"
	"github.com/aichatlog/aichatlog-server/internal/output"
	"github.com/aichatlog/aichatlog-server/internal/storage"
	"github.com/aichatlog/aichatlog-server/internal/textutil"
)

// Extractor handles LLM-based knowledge extraction from conversations.
type Extractor struct {
	store                 *storage.Store
	llm                   llm.Adapter
	adapter               output.Adapter
	syncDir               string
	minWords              int
	modelUpgradeThreshold int
	maxMonthlyBudget      int
	maxInputWords         int
	model                 string
}

// ExtractorConfig for the extraction pipeline.
type ExtractorConfig struct {
	MinWords              int
	SyncDir               string
	ModelUpgradeThreshold int
	MaxMonthlyBudget      int // max extractions per month, 0 = unlimited
	MaxInputWords         int // words of conversation sent to the LLM, 0 = 6000
}

// NewExtractor creates a new knowledge extractor. Returns nil if llm is nil.
func NewExtractor(store *storage.Store, llmAdapter llm.Adapter, outputAdapter output.Adapter, cfg *ExtractorConfig) *Extractor {
	if llmAdapter == nil {
		return nil
	}
	minWords := 100
	syncDir := "aichatlog"
	if cfg != nil {
		if cfg.MinWords > 0 {
			minWords = cfg.MinWords
		}
		if cfg.SyncDir != "" {
			syncDir = cfg.SyncDir
		}
	}
	modelUpgrade := 4000
	maxBudget := 0
	maxInput := 6000
	if cfg != nil && cfg.MaxInputWords > 0 {
		maxInput = cfg.MaxInputWords
	}
	if cfg != nil {
		if cfg.ModelUpgradeThreshold > 0 {
			modelUpgrade = cfg.ModelUpgradeThreshold
		}
		maxBudget = cfg.MaxMonthlyBudget
	}
	return &Extractor{
		store:                 store,
		llm:                   llmAdapter,
		adapter:               outputAdapter,
		syncDir:               syncDir,
		minWords:              minWords,
		modelUpgradeThreshold: modelUpgrade,
		maxMonthlyBudget:      maxBudget,
		maxInputWords:         maxInput,
		model:                 llmAdapter.Name(),
	}
}

// ExtractOne runs extraction on a single conversation.
// Skips if already extracted, below word threshold, or budget exceeded.
func (e *Extractor) ExtractOne(conversationID string) error {
	// Check monthly budget
	if e.maxMonthlyBudget > 0 {
		count, err := e.store.MonthlyExtractionCount()
		if err != nil {
			return fmt.Errorf("check budget: %w", err)
		}
		if count >= e.maxMonthlyBudget {
			log.Printf("extractor: monthly budget reached (%d/%d), skipping %s", count, e.maxMonthlyBudget, conversationID)
			return nil
		}
	}

	// Check if already extracted
	has, err := e.store.HasExtraction(conversationID)
	if err != nil {
		return err
	}
	if has {
		return nil
	}

	conv, err := e.store.Get(conversationID)
	if err != nil || conv == nil {
		return err
	}

	messages, err := e.store.GetMessages(conversationID)
	if err != nil {
		return err
	}

	// Skip short conversations. Count words from the dialogue itself (CJK-aware)
	// instead of the client-reported word_count, which splits on spaces only.
	words := 0
	for _, m := range messages {
		if !m.IsContext {
			words += textutil.CountWords(m.Content)
		}
	}
	if words < e.minWords {
		return nil
	}

	// Build prompt and call LLM; retry once if the reply is not valid JSON
	// (truncated output or a stray character inside a string).
	userPrompt := llm.BuildUserPrompt(conv, messages, e.maxInputWords)
	var result llm.ExtractionResult
	for attempt := 1; ; attempt++ {
		rawJSON, err := e.llm.Extract(llm.SystemPrompt(), userPrompt)
		if err != nil {
			return fmt.Errorf("LLM extraction: %w", err)
		}
		perr := parseExtraction(rawJSON, &result)
		if perr == nil {
			break
		}
		if attempt == 2 {
			return fmt.Errorf("parse LLM response: %w (raw: %.200s)", perr, rawJSON)
		}
		log.Printf("extractor: %s returned invalid JSON (%v), retrying", conversationID, perr)
	}

	// Store extractions
	modelUsed := e.model

	// Always store work_log
	if result.WorkLogEntry != "" {
		e.store.InsertExtraction(conversationID, "work_log", result.Summary,
			result.WorkLogEntry, "{}", "", modelUsed)
	}

	// Tech solutions
	for _, ts := range result.TechSolutions {
		meta, _ := json.Marshal(map[string]interface{}{
			"code": ts.Code, "gotchas": ts.Gotchas, "tags": ts.Tags,
		})
		content := fmt.Sprintf("## Problem\n%s\n\n## Solution\n%s", ts.Problem, ts.Solution)
		path := e.notePath("tech", conv.StartedAt, ts.Title)
		e.store.InsertExtraction(conversationID, "tech_solution", ts.Title,
			content, string(meta), path, modelUsed)

		if e.adapter != nil {
			note := renderTechNote(conv, ts)
			e.adapter.Push(path, note)
		}
	}

	// Concepts
	for _, c := range result.Concepts {
		meta, _ := json.Marshal(map[string]interface{}{
			"related": c.Related,
		})
		content := fmt.Sprintf("%s\n\n## Why it matters\n%s", c.Explanation, c.WhyItMatters)
		path := e.notePath("concepts", conv.StartedAt, c.Title)
		e.store.InsertExtraction(conversationID, "concept", c.Title,
			content, string(meta), path, modelUsed)

		if e.adapter != nil {
			note := renderConceptNote(conv, c)
			e.adapter.Push(path, note)
		}
	}

	// Prompts
	for _, p := range result.Prompts {
		content := fmt.Sprintf("## Prompt\n%s\n\n## When to use\n%s\n\n## Why it works\n%s",
			p.PromptText, p.WhenToUse, p.WhyEffective)
		path := e.notePath("prompts", conv.StartedAt, p.Title)
		e.store.InsertExtraction(conversationID, "prompt", p.Title,
			content, "{}", path, modelUsed)

		if e.adapter != nil {
			note := renderPromptNote(conv, p)
			e.adapter.Push(path, note)
		}
	}

	count := len(result.TechSolutions) + len(result.Concepts) + len(result.Prompts)
	if result.WorkLogEntry != "" {
		count++
	}
	log.Printf("extractor: %s → %d extractions (model=%s)", conversationID, count, modelUsed)
	return nil
}

// parseExtraction strips markdown fences and any text around the outermost
// JSON object before decoding.
func parseExtraction(raw string, out *llm.ExtractionResult) error {
	raw = strings.TrimSpace(raw)
	raw = strings.TrimPrefix(raw, "```json")
	raw = strings.TrimPrefix(raw, "```")
	raw = strings.TrimSuffix(raw, "```")
	if i, j := strings.Index(raw, "{"), strings.LastIndex(raw, "}"); i >= 0 && j > i {
		raw = raw[i : j+1]
	}
	*out = llm.ExtractionResult{}
	return json.Unmarshal([]byte(raw), out)
}

func (e *Extractor) notePath(category, date, title string) string {
	datePrefix := ""
	if len(date) >= 10 {
		datePrefix = date[:10] + "-"
	}
	slug := sanitizeFilename(title)
	if len(slug) > 60 {
		slug = slug[:60]
	}
	return fmt.Sprintf("%s/knowledge/%s/%s%s.md", e.syncDir, category, datePrefix, slug)
}

// ── Note Templates ──

func renderTechNote(conv *storage.ConversationRow, ts llm.TechSolution) string {
	tags := strings.Join(ts.Tags, ", ")
	gotchas := ""
	for _, g := range ts.Gotchas {
		gotchas += "- " + g + "\n"
	}
	return fmt.Sprintf(`---
type: tech-solution
date: %s
project: "%s"
tags: [tech-solution, %s]
source: %s
---

# %s

## Problem
%s

## Solution
%s

## Key code
%s

## Gotchas
%s`, conv.StartedAt[:min10(conv.StartedAt)], conv.Project, tags, conv.ID,
		ts.Title, ts.Problem, ts.Solution, ts.Code, gotchas)
}

func renderConceptNote(conv *storage.ConversationRow, c llm.Concept) string {
	related := ""
	for _, r := range c.Related {
		related += "- " + r + "\n"
	}
	return fmt.Sprintf(`---
type: concept
date: %s
source: %s
---

# %s

%s

## Why it matters
%s

## See also
%s`, conv.StartedAt[:min10(conv.StartedAt)], conv.ID,
		c.Title, c.Explanation, c.WhyItMatters, related)
}

func renderPromptNote(conv *storage.ConversationRow, p llm.PromptTemplate) string {
	return fmt.Sprintf(`---
type: prompt-template
date: %s
source: %s
---

# %s

## Prompt
%s

## When to use
%s

## Why it works
%s`, conv.StartedAt[:min10(conv.StartedAt)], conv.ID,
		p.Title, p.PromptText, p.WhenToUse, p.WhyEffective)
}

func min10(s string) int {
	if len(s) < 10 {
		return len(s)
	}
	return 10
}
