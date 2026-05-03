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

	"github.com/mihirsathe/wafers/internal/state"
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
	runGit(t, repo, "config", "user.name", "wafers")
	runGit(t, repo, "config", "user.email", "wafers@example.invalid")
	if err := os.WriteFile(filepath.Join(repo, "README.md"), []byte("hello\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(repo, "pkg"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(repo, "pkg", "value.txt"), []byte("base value\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	runGit(t, repo, "add", ".")
	runGit(t, repo, "-c", "user.name=wafers", "-c", "user.email=wafers@example.invalid", "commit", "-m", "initial")

	var stdout, stderr bytes.Buffer
	err := Run(ctx, []string{"add", "foo", "--from", repo, "--at", mountpoint, "--branch", "agent/foo"}, &stdout, &stderr)
	if err != nil {
		t.Fatalf("add failed: %v\nstdout:\n%s\nstderr:\n%s", err, stdout.String(), stderr.String())
	}
	if !strings.Contains(stdout.String(), "agent/foo") {
		t.Fatalf("add output did not mention branch: %s", stdout.String())
	}
	defer func() {
		_ = Run(ctx, []string{"rm", "foo", "--force"}, &bytes.Buffer{}, &bytes.Buffer{})
	}()
	store, err := state.OpenDefault()
	if err != nil {
		t.Fatal(err)
	}
	meta, err := store.Load("foo")
	if err != nil {
		t.Fatal(err)
	}
	if meta.LastCommit != meta.BaseCommit {
		t.Fatalf("last_commit after add = %s, want base %s", meta.LastCommit, meta.BaseCommit)
	}
	if branchTip := gitOutput(t, repo, "rev-parse", "refs/heads/agent/foo"); branchTip != meta.BaseCommit {
		t.Fatalf("branch tip after add = %s, want base %s", branchTip, meta.BaseCommit)
	}
	err = Run(ctx, []string{"add", "bar", "--from", repo, "--at", filepath.Join(root, "mnt2"), "--branch", "agent/foo"}, &stdout, &stderr)
	if err == nil || !strings.Contains(err.Error(), "already exists") {
		t.Fatalf("duplicate branch add err = %v, want already exists", err)
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

	baseIndexBefore, err := os.ReadFile(filepath.Join(repo, ".git", "index"))
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(mountpoint, "pkg", "value.txt"), []byte("wafer value\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(mountpoint, "new.txt"), []byte("new wafer file\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(filepath.Join(mountpoint, "README.md")); err != nil {
		t.Fatal(err)
	}
	stdout.Reset()
	err = Run(ctx, []string{"git-commit", "foo", "-m", "wafer changes"}, &stdout, &stderr)
	if err != nil {
		t.Fatal(err)
	}
	meta, err = store.Load("foo")
	if err != nil {
		t.Fatal(err)
	}
	if meta.LastCommit == "" {
		t.Fatal("last_commit was not set")
	}
	if parent := gitOutput(t, repo, "rev-parse", meta.LastCommit+"^"); parent != meta.BaseCommit {
		t.Fatalf("parent = %s, want %s", parent, meta.BaseCommit)
	}
	if branchTip := gitOutput(t, repo, "rev-parse", "refs/heads/agent/foo"); branchTip != meta.LastCommit {
		t.Fatalf("branch tip after commit = %s, want %s", branchTip, meta.LastCommit)
	}
	if got := gitOutput(t, repo, "show", meta.LastCommit+":pkg/value.txt"); got != "wafer value" {
		t.Fatalf("committed pkg/value.txt = %q", got)
	}
	if got := gitOutput(t, repo, "show", meta.LastCommit+":new.txt"); got != "new wafer file" {
		t.Fatalf("committed new.txt = %q", got)
	}
	if gitPathExists(repo, meta.LastCommit, "README.md") {
		t.Fatal("README.md should be deleted in committed tree")
	}
	baseIndexAfter, err := os.ReadFile(filepath.Join(repo, ".git", "index"))
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(baseIndexBefore, baseIndexAfter) {
		t.Fatal("base repo index changed during wafers git-commit")
	}

	if err := os.WriteFile(filepath.Join(mountpoint, "second.txt"), []byte("second\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	firstCommit := meta.LastCommit
	stdout.Reset()
	err = Run(ctx, []string{"git-commit", "foo", "--message", "second wafer change"}, &stdout, &stderr)
	if err != nil {
		t.Fatal(err)
	}
	meta, err = store.Load("foo")
	if err != nil {
		t.Fatal(err)
	}
	if parent := gitOutput(t, repo, "rev-parse", meta.LastCommit+"^"); parent != firstCommit {
		t.Fatalf("second parent = %s, want %s", parent, firstCommit)
	}
	if branchTip := gitOutput(t, repo, "rev-parse", "refs/heads/agent/foo"); branchTip != meta.LastCommit {
		t.Fatalf("branch tip after second commit = %s, want %s", branchTip, meta.LastCommit)
	}

	err = Run(ctx, []string{"git-commit", "foo", "-m", "empty"}, &stdout, &stderr)
	if err == nil || !strings.Contains(err.Error(), "nothing to commit") {
		t.Fatalf("empty commit err = %v, want nothing to commit", err)
	}

	runGit(t, repo, "update-ref", "refs/heads/agent/foo", meta.BaseCommit)
	if err := os.WriteFile(filepath.Join(mountpoint, "moved.txt"), []byte("moved\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	err = Run(ctx, []string{"git-commit", "foo", "-m", "should fail"}, &stdout, &stderr)
	if err == nil || !strings.Contains(err.Error(), "moved outside wafers") {
		t.Fatalf("moved branch commit err = %v, want moved outside wafers", err)
	}
	runGit(t, repo, "update-ref", "refs/heads/agent/foo", meta.LastCommit)

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
	if branchTip := gitOutput(t, repo, "rev-parse", "refs/heads/agent/foo"); branchTip != meta.LastCommit {
		t.Fatalf("branch should remain after rm, tip = %s, want %s", branchTip, meta.LastCommit)
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

func gitOutput(t *testing.T, dir string, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %v failed: %v\n%s", args, err, string(out))
	}
	return strings.TrimSpace(string(out))
}

func gitPathExists(dir, commit, path string) bool {
	cmd := exec.Command("git", "cat-file", "-e", commit+":"+path)
	cmd.Dir = dir
	return cmd.Run() == nil
}

func gitInsideWorkTree(dir string) bool {
	cmd := exec.Command("git", "rev-parse", "--is-inside-work-tree")
	cmd.Dir = dir
	out, err := cmd.Output()
	return err == nil && strings.TrimSpace(string(out)) == "true"
}
