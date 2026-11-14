package gui

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"

	"github.com/lrweck/gosfv/internal/badtools"
	"github.com/lrweck/gosfv/internal/checksum"
	"github.com/lrweck/gosfv/internal/drive"
	"github.com/lrweck/gosfv/internal/hexfmt"
	"github.com/lrweck/gosfv/internal/model"
	"github.com/lrweck/gosfv/internal/settings"
	"github.com/lrweck/gosfv/internal/shell"
)

func (a *App) openDialog() {
	if a.modeJobs {
		a.setNotice("Open is for the standard file list; drop folders in job mode.")
		return
	}
	if a.processing {
		a.setNotice("Busy: wait for the current operation to finish.")
		return
	}
	paths, err := mygo.Dialog.Open(mygo.OpenDialogOptions{
		Parent: a.win,
		Title:  "Open a checksum file",
		Filters: []mygo.FileFilter{
			{Name: "Checksum files", Extensions: []string{"sfv", "md5", "sha1", "sha256", "xxh3", "xxh", "txt"}},
			{Name: "All files", Extensions: []string{"*"}},
		},
	})
	if err != nil {
		a.dialogError(err)
		return
	}
	if len(paths) > 0 {
		a.handlePaths(paths)
	}
}

func (a *App) saveAs() {
	if a.modeJobs {
		a.setNotice("Job mode writes each checksum automatically when a job finishes.")
		return
	}
	if a.processing {
		return
	}
	if len(a.store) == 0 {
		a.setNotice("There are no files to save.")
		return
	}
	dir := a.saveDirectory()
	path, err := mygo.Dialog.Save(mygo.SaveDialogOptions{
		Parent:      a.win,
		Title:       "Save checksums",
		DefaultPath: filepath.Join(dir, checksum.DefaultName(a.algo)),
		Filters: []mygo.FileFilter{
			{Name: a.algo.Label(), Extensions: []string{strings.TrimPrefix(a.algo.Ext(), ".")}},
			{Name: "Text file", Extensions: []string{"txt"}},
		},
	})
	if err != nil {
		a.dialogError(err)
		return
	}
	if path == "" {
		return
	}
	n, err := checksum.WriteFile(path, a.algo, a.store, checksum.WriteOptions{
		Relative:  a.settings.PathMode == settings.Relative,
		SaveDir:   filepath.Dir(path),
		Prefix:    a.pathPrefix(),
		Comments:  a.settings.Comments,
		Signature: a.settings.Signature,
	})
	if err != nil {
		a.dialogError(err)
		return
	}
	a.setNotice(fmt.Sprintf("Wrote %d entries to %s", n, path))
}

func (a *App) saveDirectory() string {
	for i := range a.store {
		if a.store[i].Dir != "" {
			return a.store[i].Dir
		}
	}
	if len(a.store) > 0 {
		return filepath.Dir(a.store[0].FullPath())
	}
	if wd, err := os.Getwd(); err == nil {
		return wd
	}
	return "."
}

func (a *App) copyEntries() {
	if a.modeJobs {
		var b strings.Builder
		n := 0
		for i := range a.jobs {
			if !a.jobSel.Has(a.jobs[i].ID) {
				continue
			}
			fmt.Fprintf(&b, "%s\t%s\t%.1f%%\n", a.jobs[i].Name, a.jobs[i].Status, a.jobs[i].Progress)
			n++
		}
		if n == 0 {
			a.setNotice("Select one or more jobs to copy.")
			return
		}
		mygo.Clipboard.WriteText(b.String())
		a.setNotice(fmt.Sprintf("Copied %d job(s).", n))
		return
	}
	idx := a.selectedIndices()
	if len(idx) == 0 {
		a.setNotice("Select one or more files to copy.")
		return
	}
	a.copyIndices(idx)
}

func (a *App) copyIndices(idx []int) {
	var b strings.Builder
	for _, i := range idx {
		if i < 0 || i >= len(a.store) {
			continue
		}
		f := &a.store[i]
		if f.Hash != nil {
			b.WriteString(hexfmt.Upper(f.Hash))
		}
		b.WriteString(" *")
		if a.settings.ShowFullPaths {
			b.WriteString(f.FullPath())
		} else {
			b.WriteString(f.Rel)
		}
		b.WriteByte('\n')
	}
	mygo.Clipboard.WriteText(b.String())
	a.setNotice(fmt.Sprintf("Copied %d entries.", len(idx)))
}

func (a *App) pastePaths() {
	text := mygo.Clipboard.ReadText()
	if strings.TrimSpace(text) == "" {
		return
	}
	var paths []string
	for _, line := range strings.Split(text, "\n") {
		p := strings.TrimSpace(strings.Trim(line, `"'`))
		if p == "" {
			continue
		}
		if info, err := os.Stat(p); err == nil && (info.IsDir() || info.Mode().IsRegular()) {
			paths = append(paths, p)
		}
	}
	if len(paths) == 0 {
		a.setNotice("The clipboard does not contain existing paths.")
		return
	}
	a.handlePaths(paths)
}

func (a *App) compareClipboard() {
	if a.processing {
		a.setNotice("Busy: wait for the current operation to finish.")
		return
	}
	text := strings.TrimSpace(mygo.Clipboard.ReadText())
	if text == "" {
		a.setNotice("The clipboard is empty.")
		return
	}
	idx := a.selectedIndices()
	if len(idx) != 1 {
		if len(a.viewRows) == 1 {
			idx = []int{a.viewRows[0]}
		} else {
			a.setNotice("Select a single file to compare with the clipboard.")
			return
		}
	}
	if len(text)%2 == 1 {
		text = "0" + text
	}
	hash, ok := hexfmt.DecodeString(text)
	if !ok {
		a.setNotice("The clipboard does not contain a hexadecimal hash.")
		return
	}
	f := &a.store[idx[0]]
	f.Expected = hash
	if f.Hash != nil {
		if bytes.Equal(f.Hash, hash) {
			f.Status = model.OK
			a.setNotice("Clipboard matches " + f.Name)
		} else {
			f.Status = model.Bad
			a.setNotice("Clipboard does NOT match " + f.Name)
		}
		a.recountStats()
		a.applyView()
	} else {
		a.setNotice("Hash stored for " + f.Name + "; it has not been calculated yet.")
	}
	a.updateProblemMenus()
	a.invalidate()
}

func (a *App) selectAll() {
	if a.modeJobs {
		for i := range a.jobs {
			a.jobSel.Add(a.jobs[i].ID)
		}
		a.invalidate()
		return
	}
	for _, i := range a.viewRows {
		a.selection.Add(a.store[i].ID)
	}
	a.invalidate()
}

func (a *App) selectedIndices() []int {
	var idx []int
	for i := range a.store {
		if a.selection.Has(a.store[i].ID) {
			idx = append(idx, i)
		}
	}
	if len(idx) == 0 && len(a.viewRows) == 1 {
		idx = append(idx, a.viewRows[0])
	}
	return idx
}

func (a *App) selectedOrRow(row int) []int {
	if idx := a.selectedIndices(); len(idx) > 0 {
		return idx
	}
	if row >= 0 && row < len(a.viewRows) {
		return []int{a.viewRows[row]}
	}
	return nil
}

func (a *App) fileContextMenu(m *ui.Menu, row int) {
	if a.modeJobs || row < 0 || row >= len(a.viewRows) {
		return
	}
	targets := a.selectedOrRow(row)
	if len(targets) == 0 {
		return
	}
	single := len(targets) == 1
	exists := false
	if single {
		_, err := os.Stat(a.store[targets[0]].FullPath())
		exists = err == nil
	}
	if m.Item("Open Containing Folder").Disabled(!single || !exists).Chosen() {
		full := a.store[targets[0]].FullPath()
		a.deferAction(func() { mygo.Shell.ShowItemInFolder(full) })
	}
	m.Separator()
	copyLabel := "Copy Entry"
	if !single {
		copyLabel = "Copy Entries"
	}
	if m.Item(copyLabel).Chosen() {
		list := append([]int(nil), targets...)
		a.deferAction(func() { a.copyIndices(list) })
	}
	m.Separator()
	if m.Item("Rename File...").Disabled(!single || !exists || a.processing).Chosen() {
		idx := targets[0]
		a.deferAction(func() { a.beginRename(idx) })
	}
	deleteLabel := "Delete File..."
	if !single {
		deleteLabel = "Delete Files..."
	}
	if m.Item(deleteLabel).Disabled(a.processing).Chosen() {
		list := append([]int(nil), targets...)
		a.deferAction(func() { a.deleteIndices(list) })
	}
	if m.Item("Remove from List").Disabled(a.processing).Chosen() {
		list := append([]int(nil), targets...)
		a.deferAction(func() { a.removeIndices(list) })
	}
}

func (a *App) headerMenu(m *ui.Menu, col int) {
	if m.Item("Show Hash Column").Checked(a.settings.ShowHashCol).Chosen() {
		a.settings.ShowHashCol = !a.settings.ShowHashCol
		a.refreshColumns()
	}
	if m.Item("Show Expected Hash Column").Checked(a.settings.ShowExpectedCol).Chosen() {
		a.settings.ShowExpectedCol = !a.settings.ShowExpectedCol
		a.refreshColumns()
	}
	if m.Item("Show Time Elapsed").Checked(a.settings.ShowTime).Chosen() {
		a.settings.ShowTime = !a.settings.ShowTime
		a.refreshColumns()
	}
	m.Separator()
	if m.Item("Reset Column Layout").Chosen() {
		a.deferAction(a.resetColumns)
	}
	a.invalidate()
}

func (a *App) beginRename(idx int) {
	if idx < 0 || idx >= len(a.store) {
		return
	}
	a.renameIndex = idx
	a.renameText = a.store[idx].Name
	a.renameOpen = true
	a.invalidate()
}

func (a *App) commitRename() {
	if a.processing {
		a.renameOpen = false
		return
	}
	idx := a.renameIndex
	name := strings.TrimSpace(a.renameText)
	if idx < 0 || idx >= len(a.store) || name == "" || strings.ContainsAny(name, `/\`) {
		a.setNotice("Enter a file name without path separators.")
		return
	}
	f := &a.store[idx]
	full := f.FullPath()
	dest := filepath.Join(filepath.Dir(full), name)
	if dest != full {
		if err := os.Rename(full, dest); err != nil {
			a.dialogError(err)
			return
		}
	}
	f.Name = name
	dir := filepath.Dir(f.Rel)
	if dir == "." || dir == "" {
		f.Rel = name
	} else {
		f.Rel = filepath.Join(dir, name)
	}
	f.Full = dest
	a.renameOpen = false
	a.applyView()
	a.invalidate()
}

func (a *App) deleteIndices(idx []int) {
	if len(idx) == 0 || a.processing {
		return
	}
	res, err := mygo.Dialog.Message(mygo.MessageOptions{
		Parent:             a.win,
		Type:               mygo.MessageWarning,
		Title:              "Delete files",
		Message:            fmt.Sprintf("Delete %d file(s) from disk?", len(idx)),
		Detail:             "This cannot be undone.",
		Buttons:            []string{"Delete", "Cancel"},
		DefaultButton:      1,
		CancelButton:       1,
		DestructiveButtons: []int{0},
	})
	if err != nil || res.Button != 0 {
		return
	}
	var removed []int
	errs := 0
	for _, i := range idx {
		if i < 0 || i >= len(a.store) {
			continue
		}
		full := a.store[i].FullPath()
		if err := os.Remove(full); err == nil || os.IsNotExist(err) {
			removed = append(removed, i)
		} else {
			errs++
		}
	}
	a.removeIndices(removed)
	a.setNotice(fmt.Sprintf("Deleted %d file(s); %d error(s).", len(removed), errs))
}

func (a *App) removeIndices(idx []int) {
	if len(idx) == 0 || a.processing {
		return
	}
	remove := make(map[int]struct{}, len(idx))
	for _, i := range idx {
		remove[i] = struct{}{}
	}
	oldLen := len(a.store)
	store := a.store[:0]
	for i := 0; i < oldLen; i++ {
		if _, drop := remove[i]; !drop {
			store = append(store, a.store[i])
		}
	}
	for i := len(store); i < oldLen; i++ {
		a.store[i] = model.File{}
	}
	a.store = store
	a.selection.Clear()
	a.viewRows = nil
	a.recountStats()
	a.applyView()
	a.updateTitle()
	a.updateProblemMenus()
	a.invalidate()
}

func (a *App) recountStats() {
	p := a.stats
	p.Total = len(a.store)
	p.OK, p.Bad, p.Missing, p.Error, p.Done = 0, 0, 0, 0, 0
	for i := range a.store {
		switch a.store[i].Status {
		case model.OK:
			p.OK++
		case model.Bad:
			p.Bad++
		case model.Missing:
			p.Missing++
		case model.Error:
			p.Error++
		}
		if a.store[i].Status != model.Queued && a.store[i].Status != model.Pending {
			p.Done++
		}
	}
	a.stats = p
}

func (a *App) moveBadFiles() {
	if a.processing {
		return
	}
	var idx []int
	for i := range a.store {
		if a.store[i].Status.IsProblem() {
			idx = append(idx, i)
		}
	}
	if len(idx) == 0 {
		a.setNotice("There are no BAD or ERROR files.")
		return
	}
	res, err := mygo.Dialog.Message(mygo.MessageOptions{
		Parent:             a.win,
		Type:               mygo.MessageWarning,
		Title:              "Move Bad Files",
		Message:            fmt.Sprintf("Move %d BAD/ERROR file(s) to a '%s' subfolder?", len(idx), badtools.BadDir),
		Buttons:            []string{"Move", "Cancel"},
		DefaultButton:      1,
		CancelButton:       1,
		DestructiveButtons: []int{0},
	})
	if err != nil || res.Button != 0 {
		return
	}
	moved, errs := 0, 0
	for _, i := range idx {
		f := &a.store[i]
		full := f.FullPath()
		if _, err := os.Stat(full); err != nil {
			errs++
			continue
		}
		newPath, err := badtools.Move(full)
		if err != nil {
			errs++
			continue
		}
		f.Rel = badtools.NewRelativePath(f.Rel)
		f.Full = newPath
		moved++
	}
	a.applyView()
	a.setNotice(fmt.Sprintf("Moved %d file(s); %d error(s).", moved, errs))
}

func (a *App) renameBadFiles() {
	if a.processing {
		return
	}
	var idx []int
	for i := range a.store {
		if a.store[i].Status.IsProblem() {
			idx = append(idx, i)
		}
	}
	if len(idx) == 0 {
		a.setNotice("There are no BAD or ERROR files.")
		return
	}
	res, err := mygo.Dialog.Message(mygo.MessageOptions{
		Parent:             a.win,
		Type:               mygo.MessageWarning,
		Title:              "Rename Bad Files",
		Message:            fmt.Sprintf("Append '.CORRUPT' to %d BAD/ERROR file(s)?", len(idx)),
		Buttons:            []string{"Rename", "Cancel"},
		DefaultButton:      1,
		CancelButton:       1,
		DestructiveButtons: []int{0},
	})
	if err != nil || res.Button != 0 {
		return
	}
	renamed, errs := 0, 0
	for _, i := range idx {
		f := &a.store[i]
		newPath, err := badtools.Rename(f.FullPath())
		if err != nil {
			errs++
			continue
		}
		f.Name += ".CORRUPT"
		f.Rel += ".CORRUPT"
		f.Full = newPath
		renamed++
	}
	a.applyView()
	a.setNotice(fmt.Sprintf("Renamed %d file(s); %d error(s).", renamed, errs))
}

func (a *App) generateBadScript() {
	if a.processing {
		return
	}
	var paths []string
	for i := range a.store {
		if a.store[i].Status.IsProblem() {
			paths = append(paths, a.store[i].FullPath())
		}
	}
	if len(paths) == 0 {
		a.setNotice("There are no BAD or ERROR files.")
		return
	}
	path, err := mygo.Dialog.Save(mygo.SaveDialogOptions{
		Parent:      a.win,
		Title:       "Save delete script",
		DefaultPath: filepath.Join(a.saveDirectory(), "delete_bad_files.bat"),
		Filters:     []mygo.FileFilter{{Name: "Batch script", Extensions: []string{"bat"}}},
	})
	if err != nil {
		a.dialogError(err)
		return
	}
	if path == "" {
		return
	}
	if err := badtools.WriteDeleteScript(path, paths); err != nil {
		a.dialogError(err)
		return
	}
	a.setNotice("Wrote " + path)
}

func (a *App) generateDuplicateScript(copyFiles bool) {
	if a.processing {
		return
	}
	groups := a.duplicates()
	if len(groups) == 0 {
		a.setNotice("Enable 'Show Duplicates' and hash files first.")
		return
	}
	paths := make([][]string, 0, len(groups))
	for _, g := range groups {
		list := make([]string, 0, len(g))
		for _, i := range g {
			list = append(list, a.store[i].FullPath())
		}
		if len(list) > 1 {
			paths = append(paths, list)
		}
	}
	name := "delete_duplicates.bat"
	if copyFiles {
		name = "copy_duplicates.bat"
	}
	path, err := mygo.Dialog.Save(mygo.SaveDialogOptions{
		Parent:      a.win,
		Title:       "Save duplicate script",
		DefaultPath: filepath.Join(a.saveDirectory(), name),
		Filters:     []mygo.FileFilter{{Name: "Batch script", Extensions: []string{"bat"}}},
	})
	if err != nil {
		a.dialogError(err)
		return
	}
	if path == "" {
		return
	}
	if copyFiles {
		err = badtools.WriteCopyDuplicatesScript(path, paths)
	} else {
		err = badtools.WriteDeleteDuplicatesScript(path, paths)
	}
	if err != nil {
		a.dialogError(err)
		return
	}
	a.setNotice("Wrote " + path)
}

func (a *App) registerShell() {
	if runtime.GOOS != "windows" {
		a.setNotice(shell.ErrUnsupported.Error())
		return
	}
	if err := shell.Register(shell.Self()); err != nil {
		a.dialogError(err)
		return
	}
	a.syncShellMenu()
	_, _ = mygo.Dialog.Message(mygo.MessageOptions{
		Parent:  a.win,
		Type:    mygo.MessageInfo,
		Title:   "GoSFV",
		Message: "Explorer integration registered.",
		Detail:  "Right-click a file or folder and choose 'GoSFV: Create Checksum'.",
	})
}

func (a *App) unregisterShell() {
	if runtime.GOOS != "windows" {
		a.setNotice(shell.ErrUnsupported.Error())
		return
	}
	if err := shell.Unregister(); err != nil {
		a.dialogError(err)
		return
	}
	a.syncShellMenu()
	a.setNotice("Explorer integration removed.")
}

func (a *App) showDiskInfo() {
	path, _ := os.Executable()
	if len(a.store) > 0 {
		path = a.store[0].FullPath()
	}
	info := drive.Detect(path)
	detail := info.String()
	detail += fmt.Sprintf("\n\nHex codec: %s\nGo runtime: %s\nArchitecture: %s/%s\nCPU threads: %d\nHash workers: %d (GOMAXPROCS)",
		hexfmt.Backend(), runtime.Version(), runtime.GOOS, runtime.GOARCH, runtime.NumCPU(), runtime.GOMAXPROCS(0))
	detail += "\nHash backends: xxHash3 assembly, CRC-32 PCLMULQDQ/CRC, MD5/SHA runtime assembly."
	_, _ = mygo.Dialog.Message(mygo.MessageOptions{
		Parent:  a.win,
		Type:    mygo.MessageInfo,
		Title:   "Disk and Backend Info",
		Message: "GoSFV storage and hashing",
		Detail:  detail,
	})
}

func (a *App) showCredits() {
	_, _ = mygo.Dialog.Message(mygo.MessageOptions{
		Parent:  a.win,
		Type:    mygo.MessageInfo,
		Title:   "About GoSFV",
		Message: "GoSFV 1.0.0",
		Detail: "A fast checksum creator and verifier with a native Go interface.\n\n" +
			"Project: https://github.com/lrweck/gosfv\n" +
			"Native UI by MyGo 0.9.0: https://github.com/egoist/mygo\n\n" +
			"GoSFV is licensed under GPL-3.0; see NOTICE for attribution.",
	})
}

func (a *App) resetDefaults() {
	if a.processing {
		return
	}
	res, err := mygo.Dialog.Message(mygo.MessageOptions{
		Parent:        a.win,
		Type:          mygo.MessageQuestion,
		Title:         "Reset settings",
		Message:       "Reset all options to their defaults?",
		Buttons:       []string{"Reset", "Cancel"},
		DefaultButton: 1,
		CancelButton:  1,
	})
	if err != nil || res.Button != 0 {
		return
	}
	_ = settings.Reset(a.settingsPath)
	a.settings = settings.Defaults()
	a.settingsSave = false
	a.algo = a.settings.DefaultAlgo
	a.clearList()
	a.syncAlgoMenu()
	a.syncViewMenu()
	a.setProcMode(a.settings.ProcMode)
	a.setPathMode(a.settings.PathMode)
	a.setNotice("Settings reset to defaults.")
}

func (a *App) openSelectedFolder() {
	idx := a.selectedIndices()
	if len(idx) == 0 && len(a.viewRows) > 0 {
		idx = []int{a.viewRows[0]}
	}
	if len(idx) == 0 {
		return
	}
	mygo.Shell.ShowItemInFolder(a.store[idx[0]].FullPath())
}

func (a *App) dialogError(err error) {
	_, _ = mygo.Dialog.Message(mygo.MessageOptions{
		Parent:  a.win,
		Type:    mygo.MessageError,
		Title:   "GoSFV",
		Message: "Something went wrong.",
		Detail:  err.Error(),
	})
}
