package badtools

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestMoveAndRename(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "a.bin")
	if err := os.WriteFile(p, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	moved, err := Move(p)
	if err != nil {
		t.Fatal(err)
	}
	if filepath.Dir(moved) != filepath.Join(dir, BadDir) {
		t.Errorf("moved to %q", moved)
	}
	if _, err := os.Stat(p); !os.IsNotExist(err) {
		t.Error("original still exists")
	}

	p2 := filepath.Join(dir, "b.bin")
	if err := os.WriteFile(p2, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	renamed, err := Rename(p2)
	if err != nil {
		t.Fatal(err)
	}
	if renamed != p2+".CORRUPT" {
		t.Errorf("renamed to %q", renamed)
	}
	if _, err := os.Stat(renamed); err != nil {
		t.Error(err)
	}
}

func TestMoveCollision(t *testing.T) {
	dir := t.TempDir()
	for i := 0; i < 2; i++ {
		p := filepath.Join(dir, "same.bin")
		if err := os.WriteFile(p, []byte("x"), 0o644); err != nil {
			t.Fatal(err)
		}
		if _, err := Move(p); err != nil {
			t.Fatal(err)
		}
	}
	entries, err := os.ReadDir(filepath.Join(dir, BadDir))
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 2 {
		t.Fatalf("got %d entries", len(entries))
	}
}

func TestScripts(t *testing.T) {
	dir := t.TempDir()
	deletePath := filepath.Join(dir, "delete.bat")
	if err := WriteDeleteScript(deletePath, []string{`C:\a b\x.bin`}); err != nil {
		t.Fatal(err)
	}
	data, _ := os.ReadFile(deletePath)
	if !strings.Contains(string(data), `del "C:\a b\x.bin"`) {
		t.Errorf("script = %q", data)
	}
	groups := [][]string{{"a", "b", "c"}}
	copyPath := filepath.Join(dir, "copy.bat")
	if err := WriteCopyDuplicatesScript(copyPath, groups); err != nil {
		t.Fatal(err)
	}
	data, _ = os.ReadFile(copyPath)
	if !strings.Contains(string(data), `copy /y "a" "b"`) {
		t.Errorf("copy script = %q", data)
	}
	delDupPath := filepath.Join(dir, "deldup.bat")
	if err := WriteDeleteDuplicatesScript(delDupPath, groups); err != nil {
		t.Fatal(err)
	}
	data, _ = os.ReadFile(delDupPath)
	if !strings.Contains(string(data), `del "c"`) {
		t.Errorf("delete duplicates script = %q", data)
	}
}

func TestNewRelativePath(t *testing.T) {
	got := NewRelativePath(filepath.Join("sub", "a.bin"))
	want := filepath.Join("sub", BadDir, "a.bin")
	if got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}
