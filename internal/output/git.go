package output

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
)

// GitConfig for git repo output adapter.
type GitConfig struct {
	RepoPath   string `json:"repo_path"`
	AutoCommit bool   `json:"auto_commit"`
	AutoPush   bool   `json:"auto_push"`
}

type gitAdapter struct {
	cfg *GitConfig
}

// NewGitAdapter creates a git repository adapter.
func NewGitAdapter(cfg *GitConfig) Adapter {
	return &gitAdapter{cfg: cfg}
}

func (a *gitAdapter) Name() string { return "git" }

func (a *gitAdapter) Push(relPath string, content string) error {
	fullPath := filepath.Join(a.cfg.RepoPath, relPath)
	if err := os.MkdirAll(filepath.Dir(fullPath), 0o755); err != nil {
		return fmt.Errorf("create directory: %w", err)
	}
	if err := os.WriteFile(fullPath, []byte(content), 0o644); err != nil {
		return fmt.Errorf("write file: %w", err)
	}

	if !a.cfg.AutoCommit {
		return nil
	}

	// git add
	if err := a.git("add", relPath); err != nil {
		return fmt.Errorf("git add: %w", err)
	}

	// git commit
	stem := filepath.Base(relPath)
	stem = stem[:len(stem)-len(filepath.Ext(stem))]
	if err := a.git("commit", "-m", "aichatlog: "+stem); err != nil {
		// If nothing to commit (file unchanged), that's OK
		return nil
	}

	if a.cfg.AutoPush {
		if err := a.git("push"); err != nil {
			return fmt.Errorf("git push: %w", err)
		}
	}

	return nil
}

func (a *gitAdapter) Test() error {
	cmd := exec.Command("git", "status", "--porcelain")
	cmd.Dir = a.cfg.RepoPath
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("not a git repo or git not available: %w", err)
	}
	return nil
}

func (a *gitAdapter) git(args ...string) error {
	cmd := exec.Command("git", args...)
	cmd.Dir = a.cfg.RepoPath
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("%s: %s", err, string(out))
	}
	return nil
}
