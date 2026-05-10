package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/satmihir/wafers/internal/state"
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
	if err == nil || !strings.Contains(err.Error(), "already owned") {
		t.Fatalf("duplicate branch add err = %v, want already owned", err)
	}
}

func TestIntegrationAddJSONOutput(t *testing.T) {
	env := newIntegrationEnv(t)
	env.stdout.Reset()
	env.stderr.Reset()
	err := Run(env.ctx, []string{"add", "foo", "--from", env.repo, "--at", env.mountpoint, "--branch", "agent/foo", "--json"}, &env.stdout, &env.stderr)
	if err != nil {
		t.Fatalf("add failed: %v\nstdout:\n%s\nstderr:\n%s", err, env.stdout.String(), env.stderr.String())
	}
	t.Cleanup(func() {
		_ = Run(env.ctx, []string{"rm", "foo", "--force"}, &bytes.Buffer{}, &bytes.Buffer{})
	})
	var out struct {
		Name       string `json:"name"`
		Branch     string `json:"branch"`
		BaseCommit string `json:"base_commit"`
		LastCommit string `json:"last_commit"`
		Mountpoint string `json:"mountpoint"`
		Status     string `json:"status"`
	}
	if err := json.Unmarshal(env.stdout.Bytes(), &out); err != nil {
		t.Fatalf("invalid json output: %v\n%s", err, env.stdout.String())
	}
	if out.Name != "foo" || out.Branch != "agent/foo" || out.Status != "mounted" || out.Mountpoint != env.mountpoint {
		t.Fatalf("unexpected json output: %#v", out)
	}
	if out.BaseCommit == "" || out.LastCommit != out.BaseCommit {
		t.Fatalf("unexpected commit fields: %#v", out)
	}
}

func TestIntegrationAddRefusesDirtyBaseRepo(t *testing.T) {
	tests := []struct {
		name  string
		setup func(t *testing.T, env *integrationEnv)
		want  string
	}{
		{
			name: "modified tracked file",
			setup: func(t *testing.T, env *integrationEnv) {
				writeFile(t, filepath.Join(env.repo, "README.md"), "dirty\n")
			},
			want: "README.md",
		},
		{
			name: "untracked file",
			setup: func(t *testing.T, env *integrationEnv) {
				writeFile(t, filepath.Join(env.repo, "scratch.txt"), "scratch\n")
			},
			want: "scratch.txt",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			env := newIntegrationEnv(t)
			tt.setup(t, env)

			err := Run(env.ctx, []string{"add", "foo", "--from", env.repo, "--at", env.mountpoint, "--branch", "agent/foo"}, &env.stdout, &env.stderr)
			if err == nil || !strings.Contains(err.Error(), "base repo worktree is not clean") || !strings.Contains(err.Error(), tt.want) {
				t.Fatalf("add err = %v, want dirty base mentioning %q", err, tt.want)
			}
			if _, err := env.store.Load("foo"); !os.IsNotExist(err) {
				t.Fatalf("wafer metadata should not exist, err = %v", err)
			}
			if _, err := os.Stat(filepath.Join(env.stateRoot, "wafers", "foo")); !os.IsNotExist(err) {
				t.Fatalf("wafer state dir should not exist, err = %v", err)
			}
			if branchExists(t, env.repo, "agent/foo") {
				t.Fatal("branch should not be created")
			}
			if mountpointExists, err := pathExists(env.mountpoint); err != nil || mountpointExists {
				t.Fatalf("mountpoint should not be created, exists=%v err=%v", mountpointExists, err)
			}
		})
	}
}

func TestIntegrationAddAllowsIgnoredBaseFiles(t *testing.T) {
	env := newIntegrationEnv(t)
	writeFile(t, filepath.Join(env.repo, ".gitignore"), "ignored.log\n")
	runGit(t, env.repo, "add", ".gitignore")
	runGit(t, env.repo, "commit", "-m", "ignore logs")
	writeFile(t, filepath.Join(env.repo, "ignored.log"), "ignored\n")

	meta := env.addWafer(t, "foo", "agent/foo")
	if meta.LastCommit != meta.BaseCommit {
		t.Fatalf("last_commit after add = %s, want base %s", meta.LastCommit, meta.BaseCommit)
	}
}

func TestIntegrationAddExistingBranchReplaysBranchTreeAndContinues(t *testing.T) {
	env := newIntegrationEnv(t)
	base := gitOutput(t, env.repo, "rev-parse", "HEAD")
	runGit(t, env.repo, "checkout", "-b", "agent/foo")
	writeFile(t, filepath.Join(env.repo, "pkg", "value.txt"), "branch value\n")
	writeFile(t, filepath.Join(env.repo, "branch-only.txt"), "branch only\n")
	if err := os.Remove(filepath.Join(env.repo, "README.md")); err != nil {
		t.Fatal(err)
	}
	runGit(t, env.repo, "add", "-A", ".")
	runGit(t, env.repo, "commit", "-m", "branch changes")
	branchTip := gitOutput(t, env.repo, "rev-parse", "HEAD")
	runGit(t, env.repo, "checkout", "master")

	meta := env.addWafer(t, "foo", "agent/foo")
	if meta.BaseCommit != base || meta.LastCommit != branchTip {
		t.Fatalf("meta commits = base %s last %s, want %s %s", meta.BaseCommit, meta.LastCommit, base, branchTip)
	}
	if got := readFile(t, filepath.Join(env.mountpoint, "pkg", "value.txt")); string(got) != "branch value\n" {
		t.Fatalf("mounted pkg/value.txt = %q", got)
	}
	if got := readFile(t, filepath.Join(env.mountpoint, "branch-only.txt")); string(got) != "branch only\n" {
		t.Fatalf("mounted branch-only.txt = %q", got)
	}
	if _, err := os.Stat(filepath.Join(env.mountpoint, "README.md")); !os.IsNotExist(err) {
		t.Fatalf("README.md should be hidden in mount, stat err = %v", err)
	}
	if _, err := os.Stat(filepath.Join(env.repo, "branch-only.txt")); !os.IsNotExist(err) {
		t.Fatalf("branch-only.txt should not appear in base checkout, stat err = %v", err)
	}

	writeFile(t, filepath.Join(env.mountpoint, "continued.txt"), "continued\n")
	meta = env.gitCommit(t, "foo", "continue branch")
	if parent := gitOutput(t, env.repo, "rev-parse", meta.LastCommit+"^"); parent != branchTip {
		t.Fatalf("reattached commit parent = %s, want %s", parent, branchTip)
	}
}

func TestIntegrationAddRejectsNonDescendantExistingBranch(t *testing.T) {
	env := newIntegrationEnv(t)
	base := gitOutput(t, env.repo, "rev-parse", "HEAD")
	runGit(t, env.repo, "checkout", "-b", "agent/foo", base)
	writeFile(t, filepath.Join(env.repo, "branch.txt"), "branch\n")
	runGit(t, env.repo, "add", ".")
	runGit(t, env.repo, "commit", "-m", "branch")
	branchTip := gitOutput(t, env.repo, "rev-parse", "HEAD")
	runGit(t, env.repo, "checkout", "master")
	writeFile(t, filepath.Join(env.repo, "base2.txt"), "base2\n")
	runGit(t, env.repo, "add", ".")
	runGit(t, env.repo, "commit", "-m", "advance base")

	err := Run(env.ctx, []string{"add", "foo", "--from", env.repo, "--at", env.mountpoint, "--branch", "agent/foo"}, &env.stdout, &env.stderr)
	if err == nil || !strings.Contains(err.Error(), "does not descend") {
		t.Fatalf("non-descendant add err = %v, want does not descend", err)
	}
	if _, err := env.store.Load("foo"); !os.IsNotExist(err) {
		t.Fatalf("wafer metadata should not exist, err = %v", err)
	}
	if mountpointExists, err := pathExists(env.mountpoint); err != nil || mountpointExists {
		t.Fatalf("mountpoint should not be created, exists=%v err=%v", mountpointExists, err)
	}
	if got := gitOutput(t, env.repo, "rev-parse", "refs/heads/agent/foo"); got != branchTip {
		t.Fatalf("branch tip changed = %s, want %s", got, branchTip)
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

func TestIntegrationGitCommitSelectedPaths(t *testing.T) {
	env := newIntegrationEnv(t)
	meta := env.addWafer(t, "foo", "agent/foo")

	writeFile(t, filepath.Join(env.mountpoint, "pkg", "value.txt"), "selected value\n")
	writeFile(t, filepath.Join(env.mountpoint, "scratch.txt"), "scratch\n")
	writeFile(t, filepath.Join(env.mountpoint, "selected.txt"), "selected\n")
	if err := os.Remove(filepath.Join(env.mountpoint, "README.md")); err != nil {
		t.Fatal(err)
	}

	env.stdout.Reset()
	env.stderr.Reset()
	err := Run(env.ctx, []string{"git-commit", "foo", "-m", "selected paths", "--", "pkg/value.txt", "selected.txt"}, &env.stdout, &env.stderr)
	if err != nil {
		t.Fatal(err)
	}
	meta, err = env.store.Load("foo")
	if err != nil {
		t.Fatal(err)
	}
	if got := gitOutput(t, env.repo, "show", meta.LastCommit+":pkg/value.txt"); got != "selected value" {
		t.Fatalf("selected pkg/value.txt = %q", got)
	}
	if got := gitOutput(t, env.repo, "show", meta.LastCommit+":selected.txt"); got != "selected" {
		t.Fatalf("selected.txt = %q", got)
	}
	if gitPathExists(env.repo, meta.LastCommit, "scratch.txt") {
		t.Fatal("unselected scratch.txt should not be committed")
	}
	if got := gitOutput(t, env.repo, "show", meta.LastCommit+":README.md"); got != "hello" {
		t.Fatalf("unselected README.md deletion should not be committed, got %q", got)
	}
}

func TestIntegrationGitCommitSelectedDeletedPath(t *testing.T) {
	env := newIntegrationEnv(t)
	meta := env.addWafer(t, "foo", "agent/foo")
	if err := os.Remove(filepath.Join(env.mountpoint, "README.md")); err != nil {
		t.Fatal(err)
	}
	writeFile(t, filepath.Join(env.mountpoint, "scratch.txt"), "scratch\n")

	env.stdout.Reset()
	env.stderr.Reset()
	err := Run(env.ctx, []string{"git-commit", "foo", "-m", "delete readme", "--", "README.md"}, &env.stdout, &env.stderr)
	if err != nil {
		t.Fatal(err)
	}
	meta, err = env.store.Load("foo")
	if err != nil {
		t.Fatal(err)
	}
	if gitPathExists(env.repo, meta.LastCommit, "README.md") {
		t.Fatal("selected README.md deletion should be committed")
	}
	if gitPathExists(env.repo, meta.LastCommit, "scratch.txt") {
		t.Fatal("unselected scratch.txt should not be committed")
	}
}

func TestIntegrationGitCommitRejectsUnsafeSelectedPath(t *testing.T) {
	env := newIntegrationEnv(t)
	env.addWafer(t, "foo", "agent/foo")
	writeFile(t, filepath.Join(env.mountpoint, "pkg", "value.txt"), "selected value\n")

	err := Run(env.ctx, []string{"git-commit", "foo", "-m", "bad path", "--", "../outside.txt"}, &env.stdout, &env.stderr)
	if err == nil || !strings.Contains(err.Error(), "parent traversal") {
		t.Fatalf("unsafe selected path err = %v, want parent traversal", err)
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

func branchExists(t *testing.T, dir, branch string) bool {
	t.Helper()
	cmd := exec.Command("git", "show-ref", "--verify", "--quiet", "refs/heads/"+branch)
	cmd.Dir = dir
	return cmd.Run() == nil
}

func gitInsideWorkTree(dir string) bool {
	cmd := exec.Command("git", "rev-parse", "--is-inside-work-tree")
	cmd.Dir = dir
	out, err := cmd.Output()
	return err == nil && strings.TrimSpace(string(out)) == "true"
}

func pathExists(path string) (bool, error) {
	_, err := os.Stat(path)
	if err == nil {
		return true, nil
	}
	if os.IsNotExist(err) {
		return false, nil
	}
	return false, err
}
