package gui

import (
	"strings"
	"time"

	"github.com/egoist/mygo/ui"

	"github.com/lrweck/gosfv/internal/hasher"
	"github.com/lrweck/gosfv/internal/hexfmt"
	"github.com/lrweck/gosfv/internal/model"
	"github.com/lrweck/gosfv/internal/settings"
)

var (
	algorithmOptions = []string{"xxHash-3", "CRC-32", "MD5", "SHA-1", "SHA-256"}
	statusOptions    = []string{"All", "Waiting", "Pending", "OK", "BAD", "MISSING"}
	themeOptions     = []string{"System", "Light", "Dark"}
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
	a.rowClicked = false
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

	// A hidden sidebar does not start or keep a details job.
	if !a.settings.ShowDetails || a.modeJobs {
		if a.sel.Count != 0 || a.sel.Loading {
			a.cancelSelection()
			a.sel = selectionInfo{}
			a.selSig = selectionSig{}
		}
		a.selDirty = false
	}

	a.applyPalette(c)
	t := c.Theme()
	root := ui.Column(c).Fill()
	if root.FileDragOver() {
		root.Border(2, t.Accent)
	}
	if files := root.DroppedFiles(); files != nil {
		a.handlePaths(files)
	}
	root.Children(func() {
		a.header(c)
		a.statusBar(c)
		// The table comes first in the build so the sidebar sees the row
		// this frame's click chose; Reverse keeps the sidebar on the left.
		ui.Row(c).FillWidth().Grow(1).Shrink(1).MinHeight(0).AlignItems(ui.Stretch).Reverse().Children(func() {
			ui.Column(c).FillHeight().Grow(1).MinWidth(0).Children(func() {
				if !a.modeJobs && a.settings.ShowFilter {
					a.filterBar(c)
				}
				if !a.modeJobs && a.settings.ShowAdvanced {
					a.advancedBar(c)
				}
				if a.verifying && len(a.comments) > 0 {
					a.commentsBar(c)
				}
				if a.modeJobs {
					a.jobTable(c)
				} else {
					a.fileTable(c)
				}
				if !a.modeJobs && len(a.activeView) > 0 {
					a.activePanel(c)
				}
			})
			if a.settings.ShowDetails && !a.modeJobs {
				a.detailsPanel(c)
			}
		})
		a.footer(c)
	})
	if a.renameOpen {
		a.renameModal(c)
	}
}

// header is the application bar: identity and context on the left, search,
// mode and the three actions that act on the whole list on the right.
func (a *App) header(c *ui.Context) {
	t := c.Theme()
	ui.Column(c).Background(t.Surface).Children(func() {
		ui.Row(c).Padding(10, 16).Gap(12).AlignItems(ui.Center).Children(func() {
			ui.Row(c).Width(30).Height(30).Center().Radius(8).Background(t.Accent).Shrink(0).Children(func() {
				ui.Text(c, "G").FontSize(15).Bold().TextColor(t.AccentText)
			})
			ui.Column(c).Grow(1).Gap(2).Children(func() {
				ui.Text(c, a.sectionTitle()).FontSize(15).Bold().SingleLine()
				ui.Text(c, a.highlight()).FontSize(12).TextColor(t.TextMuted).SingleLine()
			})
			if !a.modeJobs && len(a.store) > 0 {
				if ui.SearchField(c, &a.filterText).Width(280).Placeholder("Search files").Changed() {
					a.filterAt = time.Now().Add(150 * time.Millisecond)
					c.After(150 * time.Millisecond)
				}
			}
			mode := 0
			if a.modeJobs {
				mode = 1
			}
			if ui.Segmented(c, &mode, "Files", "Jobs").Label("Mode").Changed() {
				a.deferAction(func() { a.setMode(mode == 1) })
			}
			if ui.PrimaryButton(c, "Open...").Tooltip("Open a checksum file or files").Disabled(a.modeJobs).Clicked() {
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
		})
		ui.Divider(c)
	})
}

// statusBar gathers the counts that used to be a second toolbar row into
// compact chips, with the two controls that change the whole list beside
// them.
func (a *App) statusBar(c *ui.Context) {
	t := c.Theme()
	ui.Row(c).Padding(6, 16).Gap(8).AlignItems(ui.Center).Children(func() {
		if a.modeJobs {
			done, errs, active := jobCounts(a.jobs)
			a.statChip(c, "DONE", done, t.Success)
			a.statChip(c, "ERROR", errs, t.Danger)
			a.statChip(c, "ACTIVE", active, t.Accent)
		} else {
			a.statChip(c, "OK", a.stats.OK, t.Success)
			a.statChip(c, "BAD", a.stats.Bad, t.Danger)
			a.statChip(c, "MISSING", a.stats.Missing, t.Warning)
			if a.stats.Error > 0 {
				a.statChip(c, "ERROR", a.stats.Error, t.Danger)
			}
			if a.processing || a.jobRunning {
				a.statChip(c, "ACTIVE", len(a.activeView), t.Accent)
			}
		}
		if a.settings.ShowThroughput && a.processing {
			if speed := a.speedText(); speed != "" {
				ui.Text(c, speed).FontSize(11).TextColor(t.TextMuted).SingleLine()
			}
		}
		if a.settings.ShowTime && a.elapsed > 0 {
			ui.Textf(c, "Elapsed %d ms", a.elapsed.Milliseconds()).FontSize(11).TextColor(t.TextMuted)
		}
		if a.legacy {
			ui.Text(c, "Legacy SFV byte order").FontSize(11).Bold().TextColor(t.Warning)
		}
		ui.Spacer(c)
		// The Recheck buttons keep their slots while a run is in progress,
		// disabled: the Pause and Stop controls live in the footer, so a
		// second click on Recheck cannot land on one of them.
		if !a.modeJobs && len(a.store) > 0 {
			label := "Recheck All"
			if a.selection.Len() > 0 {
				label = "Recheck Selected"
			}
			tip := "Verify the chosen rows again, or the whole list"
			if a.processing {
				tip = "Wait for the current run to finish"
			}
			if ui.Button(c, label).Tooltip(tip).Disabled(a.processing).Clicked() {
				if a.selection.Len() > 0 {
					a.recheckSelected()
				} else {
					a.recheckAll()
				}
			}
			if a.stats.Bad+a.stats.Missing+a.stats.Error > 0 {
				if ui.Button(c, "Recheck Failed").Tooltip("Recheck only BAD, MISSING and ERROR rows").Disabled(a.processing).Clicked() {
					a.recheckFailed()
				}
			}
		}
		ui.Text(c, "Hash").FontSize(11).TextColor(t.TextMuted)
		choice := a.algo.ShortLabel()
		sel := ui.Select(c, &choice, algorithmOptions).Width(120).Disabled(a.processing || a.verifying || a.modeJobs)
		if sel.Changed() {
			for i, label := range algorithmOptions {
				if label == choice {
					a.setAlgorithm(hasher.All[i])
					break
				}
			}
		}
		ui.Text(c, "Theme").FontSize(11).TextColor(t.TextMuted)
		themeChoice := a.settings.Theme.String()
		if ui.Select(c, &themeChoice, themeOptions).Width(96).Changed() {
			a.setTheme(themeMode(themeChoice))
		}
	})
}

func (a *App) statChip(c *ui.Context, label string, n int, color ui.Color) {
	t := c.Theme()
	ui.Row(c).AlignItems(ui.Center).Gap(5).Padding(2, 8).Radius(999).Shrink(0).
		Background(color.Alpha(0.13)).Children(func() {
		ui.Text(c, label).FontSize(10).Bold().TextColor(color).SingleLine()
		ui.Textf(c, "%d", n).FontSize(12).Bold().TextColor(t.Text)
	})
}

func themeMode(label string) settings.ThemeMode {
	switch label {
	case "Light":
		return settings.ThemeLight
	case "Dark":
		return settings.ThemeDark
	default:
		return settings.ThemeSystem
	}
}

func (a *App) sectionTitle() string {
	if a.modeJobs {
		return "Job Queue"
	}
	if a.rechecking {
		return "Rechecking"
	}
	if a.verifying {
		return "Verifying"
	}
	if a.processing {
		return "Creating"
	}
	return "Files"
}

func (a *App) filterBar(c *ui.Context) {
	t := c.Theme()
	ui.Row(c).Padding(4, 16).Gap(8).AlignItems(ui.Center).Children(func() {
		ui.Text(c, "Status").FontSize(11).TextColor(t.TextMuted)
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
		ui.Checkbox(c, &a.showDuplicates, "Duplicates only")
		if a.showDuplicates != before {
			a.applyView()
			a.updateProblemMenus()
			a.invalidate()
		}
		ui.Spacer(c)
		if len(a.viewRows) != len(a.store) {
			ui.Textf(c, "Showing %d of %d", len(a.viewRows), len(a.store)).FontSize(11).TextColor(t.TextMuted)
		}
	})
}

func (a *App) advancedBar(c *ui.Context) {
	ui.Row(c).Padding(4, 16).Gap(8).AlignItems(ui.Center).Children(func() {
		ui.Text(c, "Prefix").FontSize(11).TextColor(c.Theme().TextMuted)
		ui.TextInput(c, &a.settings.PathPrefix).Placeholder("incoming/iso").Width(150)
		ui.Text(c, "Include").FontSize(11).TextColor(c.Theme().TextMuted)
		ui.TextInput(c, &a.settings.IncludePattern).Placeholder("*.iso;*.mkv").Width(140)
		ui.Text(c, "Exclude").FontSize(11).TextColor(c.Theme().TextMuted)
		ui.TextInput(c, &a.settings.ExcludePattern).Placeholder("*.nfo").Width(120)
		ui.Checkbox(c, &a.settings.Recursive, "Subfolders")
		ui.Checkbox(c, &a.settings.Comments, "Save comments")
	})
}

func (a *App) commentsBar(c *ui.Context) {
	text := strings.Join(a.comments, "   •   ")
	ui.Row(c).Padding(2, 16).Children(func() {
		ui.Text(c, "// "+text).FontSize(11).TextColor(c.Theme().TextMuted).SingleLine().Grow(1).Tooltip(strings.Join(a.comments, "\n"))
	})
}

func (a *App) fileTable(c *ui.Context) {
	t := c.Theme()
	card := ui.Column(c).FillWidth().Grow(1).Shrink(1).MinHeight(0).Margin(10, 16, 0, 16).Radius(10).Border(1, t.Border).Clip()
	if len(a.store) == 0 {
		card.Background(t.Surface).Children(func() {
			ui.Column(c).Fill().Center().Gap(10).Padding(32).Children(func() {
				ui.Text(c, "Drop files or folders").FontSize(22).Bold()
				ui.Text(c, "or drop a checksum file (.sfv, .md5, .sha1, .sha256, .xxh3) to verify it").TextColor(t.TextMuted)
				ui.Row(c).Gap(12).AlignItems(ui.Center).Children(func() {
					if ui.PrimaryButton(c, "Open...").Clicked() {
						a.deferAction(a.openDialog)
					}
					ui.Textf(c, "Algorithm: %s", a.algo.Label()).TextColor(t.TextMuted).FontSize(11)
				})
			})
		})
		if files := card.DroppedFiles(); files != nil {
			a.handlePaths(files)
		}
		return
	}
	card.Children(func() {
		selBefore := a.selection.Len()
		activeBefore := a.activeRow
		tbl := ui.Table(c, &a.list, a.cols, len(a.viewRows), func(row, col int) {
			a.fileCell(c, row, col)
		}).Grow(1)
		changed := tbl.Changed()
		if changed {
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
		// A press on the table but not on a row clears the choice. A row
		// click sets rowClicked through RowClicked in fileCell, but that
		// reports only clicks without modifiers, and a Ctrl- or Shift-click
		// toggles or extends the choice on release: clearing here would
		// empty the selection before it does. Leave modified presses to the
		// table.
		if tbl.PressedInside() && !a.rowClicked && !changed &&
			c.Modifiers()&(ui.Cmd|ui.Shift) == 0 &&
			a.selection.Len() == selBefore && a.activeRow == activeBefore {
			a.selection.Clear()
			a.activeRow = -1
			a.selDirty = true
		}
		if tbl.Submitted() {
			a.deferAction(func() { a.openSelectedFolder() })
		}
	})
}

func (a *App) fileCell(c *ui.Context, row, col int) {
	if row < 0 || row >= len(a.viewRows) || col < 0 || col >= len(a.cols) {
		return
	}
	idx := a.viewRows[row]
	f := &a.store[idx]
	t := c.Theme()
	chosen := a.selection.Has(f.ID)
	textColor := t.Text
	mutedColor := t.TextMuted
	if chosen {
		// The row is painted with the accent, so every cell has to use the
		// text color meant for it.
		textColor = t.AccentText
		mutedColor = t.AccentText
	}
	cell := ui.Box(c).FillWidth()
	if cell.RowClicked() {
		a.rowClicked = true
	}
	cell.Children(func() {
		switch a.cols[col].ID {
		case "Name":
			text := f.Rel
			if a.settings.ShowFullPaths {
				text = f.FullPath()
			}
			color := a.statusColor(c, f.Status)
			if chosen {
				color = t.AccentText
			}
			ui.Text(c, text).SingleLine().TextColor(color).Tooltip(f.FullPath())
		case "Size":
			size := "—"
			switch {
			case f.Size > 0:
				size = formatBytes(f.Size)
			case f.Status == model.OK || f.Status == model.Bad:
				size = "0 B"
			}
			ui.Text(c, size).SingleLine().FontSize(11).TextColor(textColor)
		case "Progress":
			if done, total, ok := a.progressOfFile(idx); ok && total > 0 {
				pct := float64(done) / float64(total) * 100
				if pct > 100 {
					pct = 100
				}
				color := t.Accent
				if chosen {
					color = t.AccentText
				}
				ui.Textf(c, "%.1f%%", pct).SingleLine().FontSize(11).Bold().TextColor(color)
			} else if f.Status == model.OK || f.Status == model.Bad {
				ui.Text(c, "100.0%").SingleLine().FontSize(11).TextColor(mutedColor)
			} else {
				ui.Text(c, "—").SingleLine().TextColor(mutedColor)
			}
		case "Hash":
			if f.Hash != nil {
				h := hexfmt.Upper(f.Hash)
				ui.Text(c, h).SingleLine().FontSize(11).TextColor(textColor).Tooltip(h)
			} else if f.Status == model.Queued || f.Status == model.Pending {
				ui.Text(c, "Pending").TextColor(mutedColor)
			} else {
				ui.Text(c, "").TextColor(mutedColor)
			}
		case "Status":
			if _, _, active := a.progressOfFile(idx); active {
				color := t.Accent
				if chosen {
					color = t.AccentText
				}
				ui.Text(c, "PROCESSING").SingleLine().Bold().TextColor(color)
			} else {
				color := a.statusColor(c, f.Status)
				if chosen {
					color = t.AccentText
				}
				status := ui.Text(c, f.Status.String()).SingleLine().Bold().TextColor(color)
				if f.Err != "" {
					status.Tooltip(f.Err)
				}
			}
		case "Expected":
			if len(f.Expected) > 0 {
				h := hexfmt.Upper(f.Expected)
				ui.Text(c, h).SingleLine().FontSize(11).TextColor(textColor).Tooltip(h)
			} else {
				ui.Text(c, "").TextColor(mutedColor)
			}
		case "Time":
			if f.Elapsed > 0 {
				ui.Textf(c, "%d ms", f.Elapsed.Milliseconds()).SingleLine().FontSize(11).TextColor(textColor)
			} else {
				ui.Text(c, "")
			}
		}
	})
}

// footer holds the only full-width progress: the file list, the job queue
// and the active panel above it all feed the same fraction.
func (a *App) footer(c *ui.Context) {
	t := c.Theme()
	frac := a.currentFraction()
	failed := a.stats.Bad > 0 || a.stats.Missing > 0 || a.stats.Error > 0
	if a.modeJobs {
		_, errs, _ := jobCounts(a.jobs)
		failed = errs > 0
	}
	stateColor := t.TextMuted
	switch {
	case a.paused:
		stateColor = t.Warning
	case a.processing || a.jobRunning:
		stateColor = t.Text
	}
	ui.Column(c).Shrink(0).Background(t.Surface).Children(func() {
		ui.Divider(c)
		ui.Row(c).Padding(6, 16).Gap(12).AlignItems(ui.Center).Children(func() {
			ui.Text(c, a.footerState()).FontSize(11).Bold().TextColor(stateColor).Shrink(0)
			p := ui.Progress(c, frac).Height(6).Grow(1)
			switch {
			case a.paused:
				p.TextColor(t.Warning)
			case failed:
				p.TextColor(t.Danger)
			default:
				p.TextColor(t.Success)
			}
			ui.Textf(c, "%.1f%%", frac*100).FontSize(11).TextColor(t.TextMuted).Width(48)
			if a.processing && a.settings.ShowThroughput {
				if speed := a.speedText(); speed != "" {
					ui.Text(c, speed).FontSize(11).TextColor(t.TextMuted).SingleLine()
				}
			}
			if a.processing {
				label := "Pause"
				if a.paused {
					label = "Resume"
				}
				if ui.Button(c, label).Width(86).Tooltip("Pause or resume the current run").Clicked() {
					a.togglePause()
				}
				if ui.Button(c, "Stop").Width(70).Tooltip("Stop the current run; unfinished files stay waiting").TextColor(t.Danger).Clicked() {
					a.stopRun()
				}
			}
		})
	})
}

// footerState names what the run is doing in one word, so the footer is not
// just a percentage.
func (a *App) footerState() string {
	switch {
	case a.paused:
		return "Paused"
	case a.scanning:
		return "Scanning"
	case a.rechecking && a.verifying:
		return "Rechecking"
	case a.jobRunning:
		return "Job queue"
	case a.processing && a.verifying:
		return "Processing"
	case a.processing:
		return "Creating"
	case a.cancelled:
		return "Stopped"
	case a.stats.Total > 0 && a.stats.Done >= a.stats.Total:
		return "Done"
	default:
		return "Ready"
	}
}

func (a *App) currentFraction() float64 {
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
	} else {
		frac = a.progressFraction()
	}
	if frac < 0 {
		return 0
	}
	if frac > 1 {
		return 1
	}
	return frac
}

func (a *App) activePanel(c *ui.Context) {
	t := c.Theme()
	limit := len(a.activeView)
	if limit > 5 {
		limit = 5
	}
	ui.Column(c).Padding(4, 16).Gap(3).Children(func() {
		ui.Textf(c, "Active files (%d)", len(a.activeView)).FontSize(11).Bold().TextColor(t.TextMuted)
		for _, f := range a.activeView[:limit] {
			ui.Row(c).Gap(8).AlignItems(ui.Center).Children(func() {
				ui.Text(c, f.Name).FontSize(11).SingleLine().Grow(1)
				pct := float64(0)
				if f.Total > 0 {
					pct = float64(f.Done) / float64(f.Total)
				}
				ui.Progress(c, pct).Width(150)
				ui.Textf(c, "%.1f%%", pct*100).FontSize(11).Width(56)
			})
		}
		if len(a.activeView) > limit {
			ui.Textf(c, "... and %d more", len(a.activeView)-limit).FontSize(11).TextColor(t.TextMuted)
		}
	})
}

func (a *App) jobTable(c *ui.Context) {
	t := c.Theme()
	card := ui.Column(c).FillWidth().Grow(1).Shrink(1).MinHeight(0).Margin(10, 16, 0, 16).Radius(10).Border(1, t.Border).Clip()
	if len(a.jobs) == 0 {
		card.Background(t.Surface).Children(func() {
			ui.Column(c).Fill().Center().Gap(10).Padding(32).Children(func() {
				ui.Text(c, "Drop folders to queue jobs").FontSize(22).Bold()
				ui.Text(c, "Each job scans one folder and writes its checksum file beside it.").TextColor(t.TextMuted)
			})
		})
		return
	}
	card.Children(func() {
		ui.Table(c, &a.jobList, a.jobCols, len(a.jobs), func(row, col int) {
			a.jobCell(c, row, col)
		}).Grow(1)
	})
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
	a.applyPalette(c)
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
	// While a run is in progress the bar follows bytes, not finished files:
	// one 18 GiB file would otherwise leave it still until the whole file
	// ended. When every file has been applied, the count is authoritative
	// (some tasks can end with errors after reading only part of the file).
	var p float64
	if a.stats.BytesTotal > 0 && a.stats.Done < a.stats.Total {
		p = float64(a.stats.BytesDone) / float64(a.stats.BytesTotal)
	} else {
		p = float64(a.stats.Done) / float64(a.stats.Total)
	}
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
