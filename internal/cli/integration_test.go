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

type integrationEnv struct {
	ctx        context.Context
	root       string
	repo       string
	stateRoot  string
	mountpoint string
	store      *state.Store
	stdout     bytes.Buffer
	stderr     bytes.Buffer
}

func TestIntegrationAddCreatesBranchAndHidesGit(t *testing.T) {
	env := newIntegrationEnv(t)
	meta := env.addWafer(t, "foo", "agent/foo")

	if meta.LastCommit != meta.BaseCommit {
		t.Fatalf("last_commit after add = %s, want base %s", meta.LastCommit, meta.BaseCommit)
	}
	if branchTip := gitOutput(t, env.repo, "rev-parse", "refs/heads/agent/foo"); branchTip != meta.BaseCommit {
		t.Fatalf("branch tip after add = %s, want base %s", branchTip, meta.BaseCommit)
	}
	if _, err := os.Stat(filepath.Join(env.mountpoint, "README.md")); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(env.mountpoint, ".git")); !os.IsNotExist(err) {
		t.Fatalf(".git should be hidden, stat err = %v", err)
	}
	if gitInsideWorkTree(env.mountpoint) {
		t.Fatal("mountpoint still appears to be inside a Git worktree")
	}

	err := Run(env.ctx, []string{"add", "bar", "--from", env.repo, "--at", filepath.Join(env.root, "mnt2"), "--branch", "agent/foo"}, &env.stdout, &env.stderr)
	if err == nil || !strings.Contains(err.Error(), "already exists") {
		t.Fatalf("duplicate branch add err = %v, want already exists", err)
	}
}

func TestIntegrationGitCommitAdvancesBranch(t *testing.T) {
	env := newIntegrationEnv(t)
	meta := env.addWafer(t, "foo", "agent/foo")
	baseIndexBefore := readFile(t, filepath.Join(env.repo, ".git", "index"))

	writeFile(t, filepath.Join(env.mountpoint, "pkg", "value.txt"), "wafer value\n")
	writeFile(t, filepath.Join(env.mountpoint, "new.txt"), "new wafer file\n")
	if err := os.Remove(filepath.Join(env.mountpoint, "README.md")); err != nil {
		t.Fatal(err)
	}

	meta = env.gitCommit(t, "foo", "wafer changes")
	if parent := gitOutput(t, env.repo, "rev-parse", meta.LastCommit+"^"); parent != meta.BaseCommit {
		t.Fatalf("parent = %s, want %s", parent, meta.BaseCommit)
	}
	if branchTip := gitOutput(t, env.repo, "rev-parse", "refs/heads/agent/foo"); branchTip != meta.LastCommit {
		t.Fatalf("branch tip after commit = %s, want %s", branchTip, meta.LastCommit)
	}
	if got := gitOutput(t, env.repo, "show", meta.LastCommit+":pkg/value.txt"); got != "wafer value" {
		t.Fatalf("committed pkg/value.txt = %q", got)
	}
	if got := gitOutput(t, env.repo, "show", meta.LastCommit+":new.txt"); got != "new wafer file" {
		t.Fatalf("committed new.txt = %q", got)
	}
	if gitPathExists(env.repo, meta.LastCommit, "README.md") {
		t.Fatal("README.md should be deleted in committed tree")
	}
	if !bytes.Equal(baseIndexBefore, readFile(t, filepath.Join(env.repo, ".git", "index"))) {
		t.Fatal("base repo index changed during wafers git-commit")
	}
	if !strings.Contains(env.stdout.String(), shortSHA(meta.LastCommit)) {
		t.Fatalf("commit output did not include short sha: %s", env.stdout.String())
	}
}

func TestIntegrationRepeatedGitCommitChainsFromPreviousCommit(t *testing.T) {
	env := newIntegrationEnv(t)
	env.addWafer(t, "foo", "agent/foo")

	writeFile(t, filepath.Join(env.mountpoint, "first.txt"), "first\n")
	first := env.gitCommit(t, "foo", "first").LastCommit
	writeFile(t, filepath.Join(env.mountpoint, "second.txt"), "second\n")
	second := env.gitCommit(t, "foo", "second").LastCommit

	if parent := gitOutput(t, env.repo, "rev-parse", second+"^"); parent != first {
		t.Fatalf("second parent = %s, want %s", parent, first)
	}
	if branchTip := gitOutput(t, env.repo, "rev-parse", "refs/heads/agent/foo"); branchTip != second {
		t.Fatalf("branch tip after second commit = %s, want %s", branchTip, second)
	}
	err := Run(env.ctx, []string{"git-commit", "foo", "-m", "empty"}, &env.stdout, &env.stderr)
	if err == nil || !strings.Contains(err.Error(), "nothing to commit") {
		t.Fatalf("empty commit err = %v, want nothing to commit", err)
	}
}

func TestIntegrationMovedBranchRefFailsGitCommit(t *testing.T) {
	env := newIntegrationEnv(t)
	meta := env.addWafer(t, "foo", "agent/foo")
	writeFile(t, filepath.Join(env.mountpoint, "first.txt"), "first\n")
	meta = env.gitCommit(t, "foo", "first")

	runGit(t, env.repo, "update-ref", "refs/heads/agent/foo", meta.BaseCommit)
	writeFile(t, filepath.Join(env.mountpoint, "moved.txt"), "moved\n")
	err := Run(env.ctx, []string{"git-commit", "foo", "-m", "should fail"}, &env.stdout, &env.stderr)
	if err == nil || !strings.Contains(err.Error(), "moved outside wafers") {
		t.Fatalf("moved branch commit err = %v, want moved outside wafers", err)
	}
	runGit(t, env.repo, "update-ref", "refs/heads/agent/foo", meta.LastCommit)
}

func TestIntegrationRemoveKeepsBranch(t *testing.T) {
	env := newIntegrationEnv(t)
	env.addWafer(t, "foo", "agent/foo")
	writeFile(t, filepath.Join(env.mountpoint, "first.txt"), "first\n")
	meta := env.gitCommit(t, "foo", "first")

	env.stdout.Reset()
	if err := Run(env.ctx, []string{"rm", "foo", "--force"}, &env.stdout, &env.stderr); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(env.stateRoot, "wafers", "foo")); !os.IsNotExist(err) {
		t.Fatalf("wafer state should be removed, stat err = %v", err)
	}
	if branchTip := gitOutput(t, env.repo, "rev-parse", "refs/heads/agent/foo"); branchTip != meta.LastCommit {
		t.Fatalf("branch should remain after rm, tip = %s, want %s", branchTip, meta.LastCommit)
	}
}

func newIntegrationEnv(t *testing.T) *integrationEnv {
	t.Helper()
	if os.Getenv("WAFERS_INTEGRATION") != "1" {
		t.Skip("set WAFERS_INTEGRATION=1 to run fuse-overlayfs integration tests")
	}
	if runtime.GOOS != "linux" {
		t.Skip("integration test requires Linux")
	}

	root := t.TempDir()
	repo := filepath.Join(root, "repo")
	stateRoot := filepath.Join(root, "state")
	t.Setenv("XDG_STATE_HOME", stateRoot)
	createBaseRepo(t, repo)
	store, err := state.OpenDefault()
	if err != nil {
		t.Fatal(err)
	}
	return &integrationEnv{
		ctx:        context.Background(),
		root:       root,
		repo:       repo,
		stateRoot:  stateRoot,
		mountpoint: filepath.Join(root, "mnt"),
		store:      store,
	}
}

func (env *integrationEnv) addWafer(t *testing.T, name, branch string) *state.Meta {
	t.Helper()
	env.stdout.Reset()
	env.stderr.Reset()
	err := Run(env.ctx, []string{"add", name, "--from", env.repo, "--at", env.mountpoint, "--branch", branch}, &env.stdout, &env.stderr)
	if err != nil {
		t.Fatalf("add failed: %v\nstdout:\n%s\nstderr:\n%s", err, env.stdout.String(), env.stderr.String())
	}
	if !strings.Contains(env.stdout.String(), branch) {
		t.Fatalf("add output did not mention branch: %s", env.stdout.String())
	}
	t.Cleanup(func() {
		_ = Run(env.ctx, []string{"rm", name, "--force"}, &bytes.Buffer{}, &bytes.Buffer{})
	})
	meta, err := env.store.Load(name)
	if err != nil {
		t.Fatal(err)
	}
	return meta
}

func (env *integrationEnv) gitCommit(t *testing.T, name, message string) *state.Meta {
	t.Helper()
	env.stdout.Reset()
	env.stderr.Reset()
	err := Run(env.ctx, []string{"git-commit", name, "-m", message}, &env.stdout, &env.stderr)
	if err != nil {
		t.Fatal(err)
	}
	meta, err := env.store.Load(name)
	if err != nil {
		t.Fatal(err)
	}
	if meta.LastCommit == "" {
		t.Fatal("last_commit was not set")
	}
	return meta
}

func createBaseRepo(t *testing.T, repo string) {
	t.Helper()
	runGit(t, repo, "init")
	runGit(t, repo, "config", "user.name", "wafers")
	runGit(t, repo, "config", "user.email", "wafers@example.invalid")
	writeFile(t, filepath.Join(repo, "README.md"), "hello\n")
	if err := os.MkdirAll(filepath.Join(repo, "pkg"), 0o755); err != nil {
		t.Fatal(err)
	}
	writeFile(t, filepath.Join(repo, "pkg", "value.txt"), "base value\n")
	runGit(t, repo, "add", ".")
	runGit(t, repo, "commit", "-m", "initial")
}

func writeFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func readFile(t *testing.T, path string) []byte {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return data
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
