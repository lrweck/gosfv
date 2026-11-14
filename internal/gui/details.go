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
			if a.sel.Loading {
				ui.Progress(c, a.sel.Progress).Height(3)
				ui.Text(c, "Reading details...").FontSize(10).TextColor(t.TextMuted).SingleLine()
			}
		})
	})
}

func (a *App) emptyDetails(c *ui.Context) {
	t := c.Theme()
	ui.Text(c, "Nothing selected").FontSize(15).Bold()
	ui.Text(c, "Click a file to see its size, date, hashes and timing.").FontSize(11).TextColor(t.TextMuted)
	ui.Divider(c)
	a.detailField(c, "Files", strconv.Itoa(len(a.store)))
	a.detailField(c, "Algorithm", a.algo.Label())
}

func (a *App) singleDetails(c *ui.Context, d *fileDetail) {
	t := c.Theme()
	color := a.statusColor(c, d.Status)
	ui.Row(c).AlignItems(ui.Center).Gap(6).Children(func() {
		ui.Row(c).Padding(2, 8).Radius(999).Background(color.Alpha(0.13)).Shrink(0).Children(func() {
			ui.Text(c, d.Status.String()).FontSize(10).Bold().TextColor(color).SingleLine()
		})
		if d.StatOK {
			ui.Text(c, "on disk").FontSize(10).TextColor(t.TextMuted)
		} else if d.StatErr != "" {
			ui.Text(c, "unavailable").FontSize(10).TextColor(t.Warning)
		}
	})
	ui.Text(c, d.Name).FontSize(15).Bold().SingleLine().Tooltip(d.Name)
	ui.Text(c, d.Path).FontSize(10).TextColor(t.TextMuted).Tooltip(d.Path)
	ui.Divider(c)
	a.detailField(c, "Size", formatBytes(d.Size))
	if d.StatOK {
		a.detailField(c, "Modified", d.ModTime.Format("2006-01-02 15:04"))
	}
	if d.StatErr != "" {
		ui.Text(c, d.StatErr).FontSize(10).TextColor(t.Warning)
	}
	a.detailField(c, "Elapsed", formatDuration(d.Elapsed))
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
	ui.Textf(c, "%d files", info.Count).FontSize(22).Bold()
	ui.Text(c, formatBytes(info.TotalSize)).FontSize(13).TextColor(t.TextMuted)
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
	ui.Divider(c)
	if info.Found < info.Count {
		a.detailField(c, "Present", fmt.Sprintf("%d of %d", info.Found, info.Count))
	}
	a.detailField(c, "Folders", strconv.Itoa(info.Folders))
	a.detailField(c, "Elapsed", formatDuration(info.TotalElapsed))
	if info.LargestName != "" {
		a.detailField(c, "Largest", info.LargestName+" · "+formatBytes(info.LargestSize))
	}
	if info.SmallestName != "" && info.SmallestSize != info.LargestSize {
		a.detailField(c, "Smallest", info.SmallestName+" · "+formatBytes(info.SmallestSize))
	}
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
		ui.Text(c, value).FontSize(10).Tooltip(value)
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
