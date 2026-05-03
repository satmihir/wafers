package state

import (
	"os"
	"path/filepath"
	"strings"
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

func TestDefaultRootFallsBackToHome(t *testing.T) {
	home := t.TempDir()
	t.Setenv("XDG_STATE_HOME", "")
	t.Setenv("HOME", home)
	root, err := DefaultRoot()
	if err != nil {
		t.Fatal(err)
	}
	want := filepath.Join(home, ".local", "state", "wafers")
	if root != want {
		t.Fatalf("root = %q, want %q", root, want)
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

func TestListHandlesMissingValidAndCorruptMetadata(t *testing.T) {
	store := &Store{Root: t.TempDir()}
	metas, bad, err := store.List()
	if err != nil {
		t.Fatal(err)
	}
	if len(metas) != 0 || len(bad) != 0 {
		t.Fatalf("missing state list = metas %v bad %v", metas, bad)
	}

	valid := &Meta{Name: "good", Version: 1, BaseCommit: "abc", Branch: "agent/good"}
	if err := store.Save(valid); err != nil {
		t.Fatal(err)
	}
	badDir := store.WaferDir("bad")
	if err := os.MkdirAll(badDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(badDir, "meta.json"), []byte("{not-json"), 0o644); err != nil {
		t.Fatal(err)
	}

	metas, bad, err = store.List()
	if err != nil {
		t.Fatal(err)
	}
	if len(metas) != 1 || metas[0].Name != "good" {
		t.Fatalf("metas = %#v", metas)
	}
	if len(bad) != 1 || bad[0] != "bad" {
		t.Fatalf("bad = %#v", bad)
	}
}

func TestRemoveDeletesOnlyWaferDir(t *testing.T) {
	store := &Store{Root: t.TempDir()}
	if err := store.Save(&Meta{Name: "foo", Version: 1}); err != nil {
		t.Fatal(err)
	}
	other := filepath.Join(store.Root, "keep")
	if err := os.WriteFile(other, []byte("keep"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := store.Remove("foo"); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(store.WaferDir("foo")); !os.IsNotExist(err) {
		t.Fatalf("wafer dir still exists or unexpected err: %v", err)
	}
	if _, err := os.Stat(other); err != nil {
		t.Fatalf("unrelated state removed: %v", err)
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

func TestUpperHasUserChangesScenarios(t *testing.T) {
	changed, err := UpperHasUserChanges(filepath.Join(t.TempDir(), "missing"))
	if err != nil {
		t.Fatal(err)
	}
	if changed {
		t.Fatal("missing upperdir should not count as changed")
	}

	upper := t.TempDir()
	changed, err = UpperHasUserChanges(upper)
	if err != nil {
		t.Fatal(err)
	}
	if changed {
		t.Fatal("empty upperdir should not count as changed")
	}

	nested := filepath.Join(upper, "pkg")
	if err := os.MkdirAll(nested, 0o755); err != nil {
		t.Fatal(err)
	}
	changed, err = UpperHasUserChanges(upper)
	if err != nil {
		t.Fatal(err)
	}
	if changed {
		t.Fatal("directory-only upperdir should not count as changed")
	}
	if err := os.WriteFile(filepath.Join(nested, "value.txt"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	changed, err = UpperHasUserChanges(upper)
	if err != nil {
		t.Fatal(err)
	}
	if !changed {
		t.Fatal("nested user file should count as changed")
	}
}

func TestValidateNameErrorMentionsAllowedCharacters(t *testing.T) {
	err := ValidateName("bad/name")
	if err == nil || !strings.Contains(err.Error(), "dot, underscore, and dash") {
		t.Fatalf("ValidateName error = %v", err)
	}
}
