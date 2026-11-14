package gui

import (
	"image/png"
	"os"
	"path/filepath"
	"testing"

	"github.com/egoist/mygo/ui"

	"github.com/lrweck/gosfv/internal/model"
	"github.com/lrweck/gosfv/internal/runner"
)

func filesApp() *App {
	a := testApp()
	a.verifying = true
	a.refreshColumns()
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
	cases := []struct {
		name string
		app  func() *App
		dark bool
	}{
		{"gosfv-files-light.png", filesApp, false},
		{"gosfv-files-dark.png", filesApp, true},
		{"gosfv-empty-light.png", empty, false},
		{"gosfv-jobs-dark.png", jobs, true},
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
