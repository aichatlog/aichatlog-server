package llm

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"
)

type openaiAdapter struct {
	cfg    *OpenAIConfig
	client *http.Client
}

// NewOpenAIAdapter creates an OpenAI-compatible API adapter.
// Supports OpenAI, Groq, Together, local vLLM, etc.
func NewOpenAIAdapter(cfg *OpenAIConfig) Adapter {
	if cfg.BaseURL == "" {
		cfg.BaseURL = "https://api.openai.com/v1"
	}
	if cfg.Model == "" {
		cfg.Model = "gpt-4o-mini"
	}
	if cfg.BigModel == "" {
		cfg.BigModel = "gpt-4o"
	}
	return &openaiAdapter{
		cfg:    cfg,
		client: &http.Client{Timeout: 120 * time.Second},
	}
}

func (a *openaiAdapter) Name() string { return "openai" }

func (a *openaiAdapter) Extract(systemPrompt, userPrompt string) (string, error) {
	return a.call(a.cfg.Model, systemPrompt, userPrompt)
}

func (a *openaiAdapter) call(model, systemPrompt, userPrompt string) (string, error) {
	body, _ := json.Marshal(map[string]interface{}{
		"model": model,
		"messages": []map[string]string{
			{"role": "system", "content": systemPrompt},
			{"role": "user", "content": userPrompt},
		},
		"temperature": 0,
		"max_tokens":  4096,
	})

	url := a.cfg.BaseURL + "/chat/completions"
	req, err := http.NewRequest("POST", url, bytes.NewReader(body))
	if err != nil {
		return "", fmt.Errorf("create request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+a.cfg.APIKey)

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
		Choices []struct {
			Message struct {
				Content string `json:"content"`
			} `json:"message"`
		} `json:"choices"`
	}
	if err := json.Unmarshal(respBody, &result); err != nil {
		return "", fmt.Errorf("parse response: %w", err)
	}
	if len(result.Choices) == 0 {
		return "", fmt.Errorf("empty response")
	}
	return result.Choices[0].Message.Content, nil
}
