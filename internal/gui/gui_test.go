package gui

import (
	"bytes"
	"crypto/md5"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

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

func TestFadeStep(t *testing.T) {
	if got := fadeStep(0); got != 0 {
		t.Errorf("start = %v", got)
	}
	if got := fadeStep(themeFade); got != 1 {
		t.Errorf("end = %v", got)
	}
	mid := fadeStep(themeFade / 2)
	if mid < 0.49 || mid > 0.51 {
		t.Errorf("middle = %v", mid)
	}
	if fadeStep(themeFade/4) >= mid {
		t.Error("fade is not monotonic")
	}
}

func TestBlendColor(t *testing.T) {
	got := blendColor(ui.RGB(0, 0, 0), ui.RGB(200, 100, 50), 0.5)
	if got.R != 100 || got.G != 50 || got.B != 25 || got.A != 255 {
		t.Errorf("blend = %+v", got)
	}
}

func TestBlendThemesKeepsTargetAppearance(t *testing.T) {
	mid := blendThemes(lightPalette, darkPalette, 0.5)
	if mid.Dark != darkPalette.Dark {
		t.Error("blended theme lost the target appearance")
	}
	if mid.Background == lightPalette.Background || mid.Background == darkPalette.Background {
		t.Error("background did not blend")
	}
	if mid.Accent == lightPalette.Accent || mid.Accent == darkPalette.Accent {
		t.Error("accent did not blend")
	}
	if mid.Radius != darkPalette.Radius || mid.Spacing != darkPalette.Spacing {
		t.Error("metrics should come from the target")
	}
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

func TestLiveSortByProgress(t *testing.T) {
	a := testApp()
	a.sort = ui.SortOrder{Column: "Progress"}
	a.applyView()
	for row, idx := range a.viewRows {
		if a.store[idx].Name == "a.bin" {
			a.activeRow = row
		}
	}
	a.mu.Lock()
	a.active[0] = activeFile{Name: "a.bin", Done: 25, Total: 100}
	a.mu.Unlock()
	a.liveSort(0)
	// Ascending by fraction: c (no value), a (25%), b (finished).
	if got := []string{a.store[a.viewRows[0]].Name, a.store[a.viewRows[1]].Name, a.store[a.viewRows[2]].Name}; got[0] != "c.bin" || got[1] != "a.bin" || got[2] != "b.bin" {
		t.Errorf("first live sort = %v", got)
	}
	if a.activeRow != 1 {
		t.Errorf("active row = %d, want 1", a.activeRow)
	}

	// c.bin moves ahead of a.bin as its own progress rises.
	a.mu.Lock()
	a.active[2] = activeFile{Name: "c.bin", Done: 50, Total: 100}
	a.mu.Unlock()
	a.liveSort(0)
	if got := []string{a.store[a.viewRows[0]].Name, a.store[a.viewRows[1]].Name, a.store[a.viewRows[2]].Name}; got[0] != "a.bin" || got[1] != "c.bin" || got[2] != "b.bin" {
		t.Errorf("second live sort = %v", got)
	}
	if a.activeRow != 0 {
		t.Errorf("active row = %d, want 0", a.activeRow)
	}
}

func TestSortByProgress(t *testing.T) {
	a := testApp()
	a.mu.Lock()
	a.active[0] = activeFile{Name: "a.bin", Done: 25, Total: 100}
	a.active[2] = activeFile{Name: "c.bin", Done: 50, Total: 100}
	a.mu.Unlock()

	a.sort = ui.SortOrder{Column: "Progress"}
	a.applyView()
	got := []string{a.store[a.viewRows[0]].Name, a.store[a.viewRows[1]].Name, a.store[a.viewRows[2]].Name}
	// a 25%, c 50%, b finished.
	if got[0] != "a.bin" || got[1] != "c.bin" || got[2] != "b.bin" {
		t.Errorf("ascending = %v", got)
	}
	a.sort.Descending = true
	a.applyView()
	got = []string{a.store[a.viewRows[0]].Name, a.store[a.viewRows[1]].Name, a.store[a.viewRows[2]].Name}
	if got[0] != "b.bin" || got[1] != "c.bin" || got[2] != "a.bin" {
		t.Errorf("descending = %v", got)
	}
}

func TestSortByStatusPutsProcessingFirst(t *testing.T) {
	a := testApp() // a OK, b BAD, c MISSING
	a.mu.Lock()
	a.active[0] = activeFile{Name: "a.bin", Done: 25, Total: 100}
	a.mu.Unlock()
	a.sort = ui.SortOrder{Column: "Status"}
	a.applyView()
	got := []string{a.store[a.viewRows[0]].Name, a.store[a.viewRows[1]].Name, a.store[a.viewRows[2]].Name}
	if got[0] != "a.bin" || got[1] != "b.bin" || got[2] != "c.bin" {
		t.Errorf("ascending = %v", got)
	}
	a.sort.Descending = true
	a.applyView()
	got = []string{a.store[a.viewRows[0]].Name, a.store[a.viewRows[1]].Name, a.store[a.viewRows[2]].Name}
	if got[0] != "c.bin" || got[1] != "b.bin" || got[2] != "a.bin" {
		t.Errorf("descending = %v", got)
	}
}

func TestProgressFractionUsesBytes(t *testing.T) {
	a := &App{stats: runner.Progress{Total: 10, Done: 5, BytesTotal: 1000, BytesDone: 800}}
	if got := a.progressFraction(); got < 0.79 || got > 0.81 {
		t.Errorf("byte fraction = %v, want 0.8", got)
	}
	// Once every file is applied, the count wins even if a task read less
	// than its size before failing.
	a.stats.Done = 10
	if got := a.progressFraction(); got != 1 {
		t.Errorf("finished fraction = %v, want 1", got)
	}
	a.stats = runner.Progress{Total: 10, Done: 5}
	if got := a.progressFraction(); got != 0.5 {
		t.Errorf("count fraction = %v, want 0.5", got)
	}
}

func TestSpeedTextPaused(t *testing.T) {
	a := &App{
		processing: true,
		paused:     true,
		stats:      runner.Progress{SpeedBPS: 100 << 20, ETA: 5 * time.Second},
	}
	got := a.speedText()
	if !strings.Contains(got, "0.0 MB/s") {
		t.Errorf("paused speed = %q", got)
	}
	if !strings.Contains(got, "ETA: 00:05") {
		t.Errorf("paused eta = %q", got)
	}
}

func TestFooterState(t *testing.T) {
	a := &App{}
	if got := a.footerState(); got != "Ready" {
		t.Errorf("idle = %q", got)
	}
	a.processing, a.verifying = true, true
	if got := a.footerState(); got != "Processing" {
		t.Errorf("verifying = %q", got)
	}
	a.paused = true
	if got := a.footerState(); got != "Paused" {
		t.Errorf("paused = %q", got)
	}
	a.paused, a.rechecking = false, true
	if got := a.footerState(); got != "Rechecking" {
		t.Errorf("recheck = %q", got)
	}
	a.processing, a.verifying, a.rechecking = false, false, false
	a.stats = runner.Progress{Total: 2, Done: 2}
	if got := a.footerState(); got != "Done" {
		t.Errorf("done = %q", got)
	}
}

func TestRecheckButtonIncludesErrorRows(t *testing.T) {
	a := filesApp()
	a.selection.Clear()
	a.store[1].Status = model.Error
	a.store[1].Hash = nil
	tt := ui.NewTester(a.view, 1200, 720)
	if err := tt.Click("Recheck All"); err != nil {
		t.Fatal(err)
	}
	if !a.processing || !a.rechecking {
		t.Fatalf("recheck did not start: processing=%v rechecking=%v", a.processing, a.rechecking)
	}
	if a.store[1].Status != model.Pending {
		t.Errorf("ERROR row was not queued for recheck: %v", a.store[1].Status)
	}
}

func TestFooterStaysVisibleWhenMaximized(t *testing.T) {
	a := filesApp()
	a.processing = true
	a.stats = runner.Progress{Total: 3, Done: 1, OK: 1}
	a.mu.Lock()
	a.active[0] = activeFile{Name: "a.bin", Done: 9 << 30, Total: 18 << 30}
	a.mu.Unlock()
	a.settings.ShowFilter = true
	a.settings.ShowAdvanced = true
	a.activeView = []activeFile{
		{Name: "a.bin", Done: 9 << 30, Total: 18 << 30},
		{Name: "b.bin", Done: 24 << 20, Total: 48 << 20},
		{Name: "c.bin", Done: 0, Total: 4 << 30},
	}
	tt := ui.NewTester(a.view, 1920, 1080)
	r, ok := tt.Find("33.3%")
	if !ok {
		t.Fatalf("footer percentage not found; texts: %v", tt.Texts())
	}
	if r.Y+r.H > 1080 {
		t.Errorf("footer is below the window: %+v", r)
	}
	if r.Y < 900 {
		t.Errorf("footer is not at the bottom: %+v", r)
	}
}

func TestFailedIndices(t *testing.T) {
	a := testApp() // a OK, b BAD, c MISSING
	a.store = append(a.store, model.File{ID: 4, Name: "d.bin", Status: model.Error})
	a.store = append(a.store, model.File{ID: 5, Name: "e.bin", Status: model.Pending})
	got := a.failedIndices()
	if len(got) != 3 || got[0] != 1 || got[1] != 2 || got[2] != 3 {
		t.Errorf("failed = %v", got)
	}
}

func TestPauseAndStopStayOutOfTheRecheckSlots(t *testing.T) {
	a := filesApp()
	idle := ui.NewTester(a.view, 1200, 720)
	if _, ok := idle.Find("Recheck Failed"); !ok {
		t.Fatal("idle: Recheck Failed is missing from the status bar")
	}
	if _, ok := idle.Find("Stop"); ok {
		t.Fatal("idle: Stop should only appear while a run is in progress")
	}

	a.processing = true
	a.active[0] = activeFile{Name: "a.bin", Done: 10, Total: 100}
	running := ui.NewTester(a.view, 1200, 720)
	recheck, ok := running.Find("Recheck Failed")
	if !ok {
		t.Fatal("running: Recheck Failed left the status bar")
	}
	pause, ok := running.Find("Pause")
	if !ok {
		t.Fatal("running: Pause is missing from the footer")
	}
	stop, ok := running.Find("Stop")
	if !ok {
		t.Fatal("running: Stop is missing from the footer")
	}
	// A second click where Recheck Failed was must not reach Pause or Stop.
	if recheck.Y+recheck.H > pause.Y || recheck.Y+recheck.H > stop.Y {
		t.Errorf("Pause/Stop share the Recheck row: recheck=%+v pause=%+v stop=%+v", recheck, pause, stop)
	}
}

func TestOnMainUsesTheUICallback(t *testing.T) {
	a := testApp()
	old := postToMain
	t.Cleanup(func() { postToMain = old })
	postToMain = func(fn func()) { fn() }
	ran := false
	// Production leaves runMain nil; a self call here would recurse until
	// the stack overflows.
	a.onMain(func() { ran = true })
	if !ran {
		t.Error("onMain did not run the callback")
	}
}

func TestStartRecheckHashesAnErrorRow(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "a.bin")
	data := []byte("alpha")
	if err := os.WriteFile(p, data, 0o644); err != nil {
		t.Fatal(err)
	}
	sum := md5.Sum(data)

	a := testApp()
	a.runMain = func(fn func()) { fn() } // the runner goroutine drives the UI state
	a.verifying = true
	a.algo = hasher.MD5
	a.store = []model.File{{
		ID: 1, Name: "a.bin", Rel: "a.bin", Dir: dir, Full: p,
		Size: int64(len(data)), Expected: sum[:], Status: model.Error, Err: "context canceled",
	}}
	a.viewRows = []int{0}
	a.stats = runner.Progress{Total: 1, Done: 1, Error: 1}
	a.refreshColumns()

	a.startRecheck([]int{0})
	deadline := time.Now().Add(5 * time.Second)
	for a.processing && time.Now().Before(deadline) {
		time.Sleep(time.Millisecond)
	}
	if a.processing {
		t.Fatal("recheck did not finish")
	}
	if a.store[0].Status != model.OK {
		t.Errorf("status = %v, error = %q", a.store[0].Status, a.store[0].Err)
	}
	if a.store[0].Err != "" {
		t.Errorf("stale error kept: %q", a.store[0].Err)
	}
	if !bytes.Equal(a.store[0].Hash, sum[:]) {
		t.Errorf("hash = %x, want %x", a.store[0].Hash, sum)
	}
}

func TestUnfinishedIndices(t *testing.T) {
	a := testApp() // a OK, b BAD, c MISSING
	a.store = append(a.store,
		model.File{ID: 4, Status: model.Queued},
		model.File{ID: 5, Status: model.Pending},
	)
	got := a.unfinishedIndices()
	if len(got) != 2 || got[0] != 3 || got[1] != 4 {
		t.Errorf("unfinished = %v", got)
	}
}

func TestRecheckInACreationList(t *testing.T) {
	dir := t.TempDir()
	data := []byte("charlie")
	p := filepath.Join(dir, "c.bin")
	if err := os.WriteFile(p, data, 0o644); err != nil {
		t.Fatal(err)
	}
	sum := md5.Sum(data)

	a := testApp()
	a.runMain = func(fn func()) { fn() }
	a.verifying = false // a list built by creating hashes has no Expected
	a.algo = hasher.MD5
	a.store = []model.File{{
		ID: 1, Name: "c.bin", Rel: "c.bin", Dir: dir, Full: p,
		Size: int64(len(data)), Status: model.Queued,
	}}
	a.viewRows = []int{0}
	a.stats = runner.Progress{Total: 1}
	a.refreshColumns()

	a.startRecheck([]int{0})
	deadline := time.Now().Add(5 * time.Second)
	for a.processing && time.Now().Before(deadline) {
		time.Sleep(time.Millisecond)
	}
	if a.processing {
		t.Fatal("recheck did not finish")
	}
	if a.store[0].Status != model.OK || !bytes.Equal(a.store[0].Hash, sum[:]) {
		t.Errorf("rechecked row = %v hash=%x", a.store[0].Status, a.store[0].Hash)
	}
}

func TestRecheckButtonOnACreatedList(t *testing.T) {
	a := testApp()
	a.verifying = false
	tt := ui.NewTester(a.view, 1200, 720)
	if _, ok := tt.Find("Recheck All"); !ok {
		t.Fatal("Recheck All is missing from a created list")
	}
}

func TestStopThenRecheckFromTheButtons(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "big.bin")
	f, err := os.Create(p)
	if err != nil {
		t.Fatal(err)
	}
	if err := f.Truncate(512 << 20); err != nil {
		t.Fatal(err)
	}
	if err := f.Close(); err != nil {
		t.Fatal(err)
	}
	sum := strings.Repeat("0", 32)
	checks := filepath.Join(dir, "checks.md5")
	if err := os.WriteFile(checks, []byte(sum+"  big.bin\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	a := testApp()
	a.runMain = func(fn func()) { fn() }
	a.startRun([]string{checks}, dir, true, checks)

	deadline := time.Now().Add(10 * time.Second)
	for a.processing && len(a.store) == 0 && time.Now().Before(deadline) {
		time.Sleep(time.Millisecond)
	}
	if len(a.store) != 1 || !a.processing {
		t.Fatalf("verification did not start: processing=%v rows=%d", a.processing, len(a.store))
	}

	stop := ui.NewTester(a.view, 1200, 720)
	if err := stop.Click("Stop"); err != nil {
		t.Fatalf("Stop button: %v", err)
	}
	for a.processing && time.Now().Before(deadline) {
		time.Sleep(time.Millisecond)
	}
	if a.processing {
		t.Fatal("Stop did not end the run")
	}
	if a.store[0].Status == model.Error {
		t.Errorf("a stopped row is an error: %v %q", a.store[0].Status, a.store[0].Err)
	}

	recheck := ui.NewTester(a.view, 1200, 720)
	if err := recheck.Click("Recheck All"); err != nil {
		t.Fatalf("Recheck All button: %v", err)
	}
	if !a.processing {
		t.Fatal("Recheck All did not start a run")
	}
	deadline = time.Now().Add(20 * time.Second)
	for a.processing && time.Now().Before(deadline) {
		time.Sleep(time.Millisecond)
	}
	if a.processing {
		t.Fatal("recheck did not finish")
	}
	if a.store[0].Status != model.Bad || a.store[0].Err != "" || a.store[0].Hash == nil {
		t.Errorf("rechecked row = %v %q hash=%d", a.store[0].Status, a.store[0].Err, len(a.store[0].Hash))
	}
}

func TestStopThenRecheckACreatedList(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "big.bin")
	f, err := os.Create(p)
	if err != nil {
		t.Fatal(err)
	}
	if err := f.Truncate(512 << 20); err != nil {
		t.Fatal(err)
	}
	if err := f.Close(); err != nil {
		t.Fatal(err)
	}

	a := testApp()
	a.runMain = func(fn func()) { fn() }
	a.startRun([]string{p}, dir, false, "")

	deadline := time.Now().Add(10 * time.Second)
	for a.processing && len(a.store) == 0 && time.Now().Before(deadline) {
		time.Sleep(time.Millisecond)
	}
	if len(a.store) != 1 || !a.processing {
		t.Fatalf("creation did not start: processing=%v rows=%d verifying=%v", a.processing, len(a.store), a.verifying)
	}

	stop := ui.NewTester(a.view, 1200, 720)
	if err := stop.Click("Stop"); err != nil {
		t.Fatalf("Stop button: %v", err)
	}
	for a.processing && time.Now().Before(deadline) {
		time.Sleep(time.Millisecond)
	}
	if a.processing {
		t.Fatal("Stop did not end the creation")
	}
	if a.store[0].Status == model.Error {
		t.Errorf("a stopped row is an error: %v %q", a.store[0].Status, a.store[0].Err)
	}

	recheck := ui.NewTester(a.view, 1200, 720)
	if err := recheck.Click("Recheck All"); err != nil {
		t.Fatalf("Recheck All button on a created list: %v", err)
	}
	if !a.processing {
		t.Fatal("Recheck All did not start a run on a created list")
	}
	deadline = time.Now().Add(30 * time.Second)
	for a.processing && time.Now().Before(deadline) {
		time.Sleep(time.Millisecond)
	}
	if a.processing {
		t.Fatal("recheck did not finish")
	}
	if a.store[0].Status != model.OK || len(a.store[0].Hash) != md5.Size {
		t.Errorf("rechecked row = %v hash=%d", a.store[0].Status, len(a.store[0].Hash))
	}
}

func TestApplyTickKeepsErrorReason(t *testing.T) {
	a := testApp()
	a.applyTick(runner.Progress{Total: 3}, []runner.Completed{{
		Index: 1, Status: model.Error, Err: errors.New("access denied"),
	}})
	if a.store[1].Status != model.Error || a.store[1].Err != "access denied" {
		t.Fatalf("row = %+v", a.store[1])
	}
	if got := detailOf(&a.store[1]).Err; got != "access denied" {
		t.Errorf("details error = %q", got)
	}
	a.applyTick(runner.Progress{Total: 3}, []runner.Completed{{
		Index: 1, Status: model.OK, Hash: a.store[1].Expected,
	}})
	if a.store[1].Err != "" {
		t.Errorf("stale error kept after a success: %q", a.store[1].Err)
	}
}

func TestVerifySucceeded(t *testing.T) {
	cases := []struct {
		name string
		p    runner.Progress
		want bool
	}{
		{"all ok", runner.Progress{Total: 3, Done: 3, OK: 3}, true},
		{"bad", runner.Progress{Total: 3, Done: 3, OK: 2, Bad: 1}, false},
		{"missing", runner.Progress{Total: 3, Done: 3, OK: 2, Missing: 1}, false},
		{"error", runner.Progress{Total: 3, Done: 3, OK: 2, Error: 1}, false},
		{"unfinished", runner.Progress{Total: 3, Done: 2, OK: 2}, false},
		{"empty", runner.Progress{}, false},
	}
	for _, tc := range cases {
		if got := verifySucceeded(&runner.Report{Progress: tc.p}); got != tc.want {
			t.Errorf("%s: got %v, want %v", tc.name, got, tc.want)
		}
	}
	if verifySucceeded(nil) {
		t.Error("nil report counted as success")
	}
}

func TestViewShowsSizeAndProgress(t *testing.T) {
	a := testApp()
	a.store[0].Size = 1536
	a.store[1].Size = 1 << 20
	a.mu.Lock()
	a.active[0] = activeFile{Name: "a.bin", Done: 50, Total: 100}
	a.mu.Unlock()
	tt := ui.NewTester(a.view, 1400, 720)
	for _, text := range []string{"1.5 KiB", "1.0 MiB", "50.0%", "100.0%", "PROCESSING"} {
		if !tt.HasText(text) {
			t.Errorf("table does not show %q; texts: %v", text, tt.Texts())
		}
	}
}

func TestUpdateNativeProgressWithoutWindow(t *testing.T) {
	a := &App{}
	a.updateNativeProgress() // must not panic without a window
}

func TestSelectionLoadingDelay(t *testing.T) {
	a := &App{
		sel:   selectionInfo{Loading: true, Count: 1, HasSingle: true},
		selAt: time.Now(),
	}
	if a.showSelectionLoading(a.selAt.Add(10 * time.Millisecond)) {
		t.Error("single-file stat flashed before the delay")
	}
	if !a.showSelectionLoading(a.selAt.Add(selectionLoadingDelay)) {
		t.Error("single-file stat never showed the indicator")
	}
	a.sel.Count = 2
	if !a.showSelectionLoading(a.selAt.Add(time.Millisecond)) {
		t.Error("multi-selection did not show the indicator at once")
	}
}

func TestPauseAndStopButtons(t *testing.T) {
	a := testApp()
	a.processing = true
	tt := ui.NewTester(a.view, 1200, 720)
	if !tt.HasText("Pause") || !tt.HasText("Stop") {
		t.Fatalf("run controls missing; texts: %v", tt.Texts())
	}
	a.paused = true
	tt.Frame()
	if !tt.HasText("Resume") {
		t.Error("paused run does not offer Resume")
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
	if err := tt.Click("c.bin"); err != nil {
		t.Fatal(err)
	}
	if a.sel.Single.Name != "c.bin" {
		t.Errorf("sidebar did not follow the second click: %q", a.sel.Single.Name)
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

func TestClickOutsideDeselects(t *testing.T) {
	a := testApp()
	tt := ui.NewTester(a.view, 1200, 720)
	if err := tt.Click("a.bin"); err != nil {
		t.Fatal(err)
	}
	if a.selection.Len() != 1 {
		t.Fatalf("selection after click = %d", a.selection.Len())
	}
	tt.Advance(2 * time.Second) // past the double-click interval
	if err := tt.Click("a.bin"); err != nil {
		t.Fatal(err)
	}
	if a.selection.Len() != 1 {
		t.Fatalf("clicking the chosen row cleared it: %d", a.selection.Len())
	}
	tt.ClickAt(800, 400)
	if a.selection.Len() != 0 {
		t.Errorf("selection after empty click = %d", a.selection.Len())
	}
}
