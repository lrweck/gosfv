package settings

import (
	"os"
	"path/filepath"
	"testing"

	"gosfv/internal/drive"
	"gosfv/internal/hasher"
)

func TestRoundTrip(t *testing.T) {
	path := filepath.Join(t.TempDir(), "GoSFV.ini")
	s := Defaults()
	s.ShowTime = true
	s.ShowThroughput = true
	s.ShowFilter = true
	s.ShowAdvanced = true
	s.ShowFullPaths = true
	s.ShowHashCol = false
	s.ShowExpectedCol = false
	s.LockColumns = false
	s.PathMode = Absolute
	s.ProcMode = drive.HDD
	s.Comments = true
	s.PathPrefix = `incoming/iso`
	s.IncludePattern = "*.iso;*.mkv"
	s.ExcludePattern = "*.nfo"
	s.Recursive = false
	s.Signature = "TEST"
	s.Theme = ThemeDark
	s.DefaultAlgo = hasher.SHA256
	s.ColumnOrder = []string{"Status", "Name", "Hash"}
	s.ColumnWidths = map[string]float32{"Name": 321.5, "Hash": 200}
	if err := s.Save(path); err != nil {
		t.Fatal(err)
	}
	got := Load(path)
	if got.ShowTime != s.ShowTime || got.ShowThroughput != s.ShowThroughput ||
		got.ShowFilter != s.ShowFilter || got.ShowAdvanced != s.ShowAdvanced ||
		got.ShowFullPaths != s.ShowFullPaths || got.ShowHashCol != s.ShowHashCol ||
		got.ShowExpectedCol != s.ShowExpectedCol || got.LockColumns != s.LockColumns ||
		got.PathMode != s.PathMode || got.ProcMode != s.ProcMode ||
		got.Comments != s.Comments || got.PathPrefix != s.PathPrefix ||
		got.IncludePattern != s.IncludePattern || got.ExcludePattern != s.ExcludePattern ||
		got.Recursive != s.Recursive || got.Signature != s.Signature ||
		got.Theme != s.Theme || got.DefaultAlgo != s.DefaultAlgo {
		t.Fatalf("settings differ:\n got %+v\nwant %+v", got, s)
	}
	if len(got.ColumnOrder) != 3 || got.ColumnOrder[0] != "Status" {
		t.Errorf("column order = %v", got.ColumnOrder)
	}
	if got.ColumnWidths["Name"] != 321.5 {
		t.Errorf("name width = %v", got.ColumnWidths["Name"])
	}
}

func TestLegacyKeys(t *testing.T) {
	path := filepath.Join(t.TempDir(), "SharpSFV.ini")
	data := "[SharpSFV]\nUseAbsolutePaths=1\nOptimizeForHDD=1\nDefaultAlgo=Crc32\nScanRecursive=0\n"
	if err := osWriteFile(path, data); err != nil {
		t.Fatal(err)
	}
	s := Load(path)
	if s.PathMode != Absolute || s.ProcMode != drive.HDD || s.DefaultAlgo != hasher.CRC32 || s.Recursive {
		t.Errorf("legacy parse = %+v", s)
	}
}

func TestThemeKeys(t *testing.T) {
	path := filepath.Join(t.TempDir(), "GoSFV.ini")
	if err := osWriteFile(path, "Theme=dark\n"); err != nil {
		t.Fatal(err)
	}
	if got := Load(path).Theme; got != ThemeDark {
		t.Errorf("Theme=dark -> %v", got)
	}
	if err := osWriteFile(path, "Theme=Light\n"); err != nil {
		t.Fatal(err)
	}
	if got := Load(path).Theme; got != ThemeLight {
		t.Errorf("Theme=Light -> %v", got)
	}
	// SharpSFV had no theme key; a boolean dark mode is still understood.
	if err := osWriteFile(path, "DarkMode=1\n"); err != nil {
		t.Fatal(err)
	}
	if got := Load(path).Theme; got != ThemeDark {
		t.Errorf("DarkMode=1 -> %v", got)
	}
	if err := osWriteFile(path, "Theme=sepia\n"); err != nil {
		t.Fatal(err)
	}
	if got := Load(path).Theme; got != ThemeSystem {
		t.Errorf("unknown theme -> %v, want system", got)
	}
}

func TestMalformedAndMissing(t *testing.T) {
	s := Load(filepath.Join(t.TempDir(), "missing.ini"))
	if s.DefaultAlgo != hasher.XXH3 || !s.Recursive || s.Signature != "L33T" {
		t.Errorf("defaults = %+v", s)
	}
	path := filepath.Join(t.TempDir(), "bad.ini")
	if err := osWriteFile(path, "garbage\n[section\nShowHashCol=0\nDefaultAlgo=nope\n"); err != nil {
		t.Fatal(err)
	}
	s = Load(path)
	if s.ShowHashCol {
		t.Error("ShowHashCol not read")
	}
	if s.DefaultAlgo != hasher.XXH3 {
		t.Errorf("invalid algo changed default: %v", s.DefaultAlgo)
	}
}

func osWriteFile(path, data string) error {
	return os.WriteFile(path, []byte(data), 0o644)
}
