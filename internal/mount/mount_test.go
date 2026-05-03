package mount

import (
	"os"
	"path/filepath"
	"testing"
)

func TestEnsureEmptyMountpoint(t *testing.T) {
	root := t.TempDir()
	missing := filepath.Join(root, "missing")
	if err := EnsureEmptyMountpoint(missing); err != nil {
		t.Fatal(err)
	}
	if info, err := os.Stat(missing); err != nil || !info.IsDir() {
		t.Fatalf("mountpoint not created as dir: info=%v err=%v", info, err)
	}

	if err := EnsureEmptyMountpoint(missing); err != nil {
		t.Fatalf("empty mountpoint rejected: %v", err)
	}

	if err := os.WriteFile(filepath.Join(missing, "file"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := EnsureEmptyMountpoint(missing); err == nil {
		t.Fatal("non-empty mountpoint accepted")
	}
}

func TestUnescapeMountInfo(t *testing.T) {
	got := unescapeMountInfo(`/tmp/has\040space/has\011tab/has\012newline/has\134slash`)
	want := "/tmp/has space/has\ttab/has\nnewline/has\\slash"
	if got != want {
		t.Fatalf("unescapeMountInfo() = %q, want %q", got, want)
	}
}
