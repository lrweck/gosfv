package gui

import (
	"strings"
	"time"

	"github.com/egoist/mygo/ui"

	"gosfv/internal/hasher"
	"gosfv/internal/hexfmt"
	"gosfv/internal/model"
)

var (
	algorithmOptions = []string{"xxHash-3", "CRC-32", "MD5", "SHA-1", "SHA-256"}
	statusOptions    = []string{"All", "Waiting", "Pending", "OK", "BAD", "MISSING"}
)

func (a *App) view(c *ui.Context) {
	if a.closing.Load() {
		return
	}
	// Actions chosen from a context menu run here, before the table is built,
	// because they can remove the very row the loop is visiting.
	if len(a.pendingActions) > 0 {
		acts := a.pendingActions
		a.pendingActions = nil
		for _, fn := range acts {
			fn()
		}
	}
	if a.sortDirty {
		a.sortDirty = false
		if a.sort != a.appliedSort {
			a.applyView()
		}
	}
	if !a.filterAt.IsZero() && !time.Now().Before(a.filterAt) {
		a.filterAt = time.Time{}
		a.applyView()
	}

	t := c.Theme()
	root := ui.Column(c).Fill()
	if root.FileDragOver() {
		root.Border(2, t.Accent)
	}
	if files := root.DroppedFiles(); files != nil {
		a.handlePaths(files)
	}
	root.Children(func() {
		a.toolbar(c)
		if !a.modeJobs && a.settings.ShowFilter {
			a.filterBar(c)
		}
		if !a.modeJobs && a.settings.ShowAdvanced {
			a.advancedBar(c)
		}
		if a.verifying && len(a.comments) > 0 {
			a.commentsBar(c)
		}
		a.statsBar(c)
		if a.modeJobs {
			a.jobTable(c)
		} else {
			a.fileTable(c)
		}
		if !a.modeJobs && len(a.activeView) > 0 {
			a.activePanel(c)
		}
		a.progressBar(c)
	})
	if a.renameOpen {
		a.renameModal(c)
	}
}

func (a *App) toolbar(c *ui.Context) {
	ui.Toolbar(c, func() {
		if ui.Button(c, "Open...").Tooltip("Open a checksum file or files").Disabled(a.modeJobs).Clicked() {
			a.deferAction(a.openDialog)
		}
		if ui.Button(c, "Save As...").Tooltip("Write the current list as a checksum file").Disabled(a.processing || a.modeJobs || len(a.store) == 0 || a.createMode).Clicked() {
			a.deferAction(a.saveAs)
		}
		copyDisabled := a.selection.Len() == 0
		if a.modeJobs {
			copyDisabled = a.jobSel.Len() == 0
		}
		if ui.Button(c, "Copy").Tooltip("Copy the selected entries").Disabled(copyDisabled).Clicked() {
			a.copyEntries()
		}
		ui.Spacer(c)
		ui.Text(c, "Algorithm:").FontSize(12).TextColor(c.Theme().TextMuted)
		choice := a.algo.ShortLabel()
		sel := ui.Select(c, &choice, algorithmOptions).Width(128).Disabled(a.processing || a.verifying || a.modeJobs)
		if sel.Changed() {
			for i, label := range algorithmOptions {
				if label == choice {
					a.setAlgorithm(hasher.All[i])
					break
				}
			}
		}
		if a.processing {
			label := "Pause"
			if a.paused {
				label = "Resume"
			}
			if ui.Button(c, label).Width(86).Clicked() {
				a.togglePause()
			}
			if ui.Button(c, "Stop").Width(70).Clicked() {
				a.stopRun()
			}
		}
		if a.modeJobs {
			if ui.Button(c, "Clear Done").Width(96).Disabled(a.processing || !a.hasCompletedJobs()).Clicked() {
				a.clearCompletedJobs()
			}
		}
	})
}

func (a *App) filterBar(c *ui.Context) {
	t := c.Theme()
	ui.Row(c).Padding(6, 10).Gap(8).AlignItems(ui.Center).Children(func() {
		ui.Text(c, "Search:").FontSize(12).TextColor(t.TextMuted)
		if ui.SearchField(c, &a.filterText).Width(260).Changed() {
			a.filterAt = time.Now().Add(150 * time.Millisecond)
			c.After(150 * time.Millisecond)
		}
		choice := statusOptions[a.statusFilter]
		if ui.Select(c, &choice, statusOptions).Width(116).Changed() {
			for i, s := range statusOptions {
				if s == choice {
					a.statusFilter = i
					break
				}
			}
			a.applyView()
			a.updateProblemMenus()
			a.invalidate()
		}
		before := a.showDuplicates
		ui.Checkbox(c, &a.showDuplicates, "Show Duplicates")
		if a.showDuplicates != before {
			a.applyView()
			a.updateProblemMenus()
			a.invalidate()
		}
		ui.Spacer(c)
		if len(a.viewRows) != len(a.store) {
			ui.Textf(c, "Showing %d of %d", len(a.viewRows), len(a.store)).FontSize(12).TextColor(t.TextMuted)
		}
	})
}

func (a *App) advancedBar(c *ui.Context) {
	ui.Row(c).Padding(6, 10).Gap(8).AlignItems(ui.Center).Children(func() {
		ui.Text(c, "Prefix:").FontSize(12).TextColor(c.Theme().TextMuted)
		ui.TextInput(c, &a.settings.PathPrefix).Placeholder("incoming/iso").Width(150)
		ui.Text(c, "Include:").FontSize(12).TextColor(c.Theme().TextMuted)
		ui.TextInput(c, &a.settings.IncludePattern).Placeholder("*.iso;*.mkv").Width(140)
		ui.Text(c, "Exclude:").FontSize(12).TextColor(c.Theme().TextMuted)
		ui.TextInput(c, &a.settings.ExcludePattern).Placeholder("*.nfo").Width(120)
		ui.Checkbox(c, &a.settings.Recursive, "Subfolders")
		ui.Checkbox(c, &a.settings.Comments, "Save Comments")
	})
}

func (a *App) commentsBar(c *ui.Context) {
	text := strings.Join(a.comments, "   •   ")
	ui.Row(c).Padding(4, 10).Children(func() {
		ui.Text(c, "// "+text).FontSize(12).TextColor(c.Theme().TextMuted).SingleLine().Grow(1).Tooltip(strings.Join(a.comments, "\n"))
	})
}

func (a *App) statsBar(c *ui.Context) {
	t := c.Theme()
	ui.Row(c).Padding(4, 10).Gap(16).AlignItems(ui.Center).Children(func() {
		label := a.highlight()
		ui.Text(c, label).Bold().SingleLine().Grow(1)
		if a.settings.ShowThroughput && a.processing {
			if speed := a.speedText(); speed != "" {
				ui.Text(c, speed).FontSize(12).TextColor(t.TextMuted)
			}
		}
		if a.modeJobs {
			done, errs, active := jobCounts(a.jobs)
			ui.Textf(c, "DONE: %d", done).TextColor(t.Success).Bold()
			ui.Textf(c, "ERROR: %d", errs).TextColor(t.Danger).Bold()
			ui.Textf(c, "ACTIVE: %d", active).TextColor(t.Text).Bold()
		} else {
			ui.Textf(c, "OK: %d", a.stats.OK).TextColor(t.Success).Bold()
			ui.Textf(c, "BAD: %d", a.stats.Bad).TextColor(t.Danger).Bold()
			ui.Textf(c, "MISSING: %d", a.stats.Missing).TextColor(t.Warning).Bold()
			if a.stats.Error > 0 {
				ui.Textf(c, "ERROR: %d", a.stats.Error).TextColor(t.Danger).Bold()
			}
		}
		if a.settings.ShowTime && a.elapsed > 0 {
			ui.Textf(c, "Elapsed: %d ms", a.elapsed.Milliseconds()).TextColor(t.TextMuted).FontSize(12)
		}
		if a.legacy {
			ui.Text(c, "Legacy SFV (byte order)").TextColor(t.Warning).FontSize(12).Bold()
		}
	})
}

func (a *App) fileTable(c *ui.Context) {
	if len(a.store) == 0 {
		t := c.Theme()
		hint := ui.Column(c).Fill().Center().Gap(8).Padding(24)
		if hint.FileDragOver() {
			hint.Border(2, t.Accent)
		}
		hint.Children(func() {
			ui.Text(c, "Drop files or folders here").FontSize(20).Bold()
			ui.Text(c, "or drop a checksum file (.sfv, .md5, .sha1, .sha256, .xxh3) to verify it").TextColor(t.TextMuted)
			ui.Textf(c, "Algorithm: %s", a.algo.Label()).TextColor(t.TextMuted).FontSize(12)
		})
		if files := hint.DroppedFiles(); files != nil {
			a.handlePaths(files)
		}
		return
	}
	tbl := ui.Table(c, &a.list, a.cols, len(a.viewRows), func(row, col int) {
		a.fileCell(c, row, col)
	}).Grow(1)
	if tbl.Changed() {
		if a.sort != a.appliedSort {
			a.sortDirty = true
		}
		if !a.settings.LockColumns {
			a.settings.ColumnOrder = append([]string(nil), a.list.Columns.Order...)
			a.settings.ColumnWidths = make(map[string]float32, len(a.list.Columns.Widths))
			for k, v := range a.list.Columns.Widths {
				a.settings.ColumnWidths[k] = v
			}
		}
		c.Invalidate()
	}
	if tbl.Submitted() {
		a.deferAction(func() { a.openSelectedFolder() })
	}
}

func (a *App) fileCell(c *ui.Context, row, col int) {
	if row < 0 || row >= len(a.viewRows) || col < 0 || col >= len(a.cols) {
		return
	}
	f := &a.store[a.viewRows[row]]
	t := c.Theme()
	switch a.cols[col].ID {
	case "Name":
		text := f.Rel
		if a.settings.ShowFullPaths {
			text = f.FullPath()
		}
		ui.Text(c, text).SingleLine().TextColor(a.statusColor(c, f.Status)).Tooltip(f.FullPath())
	case "Hash":
		if f.Hash != nil {
			h := hexfmt.Upper(f.Hash)
			ui.Text(c, h).SingleLine().FontSize(11).Tooltip(h)
		} else if f.Status == model.Queued || f.Status == model.Pending {
			ui.Text(c, "Pending").TextColor(t.TextMuted)
		} else {
			ui.Text(c, "").TextColor(t.TextMuted)
		}
	case "Status":
		ui.Text(c, f.Status.String()).SingleLine().Bold().TextColor(a.statusColor(c, f.Status))
	case "Expected":
		if len(f.Expected) > 0 {
			h := hexfmt.Upper(f.Expected)
			ui.Text(c, h).SingleLine().FontSize(11).Tooltip(h)
		} else {
			ui.Text(c, "").TextColor(t.TextMuted)
		}
	case "Time":
		if f.Elapsed > 0 {
			ui.Textf(c, "%d ms", f.Elapsed.Milliseconds()).SingleLine().FontSize(11)
		} else {
			ui.Text(c, "")
		}
	}
}

func (a *App) progressBar(c *ui.Context) {
	t := c.Theme()
	frac := 0.0
	if a.modeJobs {
		for i := range a.jobs {
			if a.jobs[i].Status == model.JobInProgress || a.jobs[i].Status == model.JobPaused {
				frac = a.jobs[i].Progress / 100
				break
			}
		}
		if frac == 0 && len(a.jobs) > 0 {
			last := a.jobs[len(a.jobs)-1]
			if last.Status == model.JobDone || last.Status == model.JobError {
				frac = last.Progress / 100
			}
		}
	} else if a.stats.Total > 0 {
		frac = float64(a.stats.Done) / float64(a.stats.Total)
	}
	if frac < 0 {
		frac = 0
	}
	if frac > 1 {
		frac = 1
	}
	p := ui.Progress(c, frac).Height(6).Margin(10, 10, 12, 6).Label("Progress")
	switch {
	case a.paused:
		p.TextColor(t.Warning)
	case a.stats.Bad > 0 || a.stats.Missing > 0 || a.stats.Error > 0:
		p.TextColor(t.Danger)
	default:
		p.TextColor(t.Success)
	}
}

func (a *App) activePanel(c *ui.Context) {
	t := c.Theme()
	limit := len(a.activeView)
	if limit > 5 {
		limit = 5
	}
	ui.Column(c).Padding(4, 10).Gap(3).Children(func() {
		ui.Textf(c, "Active Jobs (%d)", len(a.activeView)).FontSize(12).Bold().TextColor(t.TextMuted)
		for _, f := range a.activeView[:limit] {
			ui.Row(c).Gap(8).AlignItems(ui.Center).Children(func() {
				ui.Text(c, f.Name).FontSize(12).SingleLine().Grow(1)
				pct := float64(0)
				if f.Total > 0 {
					pct = float64(f.Done) / float64(f.Total)
				}
				ui.Progress(c, pct).Width(150)
				ui.Textf(c, "%3d%%", int(pct*100)).FontSize(12).Width(48)
			})
		}
		if len(a.activeView) > limit {
			ui.Textf(c, "... and %d more", len(a.activeView)-limit).FontSize(12).TextColor(t.TextMuted)
		}
	})
}

func (a *App) jobTable(c *ui.Context) {
	if len(a.jobs) == 0 {
		t := c.Theme()
		ui.Column(c).Fill().Center().Gap(8).Children(func() {
			ui.Text(c, "Drop folders to queue jobs").FontSize(20).Bold()
			ui.Text(c, "Each job scans one folder and writes its checksum file beside it.").TextColor(t.TextMuted)
		})
		return
	}
	ui.Table(c, &a.jobList, a.jobCols, len(a.jobs), func(row, col int) {
		a.jobCell(c, row, col)
	}).Grow(1)
}

func (a *App) jobCell(c *ui.Context, row, col int) {
	if row < 0 || row >= len(a.jobs) || col < 0 || col >= len(a.jobCols) {
		return
	}
	j := &a.jobs[row]
	t := c.Theme()
	switch a.jobCols[col].ID {
	case "JobName":
		ui.Text(c, j.Name).SingleLine().Bold()
	case "RootPath":
		ui.Text(c, j.Root).SingleLine().TextColor(t.TextMuted).Tooltip(j.Root)
	case "Progress":
		ui.Textf(c, "%.1f%%", j.Progress).SingleLine()
	case "JobStatus":
		ui.Text(c, j.Status.String()).SingleLine().Bold().TextColor(jobColor(c, j.Status))
	case "JobTime":
		if j.Elapsed > 0 {
			ui.Textf(c, "%d ms", j.Elapsed.Milliseconds()).SingleLine()
		}
	}
}

func jobColor(c *ui.Context, s model.JobStatus) ui.Color {
	t := c.Theme()
	switch s {
	case model.JobDone:
		return t.Success
	case model.JobError:
		return t.Danger
	case model.JobInProgress:
		return t.Accent
	case model.JobPaused:
		return t.Warning
	default:
		return t.TextMuted
	}
}

func jobCounts(jobs []model.Job) (done, errs, active int) {
	for i := range jobs {
		switch jobs[i].Status {
		case model.JobDone:
			done++
		case model.JobError:
			errs++
		case model.JobScanning, model.JobInProgress, model.JobPaused:
			active++
		}
	}
	return
}

func (a *App) renameModal(c *ui.Context) {
	t := c.Theme()
	ui.Modal(c, &a.renameOpen, func() {
		ui.Column(c).Width(380).Padding(18).Gap(12).Background(t.Background).Border(1, t.Border).Radius(t.Radius).Children(func() {
			ui.Text(c, "Rename File").Bold().FontSize(15)
			ui.TextInput(c, &a.renameText).Width(344).Label("New name")
			ui.Row(c).Gap(8).Justify(ui.End).Children(func() {
				if ui.Button(c, "Cancel").Clicked() {
					a.renameOpen = false
				}
				if ui.PrimaryButton(c, "Rename").Clicked() || c.Shortcut(0, ui.KeyEnter) {
					a.commitRename()
				}
			})
		})
	})
}

func (a *App) createView(c *ui.Context) {
	if a.closing.Load() {
		return
	}
	if a.sortDirty {
		a.sortDirty = false
	}
	t := c.Theme()
	ui.Column(c).Fill().Padding(16).Gap(8).Children(func() {
		ui.Text(c, "Creating checksums").FontSize(16).Bold()
		ui.Text(c, a.autoSave).TextColor(t.TextMuted).FontSize(12).SingleLine().Tooltip(a.autoSave)
		ui.Text(c, a.highlight()).SingleLine()
		p := ui.Progress(c, a.progressFraction()).Height(8)
		if a.stats.Bad > 0 || a.stats.Error > 0 {
			p.TextColor(t.Danger)
		} else {
			p.TextColor(t.Success)
		}
		if len(a.activeView) > 0 {
			f := a.activeView[0]
			ui.Text(c, f.Name).FontSize(12).SingleLine().TextColor(t.TextMuted)
			pct := float64(0)
			if f.Total > 0 {
				pct = float64(f.Done) / float64(f.Total)
			}
			ui.Progress(c, pct).Height(5)
		}
		ui.Row(c).Gap(12).AlignItems(ui.Center).Children(func() {
			ui.Textf(c, "OK: %d", a.stats.OK).TextColor(t.Success).Bold()
			ui.Textf(c, "BAD: %d", a.stats.Bad).TextColor(t.Danger).Bold()
			ui.Textf(c, "MISSING: %d", a.stats.Missing).TextColor(t.Warning).Bold()
			if a.stats.Error > 0 {
				ui.Textf(c, "ERROR: %d", a.stats.Error).TextColor(t.Danger).Bold()
			}
			ui.Spacer(c)
			if a.processing {
				label := "Pause"
				if a.paused {
					label = "Resume"
				}
				if ui.Button(c, label).Width(86).Clicked() {
					a.togglePause()
				}
				if ui.Button(c, "Stop").Width(70).Clicked() {
					a.stopRun()
				}
			}
		})
	})
	if a.renameOpen {
		a.renameModal(c)
	}
}

func (a *App) progressFraction() float64 {
	if a.stats.Total <= 0 {
		return 0
	}
	p := float64(a.stats.Done) / float64(a.stats.Total)
	if p < 0 {
		return 0
	}
	if p > 1 {
		return 1
	}
	return p
}

func (a *App) deferAction(fn func()) {
	a.pendingActions = append(a.pendingActions, fn)
}
