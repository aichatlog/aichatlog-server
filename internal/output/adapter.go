package output

import "fmt"

// Adapter is the interface for pushing conversations to external systems.
type Adapter interface {
	// Name returns the adapter identifier (e.g. "local", "fns", "webhook").
	Name() string

	// Push writes a rendered note to the destination.
	// path is relative (e.g. "aichatlog/My Conversation.md").
	// content is the rendered markdown.
	Push(path string, content string) error

	// Test verifies the adapter configuration and connectivity.
	// Returns nil on success.
	Test() error
}

// Config holds adapter-specific configuration.
type Config struct {
	Adapter string         `json:"adapter"`
	Local   *LocalConfig   `json:"local,omitempty"`
	FNS     *FNSConfig     `json:"fns,omitempty"`
	Git     *GitConfig     `json:"git,omitempty"`
	Webhook *WebhookConfig `json:"webhook,omitempty"`
}

// NewAdapter creates an adapter from configuration.
func NewAdapter(cfg *Config) (Adapter, error) {
	if cfg == nil {
		return nil, fmt.Errorf("output config is nil")
	}
	switch cfg.Adapter {
	case "local":
		if cfg.Local == nil {
			return nil, fmt.Errorf("local config is required")
		}
		return NewLocalAdapter(cfg.Local), nil
	case "fns":
		if cfg.FNS == nil {
			return nil, fmt.Errorf("fns config is required")
		}
		return NewFNSAdapter(cfg.FNS), nil
	case "git":
		if cfg.Git == nil {
			return nil, fmt.Errorf("git config is required")
		}
		return NewGitAdapter(cfg.Git), nil
	case "webhook":
		if cfg.Webhook == nil {
			return nil, fmt.Errorf("webhook config is required")
		}
		return NewWebhookAdapter(cfg.Webhook), nil
	case "", "none":
		return nil, nil
	default:
		return nil, fmt.Errorf("unknown adapter: %s", cfg.Adapter)
	}
}
