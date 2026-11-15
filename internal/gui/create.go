package gui

import (
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/egoist/mygo"

	"github.com/lrweck/gosfv/internal/hasher"
	"github.com/lrweck/gosfv/internal/scanner"
)

// scheduleCreate coalesces the paths Explorer sends when several files are
// selected. Each invocation of GoSFV hands its path to the first instance, and
// the first instance waits for the burst to end before asking where to save.
func (a *App) scheduleCreate() {
	if a.createTimer != nil {
		a.createTimer.Stop()
	}
	a.createTimer = time.AfterFunc(250*time.Millisecond, func() {
		a.onMain(a.launchCreate)
	})
}

func (a *App) launchCreate() {
	if a.closing.Load() || a.createLaunch {
		return
	}
	if len(a.collected) == 0 {
		mygo.App.Quit()
		return
	}
	a.createLaunch = true
	paths := append([]string(nil), a.collected...)
	base := scanner.CommonBase(paths)
	out := a.autoSave
	algo := a.algo
	if out != "" {
		a.beginCreate(paths, base, out)
		return
	}
	if a.win != nil {
		a.win.Show()
		a.win.Center()
	}
	go func() {
		def := filepath.Join(base, createName(paths, base)+algo.Ext())
		path, err := mygo.Dialog.Save(mygo.SaveDialogOptions{
			Parent:      a.win,
			Title:       "GoSFV - Create Checksum",
			DefaultPath: def,
			Filters: []mygo.FileFilter{
				{Name: "SFV (CRC-32)", Extensions: []string{"sfv"}},
				{Name: "MD5", Extensions: []string{"md5"}},
				{Name: "SHA-1", Extensions: []string{"sha1"}},
				{Name: "SHA-256", Extensions: []string{"sha256"}},
				{Name: "xxHash3", Extensions: []string{"xxh3"}},
			},
		})
		if err != nil || path == "" {
			mygo.App.Quit()
			return
		}
		a.onMain(func() { a.beginCreate(paths, base, path) })
	}()
}

func (a *App) beginCreate(paths []string, base, output string) {
	if a.closing.Load() {
		return
	}
	if ext := filepath.Ext(output); ext == "" {
		output += a.algo.Ext()
	} else if k, ok := hasher.FromExtension(ext); ok && !a.algoSet {
		a.algo = k
	}
	a.autoSave = output
	a.collected = nil
	a.createMode = true
	a.verifying = false
	a.settings.DefaultAlgo = a.algo
	_ = a.settings.Save(a.settingsPath)
	if a.win != nil {
		a.win.SetTitle("GoSFV - Creating...")
		a.win.Show()
		a.win.Center()
	}
	a.startRun(paths, base, false, "")
}

func createName(paths []string, base string) string {
	if len(paths) == 1 {
		p := filepath.Clean(paths[0])
		if info, err := os.Stat(p); err == nil && info.IsDir() {
			return filepath.Base(p)
		}
		name := filepath.Base(p)
		return strings.TrimSuffix(name, filepath.Ext(name))
	}
	name := filepath.Base(base)
	if name == "." || name == string(filepath.Separator) || name == "" {
		return "checksums"
	}
	return name
}
