package gui

import (
	"fmt"
	"strconv"
	"time"

	"github.com/egoist/mygo/ui"
)

// detailsPanel is the sidebar for the current choice. It reads the
// snapshot that refreshSelection published; it never touches the file
// system, so opening a file, selecting a thousand or resizing the window
// all stay on the main thread for a bounded, tiny amount of work.
func (a *App) detailsPanel(c *ui.Context) {
	// Built after the table, so a click this frame is already in the
	// selection; there is no frame that shows the previous choice.
	a.refreshSelection(a.selDirty)
	a.selDirty = false
	t := c.Theme()
	side := ui.Column(c).Width(300).Shrink(0).Background(t.Surface)
	side.Children(func() {
		ui.Scroll(c).Fill().Padding(16).Gap(10).Children(func() {
			ui.Text(c, "Details").FontSize(11).Bold().TextColor(t.TextMuted).SingleLine()
			switch {
			case a.sel.TooMany:
				ui.Textf(c, "%d files", a.sel.Count).FontSize(22).Bold()
				ui.Textf(c, "A detailed summary is skipped above %d files. Select fewer files to inspect them.", selectionDetailLimit).
					FontSize(11).TextColor(t.TextMuted)
			case a.sel.HasSingle:
				a.singleDetails(c, &a.sel.Single)
			case a.sel.Found > 0:
				a.multiDetails(c, &a.sel)
			case a.sel.Count > 0:
				ui.Text(c, "The selected items are no longer in the list.").FontSize(12).TextColor(t.TextMuted)
			default:
				a.emptyDetails(c)
			}
			// The progress area keeps its height whether or not it shows:
			// a bar that appeared and vanished between two frames is what
			// reads as a flicker when moving through rows.
			if a.sel.Loading && a.sel.Count == 1 {
				// A stat is normally instant. Ask for a later frame so a
				// slow one can still show the indicator without flashing
				// it on every ordinary click.
				c.After(selectionLoadingDelay + 10*time.Millisecond)
			}
			ui.Column(c).Height(22).Children(func() {
				if a.showSelectionLoading(time.Now()) {
					ui.Progress(c, a.sel.Progress).Height(3)
					ui.Text(c, "Reading details...").FontSize(10).TextColor(t.TextMuted).SingleLine()
				}
			})
		})
	})
}

// emptyDetails deliberately has the same blocks and row count as
// singleDetails: selecting the first file fills values in instead of
// replacing a short message with a taller panel.
// selectionLoadingDelay hides the progress bar for the quick single-file
// stat. A multi-selection shows it at once; a stat that really is slow
// shows it after the delay.
const selectionLoadingDelay = 120 * time.Millisecond

func (a *App) showSelectionLoading(now time.Time) bool {
	if !a.sel.Loading {
		return false
	}
	return a.sel.Count > 1 || now.Sub(a.selAt) >= selectionLoadingDelay
}

func (a *App) emptyDetails(c *ui.Context) {
	t := c.Theme()
	ui.Row(c).AlignItems(ui.Center).Gap(6).Children(func() {
		ui.Row(c).Padding(2, 8).Radius(999).Background(t.SurfacePressed).Shrink(0).Children(func() {
			ui.Text(c, "—").FontSize(10).Bold().TextColor(t.TextMuted).SingleLine()
		})
		ui.Text(c, "No selection").FontSize(10).TextColor(t.TextMuted)
	})
	ui.Text(c, "No file selected").FontSize(15).Bold().SingleLine()
	ui.Text(c, "Click a file to inspect it.").FontSize(10).TextColor(t.TextMuted).SingleLine()
	ui.Divider(c)
	a.detailField(c, "Size", "—")
	a.detailField(c, "Modified", "—")
	a.detailField(c, "Elapsed", "—")
	a.detailField(c, "Folder", "—")
	ui.Divider(c)
	a.hashField(c, "Hash", "—")
	a.hashField(c, "Expected", "—")
}

func (a *App) singleDetails(c *ui.Context, d *fileDetail) {
	t := c.Theme()
	color := a.statusColor(c, d.Status)
	secondary := "reading…"
	secondaryColor := t.TextMuted
	switch {
	case d.StatOK:
		secondary = "on disk"
	case d.StatErr != "":
		secondary = "unavailable"
		secondaryColor = t.Warning
	}
	ui.Row(c).AlignItems(ui.Center).Gap(6).Children(func() {
		ui.Row(c).Padding(2, 8).Radius(999).Background(color.Alpha(0.13)).Shrink(0).Children(func() {
			ui.Text(c, d.Status.String()).FontSize(10).Bold().TextColor(color).SingleLine()
		})
		tip := d.StatErr
		if tip == "" {
			tip = secondary
		}
		ui.Text(c, secondary).FontSize(10).TextColor(secondaryColor).SingleLine().Tooltip(tip)
	})
	ui.Text(c, d.Name).FontSize(15).Bold().SingleLine().Tooltip(d.Name)
	ui.Text(c, d.Path).FontSize(10).TextColor(t.TextMuted).SingleLine().Tooltip(d.Path)
	ui.Divider(c)
	a.detailField(c, "Size", formatBytes(d.Size))
	// Always present: the field goes from "—" to the date when the stat
	// answers, instead of appearing and moving the rows below it.
	modified := "—"
	if d.StatOK {
		modified = d.ModTime.Format("2006-01-02 15:04")
	}
	a.detailField(c, "Modified", modified)
	a.detailField(c, "Elapsed", formatDuration(d.Elapsed))
	folder := d.Dir
	if folder == "" {
		folder = "—"
	}
	a.detailField(c, "Folder", folder)
	if d.Hash != "" {
		ui.Divider(c)
		a.hashField(c, "Hash", d.Hash)
	}
	if d.Expected != "" {
		a.hashField(c, "Expected", d.Expected)
	}
}

func (a *App) multiDetails(c *ui.Context, info *selectionInfo) {
	t := c.Theme()
	ui.Row(c).AlignItems(ui.Center).Gap(6).Children(func() {
		ui.Row(c).Padding(2, 8).Radius(999).Background(t.Accent.Alpha(0.13)).Shrink(0).Children(func() {
			ui.Text(c, "MULTI").FontSize(10).Bold().TextColor(t.Accent).SingleLine()
		})
		if info.Found < info.Count {
			ui.Textf(c, "%d of %d present", info.Found, info.Count).FontSize(10).TextColor(t.Warning).SingleLine()
		}
	})
	ui.Textf(c, "%d files", info.Count).FontSize(15).Bold().SingleLine()
	ui.Text(c, formatBytes(info.TotalSize)).FontSize(10).TextColor(t.TextMuted).SingleLine()
	ui.Divider(c)
	a.detailField(c, "Size", formatBytes(info.TotalSize))
	a.detailField(c, "Folders", strconv.Itoa(info.Folders))
	a.detailField(c, "Elapsed", formatDuration(info.TotalElapsed))
	if info.LargestName != "" {
		a.detailField(c, "Largest", info.LargestName+" · "+formatBytes(info.LargestSize))
	} else {
		a.detailField(c, "Largest", "—")
	}
	if info.SmallestName != "" && info.SmallestSize != info.LargestSize {
		a.detailField(c, "Smallest", info.SmallestName+" · "+formatBytes(info.SmallestSize))
	} else {
		a.detailField(c, "Smallest", "—")
	}
	ui.Divider(c)
	ui.Row(c).Gap(6).Wrap().Children(func() {
		if info.OK > 0 {
			a.statChip(c, "OK", info.OK, t.Success)
		}
		if info.Bad > 0 {
			a.statChip(c, "BAD", info.Bad, t.Danger)
		}
		if info.Missing > 0 {
			a.statChip(c, "MISSING", info.Missing, t.Warning)
		}
		if info.Error > 0 {
			a.statChip(c, "ERROR", info.Error, t.Danger)
		}
		if info.Waiting > 0 {
			a.statChip(c, "WAITING", info.Waiting, t.TextMuted)
		}
	})
}

// detailField is one label and one value. The value is clipped to the
// sidebar and keeps the whole text in its tooltip.
func (a *App) detailField(c *ui.Context, label, value string) {
	t := c.Theme()
	ui.Row(c).Gap(8).AlignItems(ui.Center).Children(func() {
		ui.Text(c, label).FontSize(11).TextColor(t.TextMuted).Width(68).Shrink(0).SingleLine()
		ui.Text(c, value).FontSize(12).SingleLine().Grow(1).Tooltip(value)
	})
}

// hashField wraps a digest instead of clipping it: a SHA-256 is 64
// characters and the sidebar is narrower than that.
func (a *App) hashField(c *ui.Context, label, value string) {
	t := c.Theme()
	ui.Column(c).Gap(2).Children(func() {
		ui.Text(c, label).FontSize(10).TextColor(t.TextMuted).SingleLine()
		// Two lines fit a SHA-256 at this width; keeping the height fixed
		// means switching files never grows or shrinks the panel.
		ui.Column(c).Height(30).Children(func() {
			ui.Text(c, value).FontSize(10).Tooltip(value)
		})
	})
}

func formatBytes(n int64) string {
	if n < 0 {
		return "—"
	}
	const unit = int64(1024)
	if n < unit {
		return fmt.Sprintf("%d B", n)
	}
	div, exp := unit, 0
	for v := n / unit; v >= unit && exp < 5; v /= unit {
		div *= unit
		exp++
	}
	return fmt.Sprintf("%.1f %ciB", float64(n)/float64(div), "KMGTPE"[exp])
}

func formatDuration(d time.Duration) string {
	if d <= 0 {
		return "—"
	}
	if d < time.Second {
		return fmt.Sprintf("%d ms", d.Milliseconds())
	}
	if d < time.Minute {
		return fmt.Sprintf("%.2f s", d.Seconds())
	}
	return fmt.Sprintf("%d:%02d", int(d.Minutes()), int(d.Seconds())%60)
}
