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
