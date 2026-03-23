package output

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

// NotionConfig for Notion API adapter.
type NotionConfig struct {
	APIKey     string `json:"api_key"`
	DatabaseID string `json:"database_id"` // Notion database to append pages to
}

type notionAdapter struct {
	cfg    *NotionConfig
	client *http.Client
}

// NewNotionAdapter creates a Notion API adapter.
// Pushes notes as pages into a Notion database.
func NewNotionAdapter(cfg *NotionConfig) Adapter {
	return &notionAdapter{
		cfg:    cfg,
		client: &http.Client{Timeout: 30 * time.Second},
	}
}

func (a *notionAdapter) Name() string { return "notion" }

func (a *notionAdapter) Push(relPath string, content string) error {
	// Extract title from path (last segment without extension)
	title := relPath
	if idx := strings.LastIndex(relPath, "/"); idx >= 0 {
		title = relPath[idx+1:]
	}
	title = strings.TrimSuffix(title, ".md")

	// Build Notion page with markdown content as blocks
	page := map[string]interface{}{
		"parent": map[string]string{
			"database_id": a.cfg.DatabaseID,
		},
		"properties": map[string]interface{}{
			"Name": map[string]interface{}{
				"title": []map[string]interface{}{
					{"text": map[string]string{"content": title}},
				},
			},
		},
		"children": markdownToNotionBlocks(content),
	}

	body, _ := json.Marshal(page)
	req, err := http.NewRequest("POST", "https://api.notion.com/v1/pages", bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("create request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+a.cfg.APIKey)
	req.Header.Set("Notion-Version", "2022-06-28")

	resp, err := a.client.Do(req)
	if err != nil {
		return fmt.Errorf("notion request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 400 {
		respBody, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("notion error %d: %s", resp.StatusCode, string(respBody[:min(len(respBody), 500)]))
	}
	return nil
}

func (a *notionAdapter) Test() error {
	// Try to retrieve the database to verify credentials
	req, err := http.NewRequest("GET", "https://api.notion.com/v1/databases/"+a.cfg.DatabaseID, nil)
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+a.cfg.APIKey)
	req.Header.Set("Notion-Version", "2022-06-28")

	resp, err := a.client.Do(req)
	if err != nil {
		return fmt.Errorf("notion unreachable: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 400 {
		respBody, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("notion error %d: %s", resp.StatusCode, string(respBody[:min(len(respBody), 500)]))
	}
	return nil
}

// markdownToNotionBlocks converts markdown text into Notion block objects.
// Uses paragraph blocks with rich text. Splits on double newlines for paragraphs.
func markdownToNotionBlocks(md string) []map[string]interface{} {
	var blocks []map[string]interface{}

	lines := strings.Split(md, "\n")
	var currentParagraph []string

	flush := func() {
		if len(currentParagraph) == 0 {
			return
		}
		text := strings.Join(currentParagraph, "\n")
		currentParagraph = nil
		if text == "" || text == "---" {
			return
		}
		// Detect headings
		if strings.HasPrefix(text, "# ") {
			blocks = append(blocks, notionHeading(1, strings.TrimPrefix(text, "# ")))
			return
		}
		if strings.HasPrefix(text, "## ") {
			blocks = append(blocks, notionHeading(2, strings.TrimPrefix(text, "## ")))
			return
		}
		if strings.HasPrefix(text, "### ") {
			blocks = append(blocks, notionHeading(3, strings.TrimPrefix(text, "### ")))
			return
		}
		// Code blocks
		if strings.HasPrefix(text, "```") {
			code := strings.TrimPrefix(text, "```")
			// Remove language identifier from first line and closing ```
			if idx := strings.Index(code, "\n"); idx >= 0 {
				lang := code[:idx]
				code = strings.TrimPrefix(code[idx+1:], "\n")
				code = strings.TrimSuffix(code, "```")
				code = strings.TrimSuffix(code, "\n")
				blocks = append(blocks, notionCode(code, lang))
				return
			}
		}
		// Quote blocks (> prefix)
		if strings.HasPrefix(text, "> ") {
			quoteText := strings.TrimPrefix(text, "> ")
			blocks = append(blocks, map[string]interface{}{
				"object": "block",
				"type":   "quote",
				"quote": map[string]interface{}{
					"rich_text": notionRichText(quoteText),
				},
			})
			return
		}
		// Truncate to Notion's 2000-char limit per block
		if len(text) > 2000 {
			text = text[:2000]
		}
		blocks = append(blocks, map[string]interface{}{
			"object": "block",
			"type":   "paragraph",
			"paragraph": map[string]interface{}{
				"rich_text": notionRichText(text),
			},
		})
	}

	for _, line := range lines {
		if line == "" {
			flush()
		} else {
			currentParagraph = append(currentParagraph, line)
		}
	}
	flush()

	// Notion API limits to 100 blocks per request
	if len(blocks) > 100 {
		blocks = blocks[:100]
	}
	return blocks
}

func notionRichText(text string) []map[string]interface{} {
	// Split into chunks of 2000 chars (Notion limit)
	var parts []map[string]interface{}
	for len(text) > 0 {
		chunk := text
		if len(chunk) > 2000 {
			chunk = chunk[:2000]
		}
		text = text[len(chunk):]
		parts = append(parts, map[string]interface{}{
			"type": "text",
			"text": map[string]string{"content": chunk},
		})
	}
	return parts
}

func notionHeading(level int, text string) map[string]interface{} {
	t := fmt.Sprintf("heading_%d", level)
	return map[string]interface{}{
		"object": "block",
		"type":   t,
		t: map[string]interface{}{
			"rich_text": notionRichText(text),
		},
	}
}

func notionCode(code, lang string) map[string]interface{} {
	if lang == "" {
		lang = "plain text"
	}
	return map[string]interface{}{
		"object": "block",
		"type":   "code",
		"code": map[string]interface{}{
			"rich_text": notionRichText(code),
			"language":  lang,
		},
	}
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
