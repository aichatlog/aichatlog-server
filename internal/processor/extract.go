package processor

import (
	"encoding/json"
	"fmt"
	"log"
	"strings"

	"github.com/aichatlog/aichatlog/server/internal/llm"
	"github.com/aichatlog/aichatlog/server/internal/output"
	"github.com/aichatlog/aichatlog/server/internal/storage"
)

// Extractor handles LLM-based knowledge extraction from conversations.
type Extractor struct {
	store    *storage.Store
	llm      llm.Adapter
	adapter  output.Adapter
	syncDir  string
	minWords int
	model    string
}

// ExtractorConfig for the extraction pipeline.
type ExtractorConfig struct {
	MinWords int
	SyncDir  string
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
	return &Extractor{
		store:    store,
		llm:      llmAdapter,
		adapter:  outputAdapter,
		syncDir:  syncDir,
		minWords: minWords,
		model:    llmAdapter.Name(),
	}
}

// ExtractOne runs extraction on a single conversation.
// Skips if already extracted or below word threshold.
func (e *Extractor) ExtractOne(conversationID string) error {
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

	// Skip short conversations
	if conv.WordCount < e.minWords {
		return nil
	}

	messages, err := e.store.GetMessages(conversationID)
	if err != nil {
		return err
	}

	// Build prompt and call LLM
	userPrompt := llm.BuildUserPrompt(conv, messages, 6000)
	rawJSON, err := e.llm.Extract(llm.SystemPrompt(), userPrompt)
	if err != nil {
		return fmt.Errorf("LLM extraction: %w", err)
	}

	// Clean up response (strip markdown fences if present)
	rawJSON = strings.TrimSpace(rawJSON)
	rawJSON = strings.TrimPrefix(rawJSON, "```json")
	rawJSON = strings.TrimPrefix(rawJSON, "```")
	rawJSON = strings.TrimSuffix(rawJSON, "```")
	rawJSON = strings.TrimSpace(rawJSON)

	var result llm.ExtractionResult
	if err := json.Unmarshal([]byte(rawJSON), &result); err != nil {
		return fmt.Errorf("parse LLM response: %w (raw: %.200s)", err, rawJSON)
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
