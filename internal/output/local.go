package output

import (
	"fmt"
	"os"
	"path/filepath"
)

// LocalConfig for writing markdown files to a local directory.
type LocalConfig struct {
	Path string `json:"path"`
}

type localAdapter struct {
	basePath string
}

// NewLocalAdapter creates a local filesystem adapter.
func NewLocalAdapter(cfg *LocalConfig) Adapter {
	return &localAdapter{basePath: cfg.Path}
}

func (a *localAdapter) Name() string { return "local" }

func (a *localAdapter) Push(relPath string, content string) error {
	fullPath := filepath.Join(a.basePath, relPath)
	if err := os.MkdirAll(filepath.Dir(fullPath), 0o755); err != nil {
		return fmt.Errorf("create directory: %w", err)
	}
	if err := os.WriteFile(fullPath, []byte(content), 0o644); err != nil {
		return fmt.Errorf("write file: %w", err)
	}
	return nil
}

func (a *localAdapter) Test() error {
	if err := os.MkdirAll(a.basePath, 0o755); err != nil {
		return fmt.Errorf("cannot create directory %s: %w", a.basePath, err)
	}
	testPath := filepath.Join(a.basePath, ".aichatlog-test")
	if err := os.WriteFile(testPath, []byte("ok"), 0o644); err != nil {
		return fmt.Errorf("cannot write to %s: %w", a.basePath, err)
	}
	os.Remove(testPath)
	return nil
}
