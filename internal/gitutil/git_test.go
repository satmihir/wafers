package gitutil

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestValidateBranch(t *testing.T) {
	ctx := context.Background()
	valid := []string{"agent/foo", "foo", "feature.with-dots"}
	for _, branch := range valid {
		if err := ValidateBranch(ctx, branch); err != nil {
			t.Fatalf("ValidateBranch(%q) returned error: %v", branch, err)
		}
	}
	invalid := []string{"", "bad branch", "foo..bar", "-bad"}
	for _, branch := range invalid {
		if err := ValidateBranch(ctx, branch); err == nil {
			t.Fatalf("ValidateBranch(%q) returned nil", branch)
		}
	}
}

func TestLocalBranchRef(t *testing.T) {
	got := LocalBranchRef("agent/foo")
	want := "refs/heads/agent/foo"
	if got != want {
		t.Fatalf("LocalBranchRef() = %q, want %q", got, want)
	}
}

func TestResolveRepo(t *testing.T) {
	ctx := context.Background()
	repo := newGitRepo(t)
	got, err := ResolveRepo(ctx, filepath.Join(repo, "pkg"))
	if err != nil {
		t.Fatal(err)
	}
	wantRoot, err := filepath.EvalSymlinks(repo)
	if err != nil {
		t.Fatal(err)
	}
	gotRoot, err := filepath.EvalSymlinks(got.Root)
	if err != nil {
		t.Fatal(err)
	}
	if gotRoot != wantRoot {
		t.Fatalf("Root = %q, want %q", got.Root, repo)
	}
	if !filepath.IsAbs(got.GitDir) || filepath.Base(got.GitDir) != ".git" {
		t.Fatalf("GitDir = %q", got.GitDir)
	}
	if got.Head != gitOutCmd(t, repo, "rev-parse", "HEAD") {
		t.Fatalf("Head = %q", got.Head)
	}
}

func TestAuthorIdentityUsesRepoConfig(t *testing.T) {
	ctx := context.Background()
	repo := newGitRepo(t)
	ident, err := AuthorIdentity(ctx, repo)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(ident, "wafers <wafers@example.invalid>") {
		t.Fatalf("identity = %q", ident)
	}
}

func TestLocalBranchHelpers(t *testing.T) {
	ctx := context.Background()
	repo := newGitRepo(t)
	gitDir := filepath.Join(repo, ".git")
	head := gitOutCmd(t, repo, "rev-parse", "HEAD")

	if LocalBranchExists(ctx, gitDir, "agent/foo") {
		t.Fatal("branch unexpectedly exists")
	}
	if err := CreateLocalBranch(ctx, gitDir, "agent/foo", head); err != nil {
		t.Fatal(err)
	}
	if !LocalBranchExists(ctx, gitDir, "agent/foo") {
		t.Fatal("branch should exist")
	}
	tip, err := LocalBranchTip(ctx, gitDir, "agent/foo")
	if err != nil {
		t.Fatal(err)
	}
	if tip != head {
		t.Fatalf("tip = %s, want %s", tip, head)
	}
	if err := CreateLocalBranch(ctx, gitDir, "agent/foo", head); err == nil {
		t.Fatal("duplicate branch creation succeeded")
	}

	second := commitFile(t, repo, "second.txt", "second\n", "second")
	if err := UpdateLocalBranch(ctx, gitDir, "agent/foo", second, head); err != nil {
		t.Fatal(err)
	}
	if tip := gitOutCmd(t, repo, "rev-parse", "refs/heads/agent/foo"); tip != second {
		t.Fatalf("updated tip = %s, want %s", tip, second)
	}
	if err := UpdateLocalBranch(ctx, gitDir, "agent/foo", head, head); err == nil {
		t.Fatal("guarded update with stale old tip succeeded")
	}
	if err := DeleteLocalBranch(ctx, gitDir, "agent/foo"); err != nil {
		t.Fatal(err)
	}
	if LocalBranchExists(ctx, gitDir, "agent/foo") {
		t.Fatal("branch should be deleted")
	}
}

func TestPrivateIndexCommitFlowDoesNotTouchBaseIndex(t *testing.T) {
	ctx := context.Background()
	repo := newGitRepo(t)
	gitDir := filepath.Join(repo, ".git")
	parent := gitOutCmd(t, repo, "rev-parse", "HEAD")
	baseIndexBefore, err := os.ReadFile(filepath.Join(gitDir, "index"))
	if err != nil {
		t.Fatal(err)
	}

	if err := os.WriteFile(filepath.Join(repo, "README.md"), []byte("changed\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(repo, "new.txt"), []byte("new\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	indexPath := filepath.Join(t.TempDir(), "index")
	if err := ReadTree(ctx, gitDir, indexPath, parent); err != nil {
		t.Fatal(err)
	}
	if err := AddAll(ctx, gitDir, indexPath, repo); err != nil {
		t.Fatal(err)
	}
	tree, err := WriteTree(ctx, gitDir, indexPath)
	if err != nil {
		t.Fatal(err)
	}
	parentTree, err := TreeForCommit(ctx, gitDir, parent)
	if err != nil {
		t.Fatal(err)
	}
	if tree == parentTree {
		t.Fatal("private index tree did not include worktree changes")
	}
	commit, err := CommitTree(ctx, gitDir, tree, parent, "private index commit")
	if err != nil {
		t.Fatal(err)
	}
	if got, err := ShowFile(ctx, gitDir, commit, "README.md"); err != nil || got != "changed" {
		t.Fatalf("README in commit = %q err=%v", got, err)
	}
	if !PathExistsInCommit(ctx, gitDir, commit, "new.txt") {
		t.Fatal("new.txt missing from commit")
	}

	baseIndexAfter, err := os.ReadFile(filepath.Join(gitDir, "index"))
	if err != nil {
		t.Fatal(err)
	}
	if string(baseIndexAfter) != string(baseIndexBefore) {
		t.Fatal("base index changed")
	}
}

func newGitRepo(t *testing.T) string {
	t.Helper()
	repo := t.TempDir()
	runGitCmd(t, repo, "init")
	runGitCmd(t, repo, "config", "user.name", "wafers")
	runGitCmd(t, repo, "config", "user.email", "wafers@example.invalid")
	if err := os.MkdirAll(filepath.Join(repo, "pkg"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(repo, "README.md"), []byte("hello\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(repo, "pkg", "value.txt"), []byte("base\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	runGitCmd(t, repo, "add", ".")
	runGitCmd(t, repo, "commit", "-m", "initial")
	return repo
}

func commitFile(t *testing.T, repo, path, content, message string) string {
	t.Helper()
	if err := os.WriteFile(filepath.Join(repo, path), []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	runGitCmd(t, repo, "add", path)
	runGitCmd(t, repo, "commit", "-m", message)
	return gitOutCmd(t, repo, "rev-parse", "HEAD")
}

func runGitCmd(t *testing.T, dir string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %v failed: %v\n%s", args, err, string(out))
	}
}

func gitOutCmd(t *testing.T, dir string, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %v failed: %v\n%s", args, err, string(out))
	}
	return string(bytesTrimSpace(out))
}

func bytesTrimSpace(in []byte) []byte {
	for len(in) > 0 && (in[0] == '\n' || in[0] == '\r' || in[0] == '\t' || in[0] == ' ') {
		in = in[1:]
	}
	for len(in) > 0 {
		last := in[len(in)-1]
		if last != '\n' && last != '\r' && last != '\t' && last != ' ' {
			break
		}
		in = in[:len(in)-1]
	}
	return in
}
