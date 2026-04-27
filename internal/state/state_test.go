package state

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestValidateName(t *testing.T) {
	valid := []string{"foo", "agent-1", "agent_1", "agent.1"}
	for _, name := range valid {
		if err := ValidateName(name); err != nil {
			t.Fatalf("ValidateName(%q) returned error: %v", name, err)
		}
	}
	invalid := []string{"", ".", "..", "../x", "x/y", "has space"}
	for _, name := range invalid {
		if err := ValidateName(name); err == nil {
			t.Fatalf("ValidateName(%q) returned nil", name)
		}
	}
}

func TestDefaultRootUsesXDGStateHome(t *testing.T) {
	t.Setenv("XDG_STATE_HOME", "/tmp/custom-state")
	root, err := DefaultRoot()
	if err != nil {
		t.Fatal(err)
	}
	if root != "/tmp/custom-state/wafers" {
		t.Fatalf("root = %q", root)
	}
}

func TestSaveLoadRoundTrip(t *testing.T) {
	store := &Store{Root: t.TempDir()}
	meta := &Meta{
		Version:    1,
		Name:       "foo",
		BaseRepo:   "/repo",
		BaseGitDir: "/repo/.git",
		BaseCommit: "abc123",
		Branch:     "agent/foo",
		Mountpoint: "/tmp/foo",
		Upperdir:   filepath.Join(store.WaferDir("foo"), "upper"),
		Workdir:    filepath.Join(store.WaferDir("foo"), "work"),
		Index:      filepath.Join(store.WaferDir("foo"), "index"),
		CreatedAt:  time.Now().UTC().Round(0),
		UpdatedAt:  time.Now().UTC().Round(0),
	}
	if err := store.Save(meta); err != nil {
		t.Fatal(err)
	}
	got, err := store.Load("foo")
	if err != nil {
		t.Fatal(err)
	}
	if got.Name != meta.Name || got.BaseCommit != meta.BaseCommit || got.Branch != meta.Branch {
		t.Fatalf("loaded meta mismatch: %#v", got)
	}
}

func TestUpperHasUserChangesIgnoresGitWhiteout(t *testing.T) {
	upper := t.TempDir()
	if err := os.WriteFile(filepath.Join(upper, ".wh..git"), nil, 0o644); err != nil {
		t.Fatal(err)
	}
	gitDir := filepath.Join(upper, ".git")
	if err := os.MkdirAll(gitDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(gitDir, ".wh.HEAD"), nil, 0o644); err != nil {
		t.Fatal(err)
	}
	changed, err := UpperHasUserChanges(upper)
	if err != nil {
		t.Fatal(err)
	}
	if changed {
		t.Fatal("expected internal whiteout-only upperdir to be unchanged")
	}
	if err := os.WriteFile(filepath.Join(upper, "README.md"), []byte("changed\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	changed, err = UpperHasUserChanges(upper)
	if err != nil {
		t.Fatal(err)
	}
	if !changed {
		t.Fatal("expected user file to count as a change")
	}
}
