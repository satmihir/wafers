package cli

import "testing"

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

func TestParseRemoveArgsSupportsForceAfterName(t *testing.T) {
	got, err := parseRemoveArgs([]string{"demo", "--force"})
	if err != nil {
		t.Fatal(err)
	}
	if got.Name != "demo" || !got.Force {
		t.Fatalf("unexpected args: %#v", got)
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
