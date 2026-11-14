package gui

import (
	"runtime"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"

	"github.com/lrweck/gosfv/internal/drive"
	"github.com/lrweck/gosfv/internal/hasher"
	"github.com/lrweck/gosfv/internal/runner"
	"github.com/lrweck/gosfv/internal/settings"
	"github.com/lrweck/gosfv/internal/shell"
)

// setMenuItemEnabled and setMenuItemChecked skip updates that change
// nothing. Every real update redraws the native menu bar, and the progress
// tick asks for these states several times a second; without the guard an
// unchanged menu bar repaints continuously and visibly flickers.
func setMenuItemEnabled(item *mygo.MenuItem, on bool) {
	if item != nil && item.IsEnabled() != on {
		item.SetEnabled(on)
	}
}

func setMenuItemChecked(item *mygo.MenuItem, on bool) {
	if item != nil && item.IsChecked() != on {
		item.SetChecked(on)
	}
}

func (a *App) buildMenu() {
	click := func(fn func()) func(*mygo.MenuItem, *mygo.Window) {
		return func(*mygo.MenuItem, *mygo.Window) { fn() }
	}

	file := &mygo.MenuItem{Label: "File", Submenu: []*mygo.MenuItem{
		{Label: "Open...", Accelerator: "CmdOrCtrl+O", Click: click(a.openDialog)},
		{Label: "Save As...", Accelerator: "CmdOrCtrl+S", Click: click(a.saveAs)},
		{Label: "Clear List", Click: click(a.clearList)},
		mygo.Separator(),
		{Role: mygo.RoleQuit},
	}}

	edit := &mygo.MenuItem{Label: "Edit", Submenu: []*mygo.MenuItem{
		{Label: "Copy Entry", Accelerator: "CmdOrCtrl+C", Click: click(a.copyEntries)},
		{Label: "Paste Paths", Accelerator: "CmdOrCtrl+V", Click: click(a.pastePaths)},
		{Label: "Compare with Clipboard", Click: click(a.compareClipboard)},
		mygo.Separator(),
		{Label: "Select All", Accelerator: "CmdOrCtrl+A", Click: click(a.selectAll)},
	}}

	a.miModeStandard = &mygo.MenuItem{
		ID: "mode-standard", Label: "Standard Mode", Type: mygo.MenuItemRadio, Checked: !a.modeJobs,
		Click: click(func() { a.setMode(false) }),
	}
	a.miModeJob = &mygo.MenuItem{
		ID: "mode-jobs", Label: "Job Queue Mode", Type: mygo.MenuItemRadio, Checked: a.modeJobs,
		Click: click(func() { a.setMode(true) }),
	}
	a.miClearJobs = &mygo.MenuItem{
		ID: "clear-jobs", Label: "Clear Completed Jobs", Click: click(a.clearCompletedJobs),
	}
	mode := &mygo.MenuItem{Label: "Mode", Submenu: []*mygo.MenuItem{
		a.miModeStandard,
		a.miModeJob,
		mygo.Separator(),
		a.miClearJobs,
		mygo.Separator(),
		{Label: "Pause / Resume Run", Accelerator: "F7", Click: click(a.togglePause)},
		{Label: "Stop Run", Accelerator: "F8", Click: click(a.stopRun)},
	}}

	a.miShowHash = &mygo.MenuItem{
		ID: "show-hash", Label: "Show Hash Column", Type: mygo.MenuItemCheckbox, Checked: a.settings.ShowHashCol,
		Click: func(item *mygo.MenuItem, _ *mygo.Window) {
			a.settings.ShowHashCol = item.IsChecked()
			a.refreshColumns()
			a.invalidate()
		},
	}
	a.miShowExpected = &mygo.MenuItem{
		ID: "show-expected", Label: "Show Expected Hash Column", Type: mygo.MenuItemCheckbox, Checked: a.settings.ShowExpectedCol,
		Click: func(item *mygo.MenuItem, _ *mygo.Window) {
			a.settings.ShowExpectedCol = item.IsChecked()
			a.refreshColumns()
			a.invalidate()
		},
	}
	a.miShowTime = &mygo.MenuItem{
		ID: "show-time", Label: "Show Time Elapsed", Type: mygo.MenuItemCheckbox, Checked: a.settings.ShowTime,
		Click: func(item *mygo.MenuItem, _ *mygo.Window) {
			a.settings.ShowTime = item.IsChecked()
			a.refreshColumns()
			a.invalidate()
		},
	}
	a.miThroughput = &mygo.MenuItem{
		ID: "show-throughput", Label: "Show Throughput and ETA", Type: mygo.MenuItemCheckbox, Checked: a.settings.ShowThroughput,
		Click: func(item *mygo.MenuItem, _ *mygo.Window) {
			a.settings.ShowThroughput = item.IsChecked()
			a.invalidate()
		},
	}
	a.miFullPaths = &mygo.MenuItem{
		ID: "show-full-paths", Label: "Show Full File Paths", Type: mygo.MenuItemCheckbox, Checked: a.settings.ShowFullPaths,
		Click: func(item *mygo.MenuItem, _ *mygo.Window) {
			a.settings.ShowFullPaths = item.IsChecked()
			a.invalidate()
		},
	}
	a.miLockColumns = &mygo.MenuItem{
		ID: "lock-columns", Label: "Lock Column Order", Type: mygo.MenuItemCheckbox, Checked: a.settings.LockColumns,
		Click: func(item *mygo.MenuItem, _ *mygo.Window) {
			a.settings.LockColumns = item.IsChecked()
		},
	}
	a.miFilter = &mygo.MenuItem{
		ID: "show-filter", Label: "Show Search/Filter Bar", Type: mygo.MenuItemCheckbox, Checked: a.settings.ShowFilter,
		Click: func(item *mygo.MenuItem, _ *mygo.Window) {
			a.settings.ShowFilter = item.IsChecked()
			a.invalidate()
		},
	}
	a.miAdvanced = &mygo.MenuItem{
		ID: "show-advanced", Label: "Show Advanced Options Bar", Type: mygo.MenuItemCheckbox, Checked: a.settings.ShowAdvanced,
		Click: func(item *mygo.MenuItem, _ *mygo.Window) {
			a.settings.ShowAdvanced = item.IsChecked()
			a.invalidate()
		},
	}
	a.miDetails = &mygo.MenuItem{
		ID: "show-details", Label: "Show Details Sidebar", Type: mygo.MenuItemCheckbox, Checked: a.settings.ShowDetails,
		Click: func(item *mygo.MenuItem, _ *mygo.Window) {
			a.settings.ShowDetails = item.IsChecked()
			a.invalidate()
		},
	}
	a.miThemeSystem = &mygo.MenuItem{
		ID: "theme-system", Label: "Follow System", Type: mygo.MenuItemRadio, Checked: a.settings.Theme == settings.ThemeSystem,
		Click: click(func() { a.setTheme(settings.ThemeSystem) }),
	}
	a.miThemeLight = &mygo.MenuItem{
		ID: "theme-light", Label: "Light", Type: mygo.MenuItemRadio, Checked: a.settings.Theme == settings.ThemeLight,
		Click: click(func() { a.setTheme(settings.ThemeLight) }),
	}
	a.miThemeDark = &mygo.MenuItem{
		ID: "theme-dark", Label: "Dark", Type: mygo.MenuItemRadio, Checked: a.settings.Theme == settings.ThemeDark,
		Click: click(func() { a.setTheme(settings.ThemeDark) }),
	}
	appearance := &mygo.MenuItem{Label: "Appearance", Submenu: []*mygo.MenuItem{
		a.miThemeSystem, a.miThemeLight, a.miThemeDark,
	}}

	view := &mygo.MenuItem{Label: "View", Submenu: []*mygo.MenuItem{
		appearance,
		mygo.Separator(),
		a.miShowHash,
		a.miShowExpected,
		a.miShowTime,
		a.miThroughput,
		a.miFullPaths,
		mygo.Separator(),
		a.miFilter,
		a.miDetails,
		a.miAdvanced,
		mygo.Separator(),
		a.miLockColumns,
		{Label: "Reset Column Layout", Click: click(a.resetColumns)},
	}}

	a.miPathRel = &mygo.MenuItem{
		ID: "path-relative", Label: "Relative Paths (Default)", Type: mygo.MenuItemRadio, Checked: a.settings.PathMode == settings.Relative,
		Click: click(func() { a.setPathMode(settings.Relative) }),
	}
	a.miPathAbs = &mygo.MenuItem{
		ID: "path-absolute", Label: "Absolute Paths", Type: mygo.MenuItemRadio, Checked: a.settings.PathMode == settings.Absolute,
		Click: click(func() { a.setPathMode(settings.Absolute) }),
	}
	pathStorage := &mygo.MenuItem{Label: "Path Storage", Submenu: []*mygo.MenuItem{a.miPathRel, a.miPathAbs}}

	a.miProcAuto = &mygo.MenuItem{
		ID: "proc-auto", Label: "Automatic Detection (Default)", Type: mygo.MenuItemRadio, Checked: a.settings.ProcMode == drive.Auto,
		Click: click(func() { a.setProcMode(drive.Auto) }),
	}
	a.miProcHDD = &mygo.MenuItem{
		ID: "proc-hdd", Label: "HDD Mode (Sequential)", Type: mygo.MenuItemRadio, Checked: a.settings.ProcMode == drive.HDD,
		Click: click(func() { a.setProcMode(drive.HDD) }),
	}
	a.miProcSSD = &mygo.MenuItem{
		ID: "proc-ssd", Label: "SSD Mode (Parallel)", Type: mygo.MenuItemRadio, Checked: a.settings.ProcMode == drive.SSD,
		Click: click(func() { a.setProcMode(drive.SSD) }),
	}
	procMode := &mygo.MenuItem{Label: "Processing Mode", Submenu: []*mygo.MenuItem{a.miProcAuto, a.miProcHDD, a.miProcSSD}}

	a.miAlgos = map[hasher.Kind]*mygo.MenuItem{}
	var algoItems []*mygo.MenuItem
	for _, k := range hasher.All {
		k := k
		item := &mygo.MenuItem{
			ID: "algo-" + k.String(), Label: k.Label(), Type: mygo.MenuItemRadio, Checked: k == a.algo,
			Click: click(func() { a.setAlgorithm(k) }),
		}
		a.miAlgos[k] = item
		algoItems = append(algoItems, item)
	}
	algorithm := &mygo.MenuItem{Label: "Default Hashing Algorithm", Submenu: algoItems}

	a.miShell = &mygo.MenuItem{
		ID: "shell-register", Label: "Register Explorer Context Menu", Disabled: runtime.GOOS != "windows",
		Click: click(a.registerShell),
	}
	a.miUnshell = &mygo.MenuItem{
		ID: "shell-unregister", Label: "Unregister Explorer Context Menu", Disabled: runtime.GOOS != "windows",
		Click: click(a.unregisterShell),
	}
	system := &mygo.MenuItem{Label: "System Integration", Submenu: []*mygo.MenuItem{a.miShell, a.miUnshell}}

	a.miBadMove = &mygo.MenuItem{Label: "Move Bad Files to '_BAD_FILES'...", Disabled: true, Click: click(a.moveBadFiles)}
	a.miBadRename = &mygo.MenuItem{Label: "Rename Bad Files (*.CORRUPT)...", Disabled: true, Click: click(a.renameBadFiles)}
	a.miBadScript = &mygo.MenuItem{Label: "Generate 'Delete BAD Files' Script", Disabled: true, Click: click(a.generateBadScript)}
	bad := &mygo.MenuItem{Label: "Bad File Tools", Submenu: []*mygo.MenuItem{
		a.miBadMove, a.miBadRename, mygo.Separator(), a.miBadScript,
	}}

	a.miDupCopy = &mygo.MenuItem{Label: "Generate Copy Duplicates Script", Disabled: true, Click: click(func() { a.generateDuplicateScript(true) })}
	a.miDupDelete = &mygo.MenuItem{Label: "Generate Delete Duplicates Script", Disabled: true, Click: click(func() { a.generateDuplicateScript(false) })}
	dups := &mygo.MenuItem{Label: "Duplicate Tools", Submenu: []*mygo.MenuItem{a.miDupCopy, a.miDupDelete}}

	options := &mygo.MenuItem{Label: "Options", Submenu: []*mygo.MenuItem{
		pathStorage,
		procMode,
		algorithm,
		mygo.Separator(),
		system,
		bad,
		dups,
	}}

	help := &mygo.MenuItem{Label: "Help", Submenu: []*mygo.MenuItem{
		{Label: "Disk and Backend Info", Click: click(a.showDiskInfo)},
		{Label: "Open Project Page", Click: click(func() {
			_ = mygo.Shell.OpenExternal("https://github.com/lrweck/gosfv")
		})},
		mygo.Separator(),
		{Label: "Reset to Default Values", Click: click(a.resetDefaults)},
		mygo.Separator(),
		{Label: "About GoSFV", Click: click(a.showCredits)},
	}}

	menu := mygo.NewMenu([]*mygo.MenuItem{
		{Role: mygo.RoleAppMenu},
		file,
		edit,
		mode,
		view,
		options,
		help,
		{Role: mygo.RoleWindowMenu},
	})
	mygo.App.SetMenu(menu)
	a.updateProblemMenus()
	a.syncShellMenu()
}

func (a *App) setMode(jobs bool) {
	if a.processing || a.jobRunning || a.modeJobs == jobs {
		a.syncViewMenu()
		return
	}
	a.modeJobs = jobs
	a.selDirty = true
	setMenuItemChecked(a.miModeStandard, !jobs)
	setMenuItemChecked(a.miModeJob, jobs)
	if jobs {
		a.resetJobList()
	} else {
		a.resetList()
		a.applyView()
	}
	a.refreshColumns()
	a.updateTitle()
	a.updateProblemMenus()
	a.invalidate()
}

func (a *App) setAlgorithm(k hasher.Kind) {
	if a.processing {
		a.syncAlgoMenu()
		return
	}
	a.settings.DefaultAlgo = k
	if a.verifying && len(a.store) > 0 {
		// The open checksum fixes the algorithm of the current list.
		a.syncAlgoMenu()
		return
	}
	if a.algo != k && len(a.store) > 0 {
		a.store = nil
		a.byID = nil
		a.viewRows = nil
		a.selection.Clear()
		a.selDirty = true
		a.jobSel.Clear()
		a.comments = nil
		a.elapsed = 0
		a.verifying = false
		a.stats = runner.Progress{}
		a.resetList()
	}
	a.algo = k
	a.syncAlgoMenu()
	a.refreshColumns()
	a.updateTitle()
	a.updateProblemMenus()
	a.invalidate()
}

func (a *App) syncAlgoMenu() {
	enabled := !a.processing && !(a.verifying && len(a.store) > 0)
	for k, item := range a.miAlgos {
		setMenuItemChecked(item, k == a.algo)
		setMenuItemEnabled(item, enabled)
	}
}

func (a *App) setProcMode(m drive.Mode) {
	a.settings.ProcMode = m
	setMenuItemChecked(a.miProcAuto, m == drive.Auto)
	setMenuItemChecked(a.miProcHDD, m == drive.HDD)
	setMenuItemChecked(a.miProcSSD, m == drive.SSD)
}

func (a *App) setPathMode(m settings.PathMode) {
	a.settings.PathMode = m
	setMenuItemChecked(a.miPathRel, m == settings.Relative)
	setMenuItemChecked(a.miPathAbs, m == settings.Absolute)
}

func (a *App) setTheme(m settings.ThemeMode) {
	a.settings.Theme = m
	mygo.Theme.SetSource(themeSource(m))
	if a.win != nil && !a.win.IsDestroyed() {
		a.win.SetWindowButtonsTheme(themeSource(m))
	}
	for item, mode := range map[*mygo.MenuItem]settings.ThemeMode{
		a.miThemeSystem: settings.ThemeSystem,
		a.miThemeLight:  settings.ThemeLight,
		a.miThemeDark:   settings.ThemeDark,
	} {
		setMenuItemChecked(item, mode == m)
	}
	a.invalidate()
}

func (a *App) resetColumns() {
	a.settings.ColumnOrder = nil
	a.settings.ColumnWidths = map[string]float32{}
	a.list.Columns = ui.TableLayout{}
	a.invalidate()
}

func (a *App) syncShellMenu() {
	registered := shell.Registered()
	setMenuItemEnabled(a.miShell, !registered)
	setMenuItemEnabled(a.miUnshell, registered)
}

func (a *App) syncViewMenu() {
	setMenuItemChecked(a.miShowHash, a.settings.ShowHashCol)
	setMenuItemChecked(a.miShowExpected, a.settings.ShowExpectedCol)
	setMenuItemChecked(a.miShowTime, a.settings.ShowTime)
	setMenuItemChecked(a.miThroughput, a.settings.ShowThroughput)
	setMenuItemChecked(a.miFullPaths, a.settings.ShowFullPaths)
	setMenuItemChecked(a.miLockColumns, a.settings.LockColumns)
	setMenuItemChecked(a.miFilter, a.settings.ShowFilter)
	setMenuItemChecked(a.miDetails, a.settings.ShowDetails)
	setMenuItemChecked(a.miAdvanced, a.settings.ShowAdvanced)
	setMenuItemChecked(a.miModeStandard, !a.modeJobs)
	setMenuItemChecked(a.miModeJob, a.modeJobs)
	setMenuItemChecked(a.miThemeSystem, a.settings.Theme == settings.ThemeSystem)
	setMenuItemChecked(a.miThemeLight, a.settings.Theme == settings.ThemeLight)
	setMenuItemChecked(a.miThemeDark, a.settings.Theme == settings.ThemeDark)
	enabled := !a.processing && !a.jobRunning
	setMenuItemEnabled(a.miModeStandard, enabled)
	setMenuItemEnabled(a.miModeJob, enabled)
}
