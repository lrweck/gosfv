package gui

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"

	"github.com/egoist/mygo/ui"

	"github.com/lrweck/gosfv/internal/hasher"
	"github.com/lrweck/gosfv/internal/model"
	"github.com/lrweck/gosfv/internal/runner"
	"github.com/lrweck/gosfv/internal/settings"
)

func testApp() *App {
	s := settings.Defaults()
	s.ShowFilter = false
	s.ShowAdvanced = false
	a := &App{
		settings:     s,
		algo:         hasher.MD5,
		active:       map[int32]activeFile{},
		statusFilter: 0,
	}
	h := bytes.Repeat([]byte{0xab}, hasher.MD5.Size())
	a.store = []model.File{
		{ID: 1, Name: "a.bin", Rel: filepath.Join("sub", "a.bin"), Dir: `C:\data`, Status: model.OK, Hash: h, Expected: h},
		{ID: 2, Name: "b.bin", Rel: "b.bin", Dir: `C:\data`, Status: model.Bad, Hash: bytes.Repeat([]byte{0xef}, 16), Expected: bytes.Repeat([]byte{0xcd}, 16)},
		{ID: 3, Name: "c.bin", Rel: "c.bin", Dir: `C:\data`, Status: model.Missing},
	}
	a.viewRows = []int{0, 1, 2}
	a.stats = runner.Progress{Total: 3, Done: 3, OK: 1, Bad: 1, Missing: 1}
	a.refreshColumns()
	a.resetList()
	a.list.Key = func(row int) any { return a.store[a.viewRows[row]].ID }
	a.list.Label = func(row int) string { return a.store[a.viewRows[row]].Name }
	return a
}

func TestThemePalettes(t *testing.T) {
	if lightPalette.Dark || !darkPalette.Dark {
		t.Fatalf("palettes: light.Dark=%v dark.Dark=%v", lightPalette.Dark, darkPalette.Dark)
	}
	if lightPalette.Background == darkPalette.Background {
		t.Error("both palettes share a background")
	}
	if got := themeMode("Dark"); got != settings.ThemeDark {
		t.Errorf("themeMode(Dark) = %v", got)
	}
	if got := themeMode("Light"); got != settings.ThemeLight {
		t.Errorf("themeMode(Light) = %v", got)
	}
	if got := themeMode("System"); got != settings.ThemeSystem {
		t.Errorf("themeMode(System) = %v", got)
	}
}

func TestViewRendersInBothAppearances(t *testing.T) {
	for _, dark := range []bool{false, true} {
		tt := ui.NewTesterWith(testApp().view, 1200, 720, ui.TesterOptions{Dark: dark})
		for _, text := range []string{"Files", "Theme", "OK", "BAD", "MISSING", "Open..."} {
			if !tt.HasText(text) {
				t.Errorf("dark=%v: view does not show %q", dark, text)
			}
		}
	}
}

func TestViewShowsDetailsSidebar(t *testing.T) {
	a := testApp()
	a.selection.Add(a.store[0].ID)
	a.activeRow = 0
	tt := ui.NewTester(a.view, 1200, 720)
	for _, text := range []string{"Details", "a.bin", "Size", "Hash", "Expected"} {
		if !tt.HasText(text) {
			t.Errorf("details sidebar does not show %q", text)
		}
	}
}

func TestDetailsSidebarFollowsClick(t *testing.T) {
	a := testApp()
	tt := ui.NewTester(a.view, 1200, 720)
	if err := tt.Click("b.bin"); err != nil {
		t.Fatal(err)
	}
	if a.sel.Count != 1 || !a.sel.HasSingle || a.sel.Single.Name != "b.bin" {
		t.Fatalf("selection after click = %+v", a.sel)
	}
	if tt.HasText("Click a file to inspect it.") {
		t.Error("sidebar still shows the empty state after a click")
	}
}

func TestViewShowsFiles(t *testing.T) {
	a := testApp()
	tt := ui.NewTester(a.view, 1100, 700)
	for _, text := range []string{"a.bin", "b.bin", "c.bin", "OK", "BAD", "MISSING", "Open...", "Save As...", "Copy"} {
		if !tt.HasText(text) {
			t.Errorf("view does not show %q; texts: %v", text, tt.Texts())
		}
	}
	if !tt.HasText(filepath.Join("sub", "a.bin")) {
		t.Errorf("relative path not shown; texts: %v", tt.Texts())
	}
}

func TestViewSortAndFilter(t *testing.T) {
	a := testApp()
	a.sort = ui.SortOrder{Column: "Name", Descending: false}
	a.applyView()
	if got := a.viewRows; len(got) != 3 || a.store[got[0]].Name != "b.bin" || a.store[got[2]].Name != "a.bin" {
		t.Errorf("path sort = %v", got)
	}
	a.sort = ui.SortOrder{Column: "Name", Descending: true}
	a.applyView()
	if a.store[a.viewRows[0]].Name != "a.bin" {
		t.Errorf("descending sort = %v", a.viewRows)
	}
	a.filterText = "B.BIN"
	a.statusFilter = 4
	a.applyView()
	if len(a.viewRows) != 1 || a.store[a.viewRows[0]].Name != "b.bin" {
		t.Errorf("filter = %v", a.viewRows)
	}
	a.showDuplicates = true
	a.filterText = ""
	a.statusFilter = 0
	a.applyView()
	// Both a and b have a hash, but they differ, so there are no duplicates.
	if len(a.viewRows) != 0 {
		t.Errorf("duplicates = %v", a.viewRows)
	}
}

func TestRemoveIndices(t *testing.T) {
	a := testApp()
	a.removeIndices([]int{1})
	if len(a.store) != 2 || a.store[0].Name != "a.bin" || a.store[1].Name != "c.bin" {
		t.Fatalf("store after remove = %+v", a.store)
	}
	if len(a.viewRows) != 2 {
		t.Errorf("view after remove = %v", a.viewRows)
	}
}

func TestJobBaseAndSanitize(t *testing.T) {
	dir := t.TempDir()
	if got := sanitizeName(`a:b*c?.sfv`); got != "a_b_c_.sfv" {
		t.Errorf("sanitize = %q", got)
	}
	sub := filepath.Join(dir, "Folder")
	if err := os.MkdirAll(sub, 0o755); err != nil {
		t.Fatal(err)
	}
	base, name := jobBase([]string{sub})
	if base != filepath.Clean(dir) || name != "Folder" {
		t.Errorf("jobBase = %q, %q", base, name)
	}
	if got := createName([]string{sub}, dir); got != "Folder" {
		t.Errorf("createName = %q", got)
	}
}

func TestDedupPaths(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "a")
	if got := dedupPaths([]string{p, p, filepath.Join(dir, ".", "a")}); len(got) != 1 {
		t.Errorf("dedup = %v", got)
	}
}

func TestSelectedIndices(t *testing.T) {
	a := testApp()
	a.selection.Add(2)
	if got := a.selectedIndices(); len(got) != 1 || got[0] != 1 {
		t.Errorf("selected = %v", got)
	}
	a.selection.Clear()
	if got := a.selectedIndices(); len(got) != 0 {
		t.Errorf("empty selection = %v", got)
	}
}
