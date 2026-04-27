package cli

import (
	"bytes"
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestIntegrationAddListRemove(t *testing.T) {
	if os.Getenv("WAFERS_INTEGRATION") != "1" {
		t.Skip("set WAFERS_INTEGRATION=1 to run fuse-overlayfs integration tests")
	}
	if runtime.GOOS != "linux" {
		t.Skip("integration test requires Linux")
	}

	ctx := context.Background()
	root := t.TempDir()
	repo := filepath.Join(root, "repo")
	stateRoot := filepath.Join(root, "state")
	mountpoint := filepath.Join(root, "mnt")
	t.Setenv("XDG_STATE_HOME", stateRoot)

	runGit(t, repo, "init")
	if err := os.WriteFile(filepath.Join(repo, "README.md"), []byte("hello\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	runGit(t, repo, "add", "README.md")
	runGit(t, repo, "-c", "user.name=wafers", "-c", "user.email=wafers@example.invalid", "commit", "-m", "initial")

	var stdout, stderr bytes.Buffer
	err := Run(ctx, []string{"add", "foo", "--from", repo, "--at", mountpoint, "--branch", "agent/foo"}, &stdout, &stderr)
	if err != nil {
		t.Fatalf("add failed: %v\nstdout:\n%s\nstderr:\n%s", err, stdout.String(), stderr.String())
	}
	if _, err := os.Stat(filepath.Join(mountpoint, "README.md")); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(mountpoint, ".git")); !os.IsNotExist(err) {
		t.Fatalf(".git should be hidden, stat err = %v", err)
	}
	if gitInsideWorkTree(mountpoint) {
		t.Fatal("mountpoint still appears to be inside a Git worktree")
	}

	stdout.Reset()
	err = Run(ctx, []string{"ls"}, &stdout, &stderr)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(stdout.String(), "foo") || !strings.Contains(stdout.String(), "agent/foo") {
		t.Fatalf("ls output did not include wafer: %s", stdout.String())
	}

	stdout.Reset()
	err = Run(ctx, []string{"rm", "foo", "--force"}, &stdout, &stderr)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(stateRoot, "wafers", "foo")); !os.IsNotExist(err) {
		t.Fatalf("wafer state should be removed, stat err = %v", err)
	}
}

func runGit(t *testing.T, dir string, args ...string) {
	t.Helper()
	if args[0] == "init" {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %v failed: %v\n%s", args, err, string(out))
	}
}

func gitInsideWorkTree(dir string) bool {
	cmd := exec.Command("git", "rev-parse", "--is-inside-work-tree")
	cmd.Dir = dir
	out, err := cmd.Output()
	return err == nil && strings.TrimSpace(string(out)) == "true"
}
