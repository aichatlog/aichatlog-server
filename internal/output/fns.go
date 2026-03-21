package output

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"
)

// FNSConfig for Fast Note Sync (Obsidian) adapter.
type FNSConfig struct {
	URL   string `json:"url"`
	Token string `json:"token"`
	Vault string `json:"vault"`
}

type fnsAdapter struct {
	cfg    *FNSConfig
	client *http.Client
}

// NewFNSAdapter creates a Fast Note Sync adapter.
func NewFNSAdapter(cfg *FNSConfig) Adapter {
	return &fnsAdapter{
		cfg:    cfg,
		client: &http.Client{Timeout: 30 * time.Second},
	}
}

func (a *fnsAdapter) Name() string { return "fns" }

func (a *fnsAdapter) Push(relPath string, content string) error {
	body, _ := json.Marshal(map[string]string{
		"vault":   a.cfg.Vault,
		"path":    relPath,
		"content": content,
	})

	req, err := http.NewRequest("POST", a.cfg.URL+"/api/note", bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("create request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+a.cfg.Token)
	req.Header.Set("token", a.cfg.Token)

	resp, err := a.client.Do(req)
	if err != nil {
		return fmt.Errorf("FNS request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 400 {
		respBody, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("FNS error %d: %s", resp.StatusCode, string(respBody))
	}
	return nil
}

func (a *fnsAdapter) Test() error {
	return a.Push(".aichatlog-test.md", "AIChatLog connection test: ok")
}
