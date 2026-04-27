package gitutil

import (
	"bytes"
	"context"
	"fmt"
	"os/exec"
	"path/filepath"
	"strings"
)

type Repo struct {
	Root   string
	GitDir string
	Head   string
}

func ResolveRepo(ctx context.Context, dir string) (Repo, error) {
	root, err := gitOutput(ctx, dir, "rev-parse", "--show-toplevel")
	if err != nil {
		return Repo{}, fmt.Errorf("%s is not inside a Git worktree: %w", dir, err)
	}
	gitDir, err := gitOutput(ctx, root, "rev-parse", "--git-dir")
	if err != nil {
		return Repo{}, err
	}
	if !filepath.IsAbs(gitDir) {
		gitDir = filepath.Join(root, gitDir)
	}
	head, err := gitOutput(ctx, root, "rev-parse", "HEAD")
	if err != nil {
		return Repo{}, err
	}
	return Repo{Root: root, GitDir: filepath.Clean(gitDir), Head: head}, nil
}

func ValidateBranch(ctx context.Context, branch string) error {
	if branch == "" {
		return fmt.Errorf("branch must not be empty")
	}
	if _, err := gitOutput(ctx, ".", "check-ref-format", "--branch", branch); err != nil {
		return fmt.Errorf("invalid branch %q: %w", branch, err)
	}
	return nil
}

func IsInsideWorkTree(ctx context.Context, dir string) bool {
	out, err := gitOutput(ctx, dir, "rev-parse", "--is-inside-work-tree")
	return err == nil && out == "true"
}

func gitOutput(ctx context.Context, dir string, args ...string) (string, error) {
	cmd := exec.CommandContext(ctx, "git", args...)
	cmd.Dir = dir
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	out, err := cmd.Output()
	if err != nil {
		msg := strings.TrimSpace(stderr.String())
		if msg == "" {
			msg = err.Error()
		}
		return "", fmt.Errorf("%s", msg)
	}
	return strings.TrimSpace(string(out)), nil
}
