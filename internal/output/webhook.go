package output

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"
)

// WebhookConfig for generic webhook adapter.
type WebhookConfig struct {
	URL     string            `json:"url"`
	Headers map[string]string `json:"headers,omitempty"`
}

type webhookAdapter struct {
	cfg    *WebhookConfig
	client *http.Client
}

// NewWebhookAdapter creates a generic webhook adapter.
// It POSTs JSON with path and content fields to the configured URL.
func NewWebhookAdapter(cfg *WebhookConfig) Adapter {
	return &webhookAdapter{
		cfg:    cfg,
		client: &http.Client{Timeout: 30 * time.Second},
	}
}

func (a *webhookAdapter) Name() string { return "webhook" }

func (a *webhookAdapter) Push(relPath string, content string) error {
	body, _ := json.Marshal(map[string]string{
		"path":    relPath,
		"content": content,
	})

	req, err := http.NewRequest("POST", a.cfg.URL, bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("create request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	for k, v := range a.cfg.Headers {
		req.Header.Set(k, v)
	}

	resp, err := a.client.Do(req)
	if err != nil {
		return fmt.Errorf("webhook request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 400 {
		respBody, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("webhook error %d: %s", resp.StatusCode, string(respBody))
	}
	return nil
}

func (a *webhookAdapter) Test() error {
	body, _ := json.Marshal(map[string]string{
		"type": "test",
		"from": "aichatlog-server",
	})

	req, err := http.NewRequest("POST", a.cfg.URL, bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	for k, v := range a.cfg.Headers {
		req.Header.Set(k, v)
	}

	resp, err := a.client.Do(req)
	if err != nil {
		return fmt.Errorf("webhook unreachable: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 400 {
		respBody, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("webhook error %d: %s", resp.StatusCode, string(respBody))
	}
	return nil
}
