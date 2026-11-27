package sqlitestore

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestOpenCreatesTheFolder(t *testing.T) {
	nested := filepath.Join(t.TempDir(), "data", "nested")
	store, err := Open(filepath.Join(nested, "alceacoord.db"))
	if err != nil {
		t.Fatalf("Open must create the folder: %v", err)
	}
	defer store.Close()

	if _, err := os.Stat(filepath.Join(nested, "alceacoord.db")); err != nil {
		t.Errorf("the database must exist: %v", err)
	}
}

func TestOpenRejectsAnEmptyPath(t *testing.T) {
	if _, err := Open("  "); err == nil {
		t.Error("an empty path must be rejected")
	}
}

func TestOpenReportsAnUnusableFolder(t *testing.T) {
	blocker := filepath.Join(t.TempDir(), "file")
	if err := os.WriteFile(blocker, []byte("x"), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
	_, err := Open(filepath.Join(blocker, "alceacoord.db"))
	if err == nil {
		t.Fatal("a file in place of the folder must be reported")
	}
	if !strings.Contains(err.Error(), "cannot create the database folder") {
		t.Errorf("err = %v", err)
	}
}
