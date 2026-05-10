package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/satmihir/wafers/internal/gitutil"
	"github.com/satmihir/wafers/internal/state"
)

func TestParseAddArgsSupportsDocumentedOrder(t *testing.T) {
	got, err := parseAddArgs([]string{"demo", "--at", "/tmp/view", "--branch", "agent/demo", "--from", "/repo"})
	if err != nil {
		t.Fatal(err)
	}
	if got.Name != "demo" || got.At != "/tmp/view" || got.Branch != "agent/demo" || got.From != "/repo" {
		t.Fatalf("unexpected args: %#v", got)
	}
}

func TestParseAddArgsSupportsJSON(t *testing.T) {
	got, err := parseAddArgs([]string{"demo", "--at", "/tmp/view", "--branch", "agent/demo", "--json"})
	if err != nil {
		t.Fatal(err)
	}
	if !got.JSON {
		t.Fatalf("expected JSON flag to be set: %#v", got)
	}
}

func TestParseAddArgsSupportsFlagsBeforeName(t *testing.T) {
	got, err := parseAddArgs([]string{"--at=/tmp/view", "--branch=agent/demo", "demo"})
	if err != nil {
		t.Fatal(err)
	}
	if got.Name != "demo" || got.At != "/tmp/view" || got.Branch != "agent/demo" || got.From != "." {
		t.Fatalf("unexpected args: %#v", got)
	}
}

func TestParseAddArgsRejectsInvalidForms(t *testing.T) {
	tests := [][]string{
		{},
		{"demo", "extra", "--at", "/tmp/view", "--branch", "agent/demo"},
		{"demo", "--at"},
		{"demo", "--unknown", "x", "--at", "/tmp/view", "--branch", "agent/demo"},
	}
	for _, args := range tests {
		if _, err := parseAddArgs(args); err == nil {
			t.Fatalf("parseAddArgs(%v) returned nil", args)
		}
	}
}

func TestParseRemoveArgsSupportsForceAfterName(t *testing.T) {
	got, err := parseRemoveArgs([]string{"demo", "--force"})
	if err != nil {
		t.Fatal(err)
	}
	if got.Name != "demo" || !got.Force {
		t.Fatalf("unexpected args: %#v", got)
	}
}

func TestParseRemoveArgsRejectsInvalidForms(t *testing.T) {
	tests := [][]string{
		{},
		{"demo", "extra"},
		{"demo", "--unknown"},
	}
	for _, args := range tests {
		if _, err := parseRemoveArgs(args); err == nil {
			t.Fatalf("parseRemoveArgs(%v) returned nil", args)
		}
	}
}

func TestParseGitCommitArgs(t *testing.T) {
	got, err := parseGitCommitArgs([]string{"demo", "-m", "message"})
	if err != nil {
		t.Fatal(err)
	}
	if got.Name != "demo" || got.Message != "message" {
		t.Fatalf("unexpected args: %#v", got)
	}

	got, err = parseGitCommitArgs([]string{"demo", "--message=other"})
	if err != nil {
		t.Fatal(err)
	}
	if got.Name != "demo" || got.Message != "other" {
		t.Fatalf("unexpected args: %#v", got)
	}

	got, err = parseGitCommitArgs([]string{"demo", "-m", "message", "--", "pkg/value.txt", "-dash.txt"})
	if err != nil {
		t.Fatal(err)
	}
	if got.Name != "demo" || got.Message != "message" || len(got.Paths) != 2 || got.Paths[0] != "pkg/value.txt" || got.Paths[1] != "-dash.txt" {
		t.Fatalf("unexpected path args: %#v", got)
	}
}

func TestParseGitCommitArgsRejectsInvalidForms(t *testing.T) {
	tests := [][]string{
		{"demo"},
		{"-m", "message"},
		{"demo", "-m"},
		{"demo", "-m", "message", "--"},
		{"demo", "-m", "one", "--message", "two"},
		{"demo", "--author", "me", "-m", "message"},
	}
	for _, args := range tests {
		if _, err := parseGitCommitArgs(args); err == nil {
			t.Fatalf("parseGitCommitArgs(%v) returned nil", args)
		}
	}
}

func TestValidateCommitPath(t *testing.T) {
	valid := []string{".", "README.md", "pkg/value.txt", "-dash.txt"}
	for _, path := range valid {
		if err := validateCommitPath(path); err != nil {
			t.Fatalf("validateCommitPath(%q) returned error: %v", path, err)
		}
	}
	invalid := []string{"", "/tmp/value.txt", "../outside.txt", "pkg/../outside.txt"}
	for _, path := range invalid {
		if err := validateCommitPath(path); err == nil {
			t.Fatalf("validateCommitPath(%q) returned nil", path)
		}
	}
}

func TestFirstStatusEntry(t *testing.T) {
	got := firstStatusEntry("\n M README.md\n?? scratch.txt\n")
	if got != " M README.md" {
		t.Fatalf("firstStatusEntry = %q", got)
	}
	if got := firstStatusEntry("\n\t\n"); got != "" {
		t.Fatalf("firstStatusEntry for blank status = %q", got)
	}
}

func TestRunTopLevelCommands(t *testing.T) {
	var stdout, stderr bytes.Buffer
	if err := Run(context.Background(), []string{"--help"}, &stdout, &stderr); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(stdout.String(), "Commands:") || !strings.Contains(stdout.String(), "Run \"wafers help <command>\"") {
		t.Fatalf("help output missing usage: %s", stdout.String())
	}

	stdout.Reset()
	stderr.Reset()
	if err := Run(context.Background(), nil, &stdout, &stderr); err == nil || !strings.Contains(err.Error(), "missing command") {
		t.Fatalf("missing command err = %v", err)
	}
	if !strings.Contains(stderr.String(), "Usage:") {
		t.Fatalf("missing command did not print usage: %s", stderr.String())
	}

	stdout.Reset()
	stderr.Reset()
	if err := Run(context.Background(), []string{"nope"}, &stdout, &stderr); err == nil || !strings.Contains(err.Error(), "unknown command") {
		t.Fatalf("unknown command err = %v", err)
	}
	if !strings.Contains(stderr.String(), "Usage:") {
		t.Fatalf("unknown command did not print usage: %s", stderr.String())
	}
}

func TestRunCommandHelp(t *testing.T) {
	tests := []struct {
		args []string
		want string
	}{
		{[]string{"help", "add"}, "wafers add - create a wafer"},
		{[]string{"add", "--help"}, "Usage:\n  wafers add <name>"},
		{[]string{"git-commit", "-h"}, "wafers git-commit - commit"},
		{[]string{"git-diff", "-h"}, "wafers git-diff - show"},
		{[]string{"rm", "help"}, "wafers rm - unmount"},
		{[]string{"doctor", "--help"}, "Install hints:"},
		{[]string{"skill", "--help"}, "wafers skill > SKILL.md"},
	}
	for _, tt := range tests {
		var stdout, stderr bytes.Buffer
		if err := Run(context.Background(), tt.args, &stdout, &stderr); err != nil {
			t.Fatalf("Run(%v) err = %v", tt.args, err)
		}
		if !strings.Contains(stdout.String(), tt.want) {
			t.Fatalf("Run(%v) output missing %q:\n%s", tt.args, tt.want, stdout.String())
		}
	}

	var stdout, stderr bytes.Buffer
	if err := Run(context.Background(), []string{"help", "nope"}, &stdout, &stderr); err == nil || !strings.Contains(err.Error(), "unknown command") {
		t.Fatalf("unknown help err = %v", err)
	}
	if err := Run(context.Background(), []string{"help", "add", "extra"}, &stdout, &stderr); err == nil || !strings.Contains(err.Error(), "usage: wafers help") {
		t.Fatalf("extra help err = %v", err)
	}
}

func TestParseNameOnlyArgs(t *testing.T) {
	got, err := parseNameOnlyArgs("git-diff", []string{"foo"})
	if err != nil {
		t.Fatal(err)
	}
	if got != "foo" {
		t.Fatalf("name = %q", got)
	}
	for _, args := range [][]string{{}, {"foo", "bar"}} {
		if _, err := parseNameOnlyArgs("git-diff", args); err == nil {
			t.Fatalf("parseNameOnlyArgs(%v) returned nil", args)
		}
	}
}

func TestRunVersion(t *testing.T) {
	var stdout, stderr bytes.Buffer
	old := Version
	Version = "test-version"
	t.Cleanup(func() { Version = old })

	if err := Run(context.Background(), []string{"--version"}, &stdout, &stderr); err != nil {
		t.Fatal(err)
	}
	if got := stdout.String(); got != "wafers test-version\n" {
		t.Fatalf("version output = %q", got)
	}
	if err := Run(context.Background(), []string{"version", "extra"}, &stdout, &stderr); err == nil {
		t.Fatal("version with extra args returned nil")
	}
}

func TestRunListShowsLastCommit(t *testing.T) {
	root := t.TempDir()
	t.Setenv("XDG_STATE_HOME", root)
	store := &state.Store{Root: filepath.Join(root, "wafers")}
	if err := store.Save(&state.Meta{
		Name:       "foo",
		Branch:     "agent/foo",
		BaseCommit: "1234567890abcdef",
		LastCommit: "fedcba0987654321",
		Mountpoint: filepath.Join(root, "mnt"),
	}); err != nil {
		t.Fatal(err)
	}
	var stdout, stderr bytes.Buffer
	if err := Run(context.Background(), []string{"ls"}, &stdout, &stderr); err != nil {
		t.Fatal(err)
	}
	out := stdout.String()
	for _, want := range []string{"NAME\tBRANCH\tBASE\tLAST\tSTATUS\tMOUNTPOINT", "1234567890ab", "fedcba098765"} {
		if !strings.Contains(out, want) {
			t.Fatalf("ls output missing %q:\n%s", want, out)
		}
	}
}

func TestParseListArgs(t *testing.T) {
	got, err := parseListArgs(nil)
	if err != nil {
		t.Fatal(err)
	}
	if got.JSON {
		t.Fatalf("unexpected JSON flag: %#v", got)
	}
	got, err = parseListArgs([]string{"--json"})
	if err != nil {
		t.Fatal(err)
	}
	if !got.JSON {
		t.Fatalf("expected JSON flag: %#v", got)
	}
	if _, err := parseListArgs([]string{"--bad"}); err == nil {
		t.Fatal("parseListArgs accepted unknown flag")
	}
}

func TestRunListJSON(t *testing.T) {
	root := t.TempDir()
	t.Setenv("XDG_STATE_HOME", root)
	store := &state.Store{Root: filepath.Join(root, "wafers")}
	if err := store.Save(&state.Meta{
		Name:       "foo",
		Branch:     "agent/foo",
		BaseCommit: "1234567890abcdef",
		LastCommit: "fedcba0987654321",
		Mountpoint: filepath.Join(root, "mnt"),
	}); err != nil {
		t.Fatal(err)
	}
	var stdout, stderr bytes.Buffer
	if err := Run(context.Background(), []string{"ls", "--json"}, &stdout, &stderr); err != nil {
		t.Fatal(err)
	}
	var out struct {
		Wafers []struct {
			Name       string `json:"name"`
			Branch     string `json:"branch"`
			BaseCommit string `json:"base_commit"`
			LastCommit string `json:"last_commit"`
			Status     string `json:"status"`
			Mountpoint string `json:"mountpoint"`
		} `json:"wafers"`
		InvalidEntries []string `json:"invalid_entries"`
	}
	if err := json.Unmarshal(stdout.Bytes(), &out); err != nil {
		t.Fatalf("invalid json output: %v\n%s", err, stdout.String())
	}
	if len(out.Wafers) != 1 || out.Wafers[0].Name != "foo" || out.Wafers[0].Status != "unmounted" {
		t.Fatalf("unexpected wafers output: %#v", out.Wafers)
	}
}

func TestRunGitDiffShowsCommittedBranchChanges(t *testing.T) {
	root := t.TempDir()
	t.Setenv("XDG_STATE_HOME", filepath.Join(root, "state"))
	repo := filepath.Join(root, "repo")
	runTestGit(t, repo, "init")
	runTestGit(t, repo, "config", "user.name", "wafers")
	runTestGit(t, repo, "config", "user.email", "wafers@example.invalid")
	writeTestFile(t, filepath.Join(repo, "README.md"), "base\n")
	runTestGit(t, repo, "add", ".")
	runTestGit(t, repo, "commit", "-m", "initial")
	base := testGitOutput(t, repo, "rev-parse", "HEAD")
	runTestGit(t, repo, "checkout", "-b", "agent/foo")
	writeTestFile(t, filepath.Join(repo, "README.md"), "changed\n")
	writeTestFile(t, filepath.Join(repo, "new.txt"), "new\n")
	runTestGit(t, repo, "add", ".")
	runTestGit(t, repo, "commit", "-m", "branch changes")
	runTestGit(t, repo, "checkout", "master")

	store, err := state.OpenDefault()
	if err != nil {
		t.Fatal(err)
	}
	if err := store.Save(&state.Meta{
		Version:    1,
		Name:       "foo",
		BaseRepo:   repo,
		BaseGitDir: filepath.Join(repo, ".git"),
		BaseCommit: base,
		Branch:     "agent/foo",
		LastCommit: testGitOutput(t, repo, "rev-parse", "agent/foo"),
		Mountpoint: filepath.Join(root, "missing-mount"),
	}); err != nil {
		t.Fatal(err)
	}

	var stdout, stderr bytes.Buffer
	if err := Run(context.Background(), []string{"git-diff", "foo"}, &stdout, &stderr); err != nil {
		t.Fatal(err)
	}
	out := stdout.String()
	for _, want := range []string{"diff --git a/README.md b/README.md", "+changed", "diff --git a/new.txt b/new.txt", "+new"} {
		if !strings.Contains(out, want) {
			t.Fatalf("git-diff output missing %q:\n%s", want, out)
		}
	}
}

func TestPlanAddBranch(t *testing.T) {
	ctx := context.Background()
	root := t.TempDir()
	repo := filepath.Join(root, "repo")
	runTestGit(t, repo, "init")
	runTestGit(t, repo, "config", "user.name", "wafers")
	runTestGit(t, repo, "config", "user.email", "wafers@example.invalid")
	writeTestFile(t, filepath.Join(repo, "README.md"), "base\n")
	runTestGit(t, repo, "add", ".")
	runTestGit(t, repo, "commit", "-m", "initial")
	resolved, err := gitutil.ResolveRepo(ctx, repo)
	if err != nil {
		t.Fatal(err)
	}
	store := &state.Store{Root: filepath.Join(root, "state")}

	newPlan, err := planAddBranch(ctx, store, resolved, "agent/new")
	if err != nil {
		t.Fatal(err)
	}
	if !newPlan.CreateBranch || newPlan.Replay || newPlan.BaseCommit != resolved.Head || newPlan.LastCommit != resolved.Head {
		t.Fatalf("new branch plan = %#v", newPlan)
	}

	runTestGit(t, repo, "branch", "agent/at-head", resolved.Head)
	atHeadPlan, err := planAddBranch(ctx, store, resolved, "agent/at-head")
	if err != nil {
		t.Fatal(err)
	}
	if atHeadPlan.CreateBranch || atHeadPlan.Replay || atHeadPlan.LastCommit != resolved.Head {
		t.Fatalf("at-head branch plan = %#v", atHeadPlan)
	}

	runTestGit(t, repo, "checkout", "-b", "agent/descendant")
	writeTestFile(t, filepath.Join(repo, "descendant.txt"), "descendant\n")
	runTestGit(t, repo, "add", ".")
	runTestGit(t, repo, "commit", "-m", "descendant")
	descendantTip := testGitOutput(t, repo, "rev-parse", "HEAD")
	runTestGit(t, repo, "checkout", "master")
	descendantPlan, err := planAddBranch(ctx, store, resolved, "agent/descendant")
	if err != nil {
		t.Fatal(err)
	}
	if descendantPlan.CreateBranch || !descendantPlan.Replay || descendantPlan.LastCommit != descendantTip {
		t.Fatalf("descendant branch plan = %#v, tip %s", descendantPlan, descendantTip)
	}

	runTestGit(t, repo, "checkout", "-b", "agent/sibling", resolved.Head)
	writeTestFile(t, filepath.Join(repo, "sibling.txt"), "sibling\n")
	runTestGit(t, repo, "add", ".")
	runTestGit(t, repo, "commit", "-m", "sibling")
	runTestGit(t, repo, "checkout", "master")
	writeTestFile(t, filepath.Join(repo, "base2.txt"), "base2\n")
	runTestGit(t, repo, "add", ".")
	runTestGit(t, repo, "commit", "-m", "advance base")
	advancedRepo, err := gitutil.ResolveRepo(ctx, repo)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := planAddBranch(ctx, store, advancedRepo, "agent/sibling"); err == nil || !strings.Contains(err.Error(), "does not descend") {
		t.Fatalf("non-descendant branch err = %v, want does not descend", err)
	}

	if err := store.Save(&state.Meta{Name: "owned", Branch: "agent/owned"}); err != nil {
		t.Fatal(err)
	}
	if _, err := planAddBranch(ctx, store, resolved, "agent/owned"); err == nil || !strings.Contains(err.Error(), "already owned") {
		t.Fatalf("owned branch err = %v, want already owned", err)
	}
}

func TestReplayExistingBranchUpdatesMountedTree(t *testing.T) {
	ctx := context.Background()
	root := t.TempDir()
	repoRoot := filepath.Join(root, "repo")
	runTestGit(t, repoRoot, "init")
	runTestGit(t, repoRoot, "config", "user.name", "wafers")
	runTestGit(t, repoRoot, "config", "user.email", "wafers@example.invalid")
	if err := os.MkdirAll(filepath.Join(repoRoot, "pkg"), 0o755); err != nil {
		t.Fatal(err)
	}
	writeTestFile(t, filepath.Join(repoRoot, "README.md"), "base\n")
	writeTestFile(t, filepath.Join(repoRoot, "pkg", "value.txt"), "base value\n")
	runTestGit(t, repoRoot, "add", ".")
	runTestGit(t, repoRoot, "commit", "-m", "initial")
	baseRepo, err := gitutil.ResolveRepo(ctx, repoRoot)
	if err != nil {
		t.Fatal(err)
	}

	runTestGit(t, repoRoot, "checkout", "-b", "agent/existing")
	writeTestFile(t, filepath.Join(repoRoot, "pkg", "value.txt"), "branch value\n")
	writeTestFile(t, filepath.Join(repoRoot, "branch-only.txt"), "branch only\n")
	if err := os.Remove(filepath.Join(repoRoot, "README.md")); err != nil {
		t.Fatal(err)
	}
	runTestGit(t, repoRoot, "add", "-A", ".")
	runTestGit(t, repoRoot, "commit", "-m", "branch changes")
	tip := testGitOutput(t, repoRoot, "rev-parse", "HEAD")
	runTestGit(t, repoRoot, "checkout", "master")

	mountpoint := filepath.Join(root, "mount")
	if err := os.MkdirAll(filepath.Join(mountpoint, "pkg"), 0o755); err != nil {
		t.Fatal(err)
	}
	writeTestFile(t, filepath.Join(mountpoint, "README.md"), "base\n")
	writeTestFile(t, filepath.Join(mountpoint, "pkg", "value.txt"), "base value\n")

	meta := &state.Meta{
		Name:       "existing",
		BaseGitDir: baseRepo.GitDir,
		BaseCommit: baseRepo.Head,
		Branch:     "agent/existing",
		Mountpoint: mountpoint,
		Index:      filepath.Join(root, "index"),
	}
	plan := addBranchPlan{BaseCommit: baseRepo.Head, LastCommit: tip, Replay: true}
	if err := replayExistingBranch(ctx, baseRepo, meta, plan); err != nil {
		t.Fatal(err)
	}
	if got := readTestFile(t, filepath.Join(mountpoint, "pkg", "value.txt")); got != "branch value\n" {
		t.Fatalf("pkg/value.txt = %q", got)
	}
	if got := readTestFile(t, filepath.Join(mountpoint, "branch-only.txt")); got != "branch only\n" {
		t.Fatalf("branch-only.txt = %q", got)
	}
	if _, err := os.Stat(filepath.Join(mountpoint, "README.md")); !os.IsNotExist(err) {
		t.Fatalf("README.md should be removed, stat err = %v", err)
	}
}

func TestRunSkill(t *testing.T) {
	var stdout, stderr bytes.Buffer
	if err := Run(context.Background(), []string{"skill"}, &stdout, &stderr); err != nil {
		t.Fatal(err)
	}
	out := stdout.String()
	for _, want := range []string{
		"# wafers",
		"Do not treat wafers as a sandbox",
		"wafers add <name>",
		"wafers git-commit <name>",
		"wafers ls",
		"git -C <base-repo> push origin <branch>",
		"wafers rm <name> --force",
	} {
		if !strings.Contains(out, want) {
			t.Fatalf("skill output missing %q:\n%s", want, out)
		}
	}

	stdout.Reset()
	if err := Run(context.Background(), []string{"skill", "extra"}, &stdout, &stderr); err == nil {
		t.Fatal("skill with extra args returned nil")
	}
}

func runTestGit(t *testing.T, dir string, args ...string) {
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

func testGitOutput(t *testing.T, dir string, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %v failed: %v\n%s", args, err, string(out))
	}
	return strings.TrimSpace(string(out))
}

func writeTestFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func readTestFile(t *testing.T, path string) string {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}
