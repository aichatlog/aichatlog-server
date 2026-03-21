package llm

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"
)

type anthropicAdapter struct {
	cfg    *AnthropicConfig
	client *http.Client
}

// NewAnthropicAdapter creates a Claude API adapter.
func NewAnthropicAdapter(cfg *AnthropicConfig) Adapter {
	if cfg.Model == "" {
		cfg.Model = "claude-haiku-4-5-20251001"
	}
	if cfg.BigModel == "" {
		cfg.BigModel = "claude-sonnet-4-20250514"
	}
	return &anthropicAdapter{
		cfg:    cfg,
		client: &http.Client{Timeout: 120 * time.Second},
	}
}

func (a *anthropicAdapter) Name() string { return "anthropic" }

func (a *anthropicAdapter) Extract(systemPrompt, userPrompt string) (string, error) {
	return a.call(a.cfg.Model, systemPrompt, userPrompt)
}

// ModelForWordCount returns the appropriate model based on conversation length.
func (a *anthropicAdapter) ModelForWordCount(words int, threshold int) string {
	if threshold > 0 && words >= threshold {
		return a.cfg.BigModel
	}
	return a.cfg.Model
}

func (a *anthropicAdapter) call(model, systemPrompt, userPrompt string) (string, error) {
	body, _ := json.Marshal(map[string]interface{}{
		"model":      model,
		"max_tokens": 4096,
		"system":     systemPrompt,
		"messages": []map[string]string{
			{"role": "user", "content": userPrompt},
		},
	})

	req, err := http.NewRequest("POST", "https://api.anthropic.com/v1/messages", bytes.NewReader(body))
	if err != nil {
		return "", fmt.Errorf("create request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("x-api-key", a.cfg.APIKey)
	req.Header.Set("anthropic-version", "2023-06-01")

	resp, err := a.client.Do(req)
	if err != nil {
		return "", fmt.Errorf("API request: %w", err)
	}
	defer resp.Body.Close()

	respBody, _ := io.ReadAll(resp.Body)

	if resp.StatusCode >= 400 {
		return "", fmt.Errorf("API error %d: %s", resp.StatusCode, string(respBody[:min(len(respBody), 500)]))
	}

	var result struct {
		Content []struct {
			Text string `json:"text"`
		} `json:"content"`
	}
	if err := json.Unmarshal(respBody, &result); err != nil {
		return "", fmt.Errorf("parse response: %w", err)
	}
	if len(result.Content) == 0 {
		return "", fmt.Errorf("empty response")
	}
	return result.Content[0].Text, nil
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
