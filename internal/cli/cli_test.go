package cli

import (
	"bytes"
	"context"
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
}

func TestParseGitCommitArgsRejectsInvalidForms(t *testing.T) {
	tests := [][]string{
		{"demo"},
		{"-m", "message"},
		{"demo", "-m"},
		{"demo", "-m", "one", "--message", "two"},
		{"demo", "--author", "me", "-m", "message"},
	}
	for _, args := range tests {
		if _, err := parseGitCommitArgs(args); err == nil {
			t.Fatalf("parseGitCommitArgs(%v) returned nil", args)
		}
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
