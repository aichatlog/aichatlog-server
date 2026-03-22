package config

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sync"

	"github.com/aichatlog/aichatlog/server/internal/llm"
	"github.com/aichatlog/aichatlog/server/internal/output"
)

// ServerConfig is the full server configuration.
type ServerConfig struct {
	Lang      string          `json:"lang"` // "en", "zh-CN", "zh-TW"
	Output    output.Config   `json:"output"`
	Processor ProcessorConfig `json:"processor"`
	LLM       llm.Config      `json:"llm"`
}

// ProcessorConfig controls the background processor.
type ProcessorConfig struct {
	Enabled   bool   `json:"enabled"`
	Interval  int    `json:"interval_seconds"`
	BatchSize int    `json:"batch_size"`
	SyncDir   string `json:"sync_dir"`
}

// Manager handles loading and saving configuration.
type Manager struct {
	mu       sync.RWMutex
	path     string
	config   ServerConfig
}

// NewManager creates a config manager. If the file doesn't exist, defaults are used.
func NewManager(configPath string) (*Manager, error) {
	m := &Manager{
		path: configPath,
		config: ServerConfig{
			Output: output.Config{Adapter: "none"},
			Processor: ProcessorConfig{
				Enabled:   true,
				Interval:  30,
				BatchSize: 20,
				SyncDir:   "aichatlog",
			},
		},
	}

	if _, err := os.Stat(configPath); err == nil {
		if err := m.load(); err != nil {
			return nil, fmt.Errorf("load config: %w", err)
		}
	}

	return m, nil
}

// Get returns a copy of the current configuration.
func (m *Manager) Get() ServerConfig {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.config
}

// Update replaces the configuration and saves to disk.
func (m *Manager) Update(cfg ServerConfig) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	m.config = cfg
	return m.save()
}

func (m *Manager) load() error {
	data, err := os.ReadFile(m.path)
	if err != nil {
		return err
	}
	return json.Unmarshal(data, &m.config)
}

func (m *Manager) save() error {
	if err := os.MkdirAll(filepath.Dir(m.path), 0o755); err != nil {
		return err
	}
	data, err := json.MarshalIndent(m.config, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(m.path, data, 0o644)
}
