package cli

import (
	"bytes"
	"context"
	"strings"
	"testing"
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
	if !strings.Contains(stdout.String(), "wafers add") {
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
