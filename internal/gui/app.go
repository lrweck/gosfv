// Package gui is GoSFV's native MyGo interface.
//
// The view is a function of the App: the file table reads a slice of indices,
// while workers only publish immutable results that are applied on the main
// thread in batches. That keeps the table lock free and makes a million-row
// list no slower to draw than a short one.
package gui

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"

	"github.com/lrweck/gosfv/internal/checksum"
	"github.com/lrweck/gosfv/internal/drive"
	"github.com/lrweck/gosfv/internal/engine"
	"github.com/lrweck/gosfv/internal/hasher"
	"github.com/lrweck/gosfv/internal/model"
	"github.com/lrweck/gosfv/internal/runner"
	"github.com/lrweck/gosfv/internal/scanner"
	"github.com/lrweck/gosfv/internal/settings"
)

// Config is what main decided before the UI starts.
type Config struct {
	Paths        []string
	CreateMode   bool
	CreateOutput string
	SettingsPath string
	Algo         hasher.Kind
	AlgoSet      bool
	Theme        settings.ThemeMode
	ThemeSet     bool
	Version      string
}

// App is the whole window state.
type App struct {
	win          *mygo.Window
	settings     settings.Settings
	settingsPath string
	settingsSave bool
	version      string

	themeFrom *ui.Theme
	themeTo   *ui.Theme
	themeAt   time.Time

	mu      sync.Mutex
	active  map[int32]activeFile
	closing atomic.Bool
	started atomic.Bool

	store     []model.File
	viewRows  []int
	byID      []int
	list      ui.ListState
	selection ui.Selection[uint64]
	activeRow int
	cols      []ui.TableColumn

	sel       selectionInfo
	selSig    selectionSig
	selDirty  bool
	selSeq    uint64
	selAt     time.Time
	selCancel context.CancelFunc

	jobs       []model.Job
	jobList    ui.ListState
	jobSel     ui.Selection[int]
	jobCols    []ui.TableColumn
	currentJob int
	nextJob    int

	modeJobs   bool
	jobRunning bool
	queueStop  atomic.Bool

	algo       hasher.Kind
	algoSet    bool
	verifying  bool
	processing bool
	paused     bool
	scanning   bool
	scanCount  int
	cancelled  bool
	legacy     bool
	comments   []string

	gate   *engine.Gate
	cancel context.CancelFunc

	stats      runner.Progress
	elapsed    time.Duration
	activeView []activeFile

	filterText     string
	statusFilter   int
	showDuplicates bool
	filterAt       time.Time

	sort        ui.SortOrder
	appliedSort ui.SortOrder
	sortDirty   bool

	notice string

	createMode   bool
	base         string
	autoSave     string
	collected    []string
	createLaunch bool
	createTimer  *time.Timer

	renameOpen  bool
	renameIndex int
	renameText  string

	pendingActions []func()

	miShowHash     *mygo.MenuItem
	miShowExpected *mygo.MenuItem
	miShowTime     *mygo.MenuItem
	miThroughput   *mygo.MenuItem
	miFullPaths    *mygo.MenuItem
	miLockColumns  *mygo.MenuItem
	miFilter       *mygo.MenuItem
	miAdvanced     *mygo.MenuItem
	miDetails      *mygo.MenuItem
	miModeStandard *mygo.MenuItem
	miModeJob      *mygo.MenuItem
	miClearJobs    *mygo.MenuItem
	miProcAuto     *mygo.MenuItem
	miProcHDD      *mygo.MenuItem
	miProcSSD      *mygo.MenuItem
	miPathRel      *mygo.MenuItem
	miPathAbs      *mygo.MenuItem
	miThemeSystem  *mygo.MenuItem
	miThemeLight   *mygo.MenuItem
	miThemeDark    *mygo.MenuItem
	miBadMove      *mygo.MenuItem
	miBadRename    *mygo.MenuItem
	miBadScript    *mygo.MenuItem
	miDupCopy      *mygo.MenuItem
	miDupDelete    *mygo.MenuItem
	miAlgos        map[hasher.Kind]*mygo.MenuItem
	miShell        *mygo.MenuItem
	miUnshell      *mygo.MenuItem
}

type activeFile struct {
	Name  string
	Done  int64
	Total int64
}

// Run creates the app, takes the single-instance lock and starts the event
// loop. It must be called from main, on the main goroutine.
func Run(cfg Config) error {
	path := cfg.SettingsPath
	if path == "" {
		path = settings.DefaultPath()
	}
	st := settings.Load(path)
	if cfg.ThemeSet {
		st.Theme = cfg.Theme
	}
	a := &App{
		settings:     st,
		settingsPath: path,
		version:      cfg.Version,
		algo:         st.DefaultAlgo,
		active:       map[int32]activeFile{},
		statusFilter: 0,
	}
	if cfg.AlgoSet {
		a.algo = cfg.Algo
		a.algoSet = true
		a.settings.DefaultAlgo = cfg.Algo
	}
	if cfg.CreateMode {
		a.createMode = true
	}
	// The appearance must be chosen before the first frame: the window and
	// every widget take their colors from it.
	mygo.Theme.SetSource(themeSource(st.Theme))
	if !mygo.App.RequestSingleInstanceLock() {
		return nil
	}
	mygo.App.OnSecondInstance(a.secondInstance)
	mygo.App.OnBeforeQuit(func(*mygo.QuitEvent) { a.saveSettings() })
	mygo.App.WhenReady(func() { a.start(cfg) })
	return mygo.App.Run()
}

func (a *App) start(cfg Config) {
	a.started.Store(true)
	if a.createMode {
		a.win = mygo.NewWindow(mygo.WindowOptions{
			Title:           "GoSFV - Create Checksum",
			Width:           560,
			Height:          190,
			MinWidth:        460,
			MinHeight:       170,
			DisableResize:   false,
			DisableMaximize: true,
			Hidden:          true,
			Content:         ui.View(a.createView),
		})
		a.win.SetIcon(iconPNG)
		a.win.SetWindowButtonsTheme(themeSource(a.settings.Theme))
		a.win.OnClose(func(*mygo.CloseEvent) {
			a.closing.Store(true)
			if a.cancel != nil {
				a.cancel()
			}
		})
		a.collected = append(a.collected, cfg.Paths...)
		a.autoSave = cfg.CreateOutput
		if len(a.collected) > 0 {
			a.scheduleCreate()
		} else {
			mygo.App.Quit()
		}
		return
	}

	a.win = mygo.NewWindow(mygo.WindowOptions{
		Title:     "GoSFV",
		Width:     1260,
		Height:    740,
		MinWidth:  760,
		MinHeight: 460,
		StateKey:  "main",
		Content:   ui.View(a.view),
	})
	a.win.SetIcon(iconPNG)
	a.win.SetWindowButtonsTheme(themeSource(a.settings.Theme))
	a.win.OnClose(func(*mygo.CloseEvent) {
		a.closing.Store(true)
		if a.cancel != nil {
			a.cancel()
		}
		a.saveSettings()
	})
	a.refreshColumns()
	a.resetList()
	a.resetJobList()
	a.buildMenu()
	a.updateTitle()
	paths := append([]string(nil), a.collected...)
	a.collected = nil
	paths = append(paths, cfg.Paths...)
	if len(paths) > 0 {
		a.handlePaths(paths)
	}
}

func (a *App) secondInstance(args []string, wd string) {
	paths := cleanArgs(args, wd)
	if len(paths) == 0 {
		return
	}
	if !a.started.Load() {
		a.collected = append(a.collected, paths...)
		return
	}
	if a.createMode {
		a.collected = append(a.collected, paths...)
		a.scheduleCreate()
		return
	}
	if a.win != nil {
		a.win.Restore()
		a.win.Focus()
	}
	a.handlePaths(paths)
}

func cleanArgs(args []string, wd string) []string {
	var out []string
	for _, arg := range args {
		if arg == "" || strings.HasPrefix(arg, "-") {
			continue
		}
		if !filepath.IsAbs(arg) {
			arg = filepath.Join(wd, arg)
		}
		out = append(out, filepath.Clean(arg))
	}
	return out
}

func (a *App) invalidate() {
	if a.closing.Load() {
		return
	}
	if a.win != nil && !a.win.IsDestroyed() {
		a.win.Invalidate()
	}
}

func (a *App) refreshNow() {
	if a.closing.Load() || a.win == nil || a.win.IsDestroyed() {
		return
	}
	mygo.RunOnMain(func() {
		a.invalidate()
	})
}

func (a *App) saveSettings() {
	if a.settingsSave || a.createMode {
		return
	}
	a.settingsSave = true
	if !a.settings.LockColumns {
		a.settings.ColumnOrder = append([]string(nil), a.list.Columns.Order...)
		if len(a.list.Columns.Widths) > 0 {
			a.settings.ColumnWidths = make(map[string]float32, len(a.list.Columns.Widths))
			for k, v := range a.list.Columns.Widths {
				a.settings.ColumnWidths[k] = v
			}
		}
	}
	if err := a.settings.Save(a.settingsPath); err != nil {
		fmt.Fprintf(os.Stderr, "GoSFV: saving settings: %v\n", err)
	}
}

func (a *App) resetList() {
	a.list = ui.ListState{}
	a.list.KeepOffset = true
	a.list.Sort = &a.sort
	a.activeRow = -1
	a.list.Selected = &a.activeRow
	a.list.Selection = &a.selection
	a.list.Key = func(row int) any {
		if row < 0 || row >= len(a.viewRows) {
			return nil
		}
		return a.store[a.viewRows[row]].ID
	}
	a.list.Label = func(row int) string {
		if row < 0 || row >= len(a.viewRows) {
			return ""
		}
		return a.store[a.viewRows[row]].Name
	}
	a.list.CellMenu = func(m *ui.Menu, row, col int) { a.fileContextMenu(m, row) }
	a.list.HeaderMenu = func(m *ui.Menu, col int) { a.headerMenu(m, col) }
	if len(a.settings.ColumnOrder) > 0 {
		a.list.Columns.Order = append([]string(nil), a.settings.ColumnOrder...)
	}
	if len(a.settings.ColumnWidths) > 0 {
		a.list.Columns.Widths = make(map[string]float32, len(a.settings.ColumnWidths))
		for k, v := range a.settings.ColumnWidths {
			a.list.Columns.Widths[k] = v
		}
	}
	a.sort = ui.SortOrder{}
	a.appliedSort = ui.SortOrder{}
}

func (a *App) resetJobList() {
	a.jobList = ui.ListState{}
	a.jobList.Selection = &a.jobSel
	a.jobList.Key = func(row int) any {
		if row < 0 || row >= len(a.jobs) {
			return nil
		}
		return a.jobs[row].ID
	}
	a.jobList.Label = func(row int) string {
		if row < 0 || row >= len(a.jobs) {
			return ""
		}
		return a.jobs[row].Name
	}
	a.jobList.CellMenu = func(m *ui.Menu, row, col int) { a.jobContextMenu(m, row) }
}

func (a *App) refreshColumns() {
	cols := []ui.TableColumn{
		{Title: "File Name", ID: "Name", Width: 0, MinWidth: 260, MaxWidth: 520, Sortable: true},
	}
	if a.settings.ShowHashCol {
		cols = append(cols, ui.TableColumn{Title: "Hash", ID: "Hash", Width: 430, MinWidth: 120, Sortable: true})
	}
	cols = append(cols, ui.TableColumn{Title: "Status", ID: "Status", Width: 90, MinWidth: 70, Sortable: true})
	if a.verifying && a.settings.ShowExpectedCol {
		cols = append(cols, ui.TableColumn{Title: "Expected Hash", ID: "Expected", Width: 430, MinWidth: 120, Sortable: true})
	}
	if a.settings.ShowTime {
		cols = append(cols, ui.TableColumn{Title: "Time", ID: "Time", Width: 90, MinWidth: 60, Sortable: true})
	}
	a.cols = cols
	a.jobCols = []ui.TableColumn{
		{Title: "Job Name", ID: "JobName", Width: 240, MinWidth: 100},
		{Title: "Root Path", ID: "RootPath", Width: 360, MinWidth: 120},
		{Title: "Progress", ID: "Progress", Width: 110, MinWidth: 70, Align: ui.End},
		{Title: "Status", ID: "JobStatus", Width: 120, MinWidth: 80},
		{Title: "Time", ID: "JobTime", Width: 90, MinWidth: 60, Align: ui.End},
	}
}

func (a *App) applyView() {
	if a.modeJobs {
		return
	}
	q := a.filterText
	status := a.statusFilter
	view := a.viewRows[:0]
	if a.showDuplicates {
		groups := a.duplicates()
		for _, g := range groups {
			view = append(view, g...)
		}
		slices.SortFunc(view, func(i, j int) int {
			if c := compareBytes(a.store[i].Hash, a.store[j].Hash); c != 0 {
				return c
			}
			return strings.Compare(a.store[i].Name, a.store[j].Name)
		})
	} else {
		for i := range a.store {
			f := &a.store[i]
			if q != "" && !containsFold(f.Rel, q) {
				continue
			}
			if !statusMatches(status, f.Status) {
				continue
			}
			view = append(view, i)
		}
		a.sortView(view)
	}
	a.viewRows = view
	a.appliedSort = a.sort
	a.selDirty = true
}

func (a *App) sortView(view []int) {
	col := a.sort.Column
	if col == "" {
		return
	}
	desc := a.sort.Descending
	slices.SortFunc(view, func(i, j int) int {
		fi := a.store[i]
		fj := a.store[j]
		var c int
		switch col {
		case "Name":
			c = compareFold(fi.Rel, fj.Rel)
			if c == 0 {
				c = compareFold(fi.Name, fj.Name)
			}
		case "Hash":
			c = compareBytes(fi.Hash, fj.Hash)
		case "Status":
			c = int(fi.Status) - int(fj.Status)
		case "Expected":
			c = compareBytes(fi.Expected, fj.Expected)
		case "Time":
			if fi.Elapsed < fj.Elapsed {
				c = -1
			} else if fi.Elapsed > fj.Elapsed {
				c = 1
			}
		}
		if c == 0 {
			switch {
			case fi.ID < fj.ID:
				c = -1
			case fi.ID > fj.ID:
				c = 1
			}
		}
		if desc {
			return -c
		}
		return c
	})
}

func (a *App) duplicates() [][]int {
	groups := make(map[string][]int)
	for i := range a.store {
		h := a.store[i].Hash
		if len(h) == 0 {
			continue
		}
		groups[string(h)] = append(groups[string(h)], i)
	}
	out := make([][]int, 0, 8)
	for _, g := range groups {
		if len(g) > 1 {
			out = append(out, g)
		}
	}
	slices.SortFunc(out, func(ga, gb []int) int {
		return strings.Compare(string(a.store[ga[0]].Hash), string(a.store[gb[0]].Hash))
	})
	return out
}

func statusMatches(filter int, s model.Status) bool {
	switch filter {
	case 1:
		return s == model.Queued
	case 2:
		return s == model.Pending
	case 3:
		return s == model.OK
	case 4:
		return s == model.Bad || s == model.Error || s == model.Missing
	case 5:
		return s == model.Missing
	}
	return true
}

func containsFold(s, q string) bool {
	if q == "" {
		return true
	}
	if len(q) > len(s) {
		return false
	}
	for i := 0; i+len(q) <= len(s); i++ {
		ok := true
		for j := 0; j < len(q); j++ {
			if lowerASCII(s[i+j]) != lowerASCII(q[j]) {
				ok = false
				break
			}
		}
		if ok {
			return true
		}
	}
	return false
}

func compareFold(a, b string) int {
	n := len(a)
	if len(b) < n {
		n = len(b)
	}
	for i := 0; i < n; i++ {
		ca, cb := lowerASCII(a[i]), lowerASCII(b[i])
		if ca != cb {
			if ca < cb {
				return -1
			}
			return 1
		}
	}
	switch {
	case len(a) < len(b):
		return -1
	case len(a) > len(b):
		return 1
	}
	return 0
}

func lowerASCII(c byte) byte {
	if c >= 'A' && c <= 'Z' {
		return c + ('a' - 'A')
	}
	return c
}

func compareBytes(a, b []byte) int {
	n := len(a)
	if len(b) < n {
		n = len(b)
	}
	for i := 0; i < n; i++ {
		if a[i] != b[i] {
			if a[i] < b[i] {
				return -1
			}
			return 1
		}
	}
	switch {
	case len(a) < len(b):
		return -1
	case len(a) > len(b):
		return 1
	}
	return 0
}

func (a *App) installFiles(files []model.File) {
	a.store = files
	a.byID = nil // IDs are 1..n until a removal
	a.selDirty = true
	a.viewRows = make([]int, 0, len(files))
	a.selection.Clear()
	a.jobSel.Clear()
	a.active = map[int32]activeFile{}
	a.activeView = nil
	a.scanning = false
	a.scanCount = len(files)
	a.legacy = false
	a.elapsed = 0
	a.resetList()
	a.refreshColumns()
	a.applyView()
	a.stats = runner.Progress{Total: len(files)}
	for i := range files {
		if files[i].Status == model.Missing {
			a.stats.Missing++
		}
	}
	a.updateTitle()
}

func (a *App) applyTick(p runner.Progress, done []runner.Completed) {
	for _, d := range done {
		if d.Index < 0 || d.Index >= len(a.store) {
			continue
		}
		f := &a.store[d.Index]
		if d.Status == model.Error {
			f.Hash = nil
		} else {
			f.Hash = d.Hash
		}
		f.Status = d.Status
		f.Elapsed = d.Elapsed
		delete(a.active, int32(d.Index))
	}
	a.stats = p
	a.elapsed = p.Elapsed
	a.syncActive()
	if a.modeJobs {
		a.updateJobProgress(p)
	}
	a.updateProblemMenus()
}

func (a *App) clearActive() {
	a.mu.Lock()
	a.active = map[int32]activeFile{}
	a.mu.Unlock()
	a.activeView = nil
}

func (a *App) syncActive() {
	a.mu.Lock()
	if len(a.active) == 0 {
		a.activeView = nil
	} else {
		list := make([]activeFile, 0, len(a.active))
		for _, f := range a.active {
			list = append(list, f)
		}
		slices.SortFunc(list, func(x, y activeFile) int { return strings.Compare(x.Name, y.Name) })
		a.activeView = list
	}
	a.mu.Unlock()
}

func (a *App) onFileProgress(index int32, path string, done, total int64) {
	name := filepath.Base(path)
	a.mu.Lock()
	// CRC chunks report out of order; never let the bar move backwards.
	if old, ok := a.active[index]; ok && done <= old.Done {
		a.mu.Unlock()
		return
	}
	a.active[index] = activeFile{Name: name, Done: done, Total: total}
	a.mu.Unlock()
}

func (a *App) startRun(paths []string, base string, verify bool, checksumPath string) {
	if a.processing {
		a.notice = "Busy: wait for the current operation to finish."
		a.invalidate()
		return
	}
	a.processing = true
	a.paused = false
	a.cancelled = false
	a.notice = ""
	a.verifying = verify
	a.scanning = !verify
	a.scanCount = 0
	a.comments = nil
	a.legacy = false
	a.active = map[int32]activeFile{}
	a.activeView = nil
	a.stats = runner.Progress{}
	a.elapsed = 0
	a.base = base
	a.store = nil
	a.byID = nil
	a.viewRows = nil
	a.selection.Clear()
	a.selDirty = true
	a.resetList()
	a.gate = engine.NewGate()
	ctx, cancel := context.WithCancel(context.Background())
	a.cancel = cancel
	if verify {
		if k, ok := hasher.FromExtension(filepath.Ext(checksumPath)); ok {
			a.algo = k
		}
	}
	a.refreshColumns()
	a.syncAlgoMenu()
	a.syncViewMenu()
	a.updateTitle()
	a.updateProblemMenus()
	a.invalidate()

	seqPath := base
	if verify {
		seqPath = filepath.Dir(checksumPath)
	}
	seq := drive.Sequential(a.settings.ProcMode, drive.Detect(seqPath))
	opt := runner.Options{
		Algo:       a.algo,
		Sequential: seq,
		Gate:       a.gate,
		Scan:       a.scanOptions(),
		OnScan: func(found int) {
			mygo.RunOnMain(func() { a.scanCount = found })
			a.invalidate()
		},
		OnFiles: func(files []model.File) {
			mygo.RunOnMain(func() { a.installFiles(files) })
			a.invalidate()
		},
		OnParsed: func(comments []string) {
			mygo.RunOnMain(func() { a.comments = comments })
			a.invalidate()
		},
		OnTick: func(p runner.Progress, done []runner.Completed) {
			mygo.RunOnMain(func() { a.applyTick(p, done) })
			a.invalidate()
		},
		OnFileProgress: a.onFileProgress,
	}
	go func() {
		var rep *runner.Report
		var err error
		if verify {
			rep, err = runner.Verify(ctx, checksumPath, opt)
		} else {
			rep, err = runner.Create(ctx, paths, base, opt)
		}
		mygo.RunOnMain(func() { a.finishRun(rep, err) })
		a.invalidate()
	}()
}

func (a *App) finishRun(rep *runner.Report, err error) {
	a.processing = false
	a.scanning = false
	a.paused = false
	a.cancel = nil
	a.clearActive()
	if err != nil {
		a.notice = err.Error()
		if !errors.Is(err, context.Canceled) {
			_, _ = mygo.Dialog.Message(mygo.MessageOptions{
				Parent:  a.win,
				Type:    mygo.MessageError,
				Title:   "GoSFV",
				Message: "The operation failed.",
				Detail:  err.Error(),
			})
		}
		a.selDirty = true
		a.updateTitle()
		a.updateProblemMenus()
		a.invalidate()
		return
	}
	if rep != nil {
		a.stats = rep.Progress
		a.elapsed = rep.Progress.Elapsed
		a.legacy = rep.Legacy
		if a.verifying {
			a.comments = rep.Comments
		}
		if rep.Progress.Total == 0 && !a.cancelled {
			if a.verifying {
				a.notice = "The checksum file has no entries."
			} else {
				a.notice = "No files matched."
			}
		} else if rep.ParallelCRC {
			a.notice = fmt.Sprintf("CRC-32 split across %d workers.", rep.CRCWorkers)
		} else if rep.Workers > 1 && rep.Progress.Total < rep.Workers && rep.Algo != hasher.CRC32 {
			a.notice = fmt.Sprintf("%s is a sequential digest; each file uses one worker.", rep.Algo.ShortLabel())
		}
	}
	a.selDirty = true
	a.updateTitle()
	a.updateProblemMenus()
	a.syncAlgoMenu()
	a.syncViewMenu()
	if a.statusFilter != 0 || a.showDuplicates {
		a.applyView()
	}
	if a.createMode {
		if a.cancelled {
			mygo.App.Quit()
		} else {
			a.saveAuto()
		}
	}
	a.invalidate()
}

func (a *App) saveAuto() {
	if a.autoSave == "" {
		mygo.App.Quit()
		return
	}
	_, err := checksum.WriteFile(a.autoSave, a.algo, a.store, checksum.WriteOptions{
		Relative:  true,
		SaveDir:   filepath.Dir(a.autoSave),
		Prefix:    a.pathPrefix(),
		Comments:  a.settings.Comments,
		Signature: a.settings.Signature,
		OnlyOK:    true,
	})
	if err != nil {
		_, _ = mygo.Dialog.Message(mygo.MessageOptions{
			Parent:  a.win,
			Type:    mygo.MessageError,
			Title:   "GoSFV",
			Message: "Could not write the checksum file.",
			Detail:  err.Error(),
		})
		a.processing = false
		return
	}
	a.notice = "Wrote " + a.autoSave
	mygo.App.Quit()
}

func (a *App) scanOptions() runner.ScanOptions {
	o := runner.ScanOptions{Recursive: a.settings.Recursive, SkipHidden: true}
	if a.settings.ShowAdvanced {
		o.Include = a.settings.IncludePattern
		o.Exclude = a.settings.ExcludePattern
	}
	return o
}

func (a *App) pathPrefix() string {
	if a.settings.ShowAdvanced {
		return a.settings.PathPrefix
	}
	return ""
}

func (a *App) updateProblemMenus() {
	has := !a.processing && !a.modeJobs && a.stats.Bad+a.stats.Error > 0
	for _, item := range []*mygo.MenuItem{a.miBadMove, a.miBadRename, a.miBadScript} {
		setMenuItemEnabled(item, has)
	}
	dups := !a.processing && !a.modeJobs && a.showDuplicates && len(a.viewRows) > 0
	for _, item := range []*mygo.MenuItem{a.miDupCopy, a.miDupDelete} {
		setMenuItemEnabled(item, dups)
	}
	setMenuItemEnabled(a.miClearJobs, !a.processing && a.hasCompletedJobs())
}

func (a *App) hasCompletedJobs() bool {
	for i := range a.jobs {
		if a.jobs[i].Status == model.JobDone {
			return true
		}
	}
	return false
}

func (a *App) updateTitle() {
	if a.win == nil || a.closing.Load() || a.win.IsDestroyed() {
		return
	}
	a.win.SetTitle(a.title())
}

func (a *App) title() string {
	if a.createMode {
		if a.processing {
			return "GoSFV - Creating..."
		}
		return "GoSFV - Create Checksum"
	}
	if a.modeJobs {
		if a.jobRunning {
			return "GoSFV - Job Queue [" + a.algo.ShortLabel() + "] - Running"
		}
		return "GoSFV - Job Queue [" + a.algo.ShortLabel() + "]"
	}
	if a.processing {
		if a.verifying {
			return "GoSFV - Verifying... [" + a.algo.ShortLabel() + "]"
		}
		return "GoSFV - Creating... [" + a.algo.ShortLabel() + "]"
	}
	if a.verifying && a.stats.Total > 0 {
		if a.stats.Bad == 0 && a.stats.Missing == 0 && a.stats.Error == 0 {
			return "GoSFV [" + a.algo.ShortLabel() + "] - All Files OK"
		}
		if a.stats.Error > 0 {
			return fmt.Sprintf("GoSFV [%s] - %d Bad, %d Missing, %d Error", a.algo.ShortLabel(), a.stats.Bad, a.stats.Missing, a.stats.Error)
		}
		return fmt.Sprintf("GoSFV [%s] - %d Bad, %d Missing", a.algo.ShortLabel(), a.stats.Bad, a.stats.Missing)
	}
	if a.cancelled {
		return "GoSFV - Stopped"
	}
	if len(a.store) > 0 && !a.verifying {
		return "GoSFV - Creation Complete [" + a.algo.ShortLabel() + "]"
	}
	return "GoSFV [" + a.algo.ShortLabel() + "]"
}

func (a *App) togglePause() {
	if !a.processing || a.gate == nil {
		return
	}
	a.paused = !a.paused
	a.gate.Set(a.paused)
	if a.modeJobs {
		a.setCurrentJobPaused(a.paused)
	}
	a.invalidate()
}

func (a *App) stopRun() {
	if a.modeJobs && a.jobRunning && !a.processing {
		// The queue is between jobs: stop it from starting the next one.
		a.queueStop.Store(true)
		a.setNotice("Job queue stopped.")
		return
	}
	if !a.processing {
		return
	}
	a.cancelled = true
	a.paused = false
	if a.gate != nil {
		a.gate.Set(false)
	}
	if a.modeJobs {
		a.queueStop.Store(true)
	}
	if a.cancel != nil {
		a.cancel()
	}
	a.invalidate()
}

func (a *App) handlePaths(paths []string) {
	if len(paths) == 0 {
		return
	}
	if a.processing && !a.modeJobs {
		a.notice = "Busy: wait for the current operation to finish."
		a.invalidate()
		return
	}
	if a.modeJobs {
		a.addJob(paths)
		return
	}
	paths = dedupPaths(paths)
	if len(paths) == 1 {
		if info, err := os.Stat(paths[0]); err == nil && !info.IsDir() {
			if _, ok := hasher.FromExtension(filepath.Ext(paths[0])); ok {
				a.startRun(paths, filepath.Dir(paths[0]), true, paths[0])
				return
			}
		}
	}
	base := scanner.CommonBase(paths)
	a.startRun(paths, base, false, "")
}

func dedupPaths(paths []string) []string {
	seen := make(map[string]struct{}, len(paths))
	out := paths[:0]
	for _, p := range paths {
		clean := filepath.Clean(p)
		key := clean
		if runtime.GOOS == "windows" {
			key = strings.ToLower(clean)
		}
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		out = append(out, clean)
	}
	return out
}

func (a *App) clearList() {
	if a.processing {
		return
	}
	a.store = nil
	a.byID = nil
	a.viewRows = nil
	a.selection.Clear()
	a.selDirty = true
	a.comments = nil
	a.verifying = false
	a.legacy = false
	a.cancelled = false
	a.stats = runner.Progress{}
	a.elapsed = 0
	a.notice = ""
	a.clearActive()
	a.resetList()
	a.refreshColumns()
	a.updateTitle()
	a.updateProblemMenus()
	a.invalidate()
}

func (a *App) setNotice(s string) {
	a.notice = s
	a.invalidate()
}

func (a *App) highlight() string {
	if a.notice != "" {
		return a.notice
	}
	if a.modeJobs {
		_, _, active := jobCounts(a.jobs)
		return fmt.Sprintf("%d jobs · %d active", len(a.jobs), active)
	}
	if a.scanning {
		return fmt.Sprintf("Scanning... %d files", a.scanCount)
	}
	if a.processing {
		return fmt.Sprintf("Completed files: %d / %d", a.stats.Done, a.stats.Total)
	}
	if a.stats.Total > 0 {
		return fmt.Sprintf("Completed files: %d / %d", a.stats.Done, a.stats.Total)
	}
	return "Ready"
}

func (a *App) speedText() string {
	if a.stats.SpeedBPS <= 0 {
		return ""
	}
	s := fmt.Sprintf("Speed: %.1f MB/s", a.stats.SpeedBPS/(1024*1024))
	if a.stats.ETA > 0 {
		d := a.stats.ETA
		if d < time.Second {
			s += " | ETA: <1s"
		} else {
			m := int(d.Minutes())
			sec := int(d.Seconds()) % 60
			s += fmt.Sprintf(" | ETA: %02d:%02d", m, sec)
		}
	}
	return s
}

func (a *App) statusColor(c *ui.Context, s model.Status) ui.Color {
	t := c.Theme()
	switch s {
	case model.OK:
		return t.Success
	case model.Bad, model.Error:
		return t.Danger
	case model.Missing:
		return t.Warning
	case model.Pending:
		return t.Text
	default:
		return t.TextMuted
	}
}
