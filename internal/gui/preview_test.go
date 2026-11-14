package gui

import (
	"image/png"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/egoist/mygo/ui"

	"github.com/lrweck/gosfv/internal/model"
	"github.com/lrweck/gosfv/internal/runner"
)

func filesApp() *App {
	a := testApp()
	a.verifying = true
	a.refreshColumns()
	// Show the details sidebar populated without starting a background job:
	// the tester does not run goroutines, and the stat fields would arrive
	// after the frame.
	a.store[0].Elapsed = 12 * time.Second
	a.selection.Add(a.store[0].ID)
	a.activeRow = 0
	a.selSig = selectionSig{Count: 1, Active: 0}
	a.sel = selectionInfo{Count: 1, Found: 1, HasSingle: true, Single: detailOf(&a.store[0])}
	a.sel.Single.Size = 18 << 30
	a.sel.Single.StatOK = true
	a.sel.Single.ModTime = time.Date(2026, 11, 14, 16, 43, 0, 0, time.Local)
	a.selDirty = false
	return a
}

// TestRenderPreview writes the palettes and the two content modes to PNG so
// a layout change can be reviewed without a display. It only runs when
// asked:
//
//	GOSFV_PREVIEW_DIR=%TEMP% go test ./internal/gui -run TestRenderPreview
func TestRenderPreview(t *testing.T) {
	dir := os.Getenv("GOSFV_PREVIEW_DIR")
	if dir == "" {
		t.Skip("set GOSFV_PREVIEW_DIR to write preview images")
	}
	empty := func() *App {
		a := testApp()
		a.store = nil
		a.viewRows = nil
		a.stats = runner.Progress{}
		return a
	}
	jobs := func() *App {
		a := testApp()
		a.modeJobs = true
		a.jobs = []model.Job{
			{ID: 1, Name: "Ubuntu", Root: `C:\Images\Ubuntu`, Progress: 100, Status: model.JobDone},
			{ID: 2, Name: "Debian", Root: `D:\Backups\Debian`, Progress: 42.5, Status: model.JobInProgress},
			{ID: 3, Name: "Archive", Root: `E:\Archive`, Status: model.JobQueued},
		}
		return a
	}
	running := func() *App {
		a := filesApp()
		a.processing = true
		a.stats = runner.Progress{Total: 10, Done: 4, OK: 3, Bad: 1, SpeedBPS: 800 << 20, ETA: 30 * time.Second}
		return a
	}
	cases := []struct {
		name string
		app  func() *App
		dark bool
	}{
		{"gosfv-files-light.png", filesApp, false},
		{"gosfv-files-dark.png", filesApp, true},
		{"gosfv-empty-light.png", empty, false},
		{"gosfv-jobs-dark.png", jobs, true},
		{"gosfv-running-light.png", running, false},
	}
	for _, tc := range cases {
		a := tc.app()
		tt := ui.NewTesterWith(a.view, 1200, 720, ui.TesterOptions{Dark: tc.dark})
		f, err := os.Create(filepath.Join(dir, tc.name))
		if err != nil {
			t.Fatal(err)
		}
		if err := png.Encode(f, tt.Image()); err != nil {
			f.Close()
			t.Fatal(err)
		}
		if err := f.Close(); err != nil {
			t.Fatal(err)
		}
	}
}
