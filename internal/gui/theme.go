package gui

import (
	"time"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"

	"github.com/lrweck/gosfv/internal/settings"
)

// The palettes start from the library's defaults, which are complete and
// already follow the desktop's contrast conventions, and change only the
// colors and metrics that give GoSFV its own look. Both are built once:
// SetTheme stores the pointer, so a fresh theme every frame would be
// garbage for no reason.
var (
	lightPalette = modernPalette(ui.LightTheme())
	darkPalette  = modernPalette(ui.DarkTheme())
)

func modernPalette(t *ui.Theme) *ui.Theme {
	if t.Dark {
		t.Background = ui.Hex("#101114")
		t.Surface = ui.Hex("#17191E")
		t.SurfaceHover = ui.Hex("#20232A")
		t.SurfacePressed = ui.Hex("#292D36")
		t.Border = ui.Hex("#2B2F38")
		t.Accent = ui.Hex("#6366F1")
		t.AccentHover = ui.Hex("#7C83F6")
		t.AccentPressed = ui.Hex("#4F46E5")
		t.AccentText = ui.Hex("#FFFFFF")
	} else {
		t.Background = ui.Hex("#F6F7F9")
		t.Surface = ui.Hex("#FFFFFF")
		t.SurfaceHover = ui.Hex("#F0F1F4")
		t.SurfacePressed = ui.Hex("#E4E6EB")
		t.Border = ui.Hex("#E1E4E8")
		t.Accent = ui.Hex("#4F46E5")
		t.AccentHover = ui.Hex("#4338CA")
		t.AccentPressed = ui.Hex("#3730A3")
		t.AccentText = ui.Hex("#FFFFFF")
	}
	t.Radius = 8
	t.Spacing = 4
	return t
}

// palette returns the light or dark palette for the frame.
func palette(dark bool) *ui.Theme {
	if dark {
		return darkPalette
	}
	return lightPalette
}

// themeFade is how long the colors take to move between the light and dark
// palettes. The Windows title bar fades too; matching it avoids the flash
// of an instant repaint.
const themeFade = 200 * time.Millisecond

// applyPalette makes the window use GoSFV's palette for the appearance the
// frame resolved, which follows the system unless the user forced one. A
// change blends from the colors on screen to the new ones.
func (a *App) applyPalette(c *ui.Context) {
	target := palette(c.Theme().Dark)
	now := time.Now()
	if a.themeTo == nil {
		// First frame: start in the right palette, with no fade. The
		// start is backdated so later frames do not schedule one either.
		a.themeFrom, a.themeTo, a.themeAt = target, target, now.Add(-themeFade)
		c.SetTheme(target)
		return
	}
	if target != a.themeTo {
		// Keep the colors currently on screen, which may themselves be
		// halfway through another change.
		a.themeFrom = a.fadeTheme(now)
		a.themeTo = target
		a.themeAt = now
	}
	if c.Preferences().ReduceMotion {
		a.themeFrom, a.themeAt = target, now
		c.SetTheme(target)
		return
	}
	frac := fadeStep(now.Sub(a.themeAt))
	if frac >= 1 {
		a.themeFrom = target
		c.SetTheme(target)
		return
	}
	c.SetTheme(blendThemes(a.themeFrom, a.themeTo, frac))
	c.After(16 * time.Millisecond)
}

// fadeTheme is the blended palette for now, used when a new change starts
// before the last one finished.
func (a *App) fadeTheme(now time.Time) *ui.Theme {
	frac := fadeStep(now.Sub(a.themeAt))
	switch {
	case frac >= 1:
		return a.themeTo
	case frac <= 0:
		return a.themeFrom
	default:
		return blendThemes(a.themeFrom, a.themeTo, frac)
	}
}

// fadeStep maps elapsed time to 0..1 with a smoothstep curve, so the change
// starts and ends gently instead of looking like a linear wipe.
func fadeStep(elapsed time.Duration) float32 {
	p := float32(elapsed) / float32(themeFade)
	if p <= 0 {
		return 0
	}
	if p >= 1 {
		return 1
	}
	return p * p * (3 - 2*p)
}

func blendThemes(a, b *ui.Theme, t float32) *ui.Theme {
	out := *b
	out.Background = blendColor(a.Background, b.Background, t)
	out.Surface = blendColor(a.Surface, b.Surface, t)
	out.SurfaceHover = blendColor(a.SurfaceHover, b.SurfaceHover, t)
	out.SurfacePressed = blendColor(a.SurfacePressed, b.SurfacePressed, t)
	out.Border = blendColor(a.Border, b.Border, t)
	out.Text = blendColor(a.Text, b.Text, t)
	out.TextMuted = blendColor(a.TextMuted, b.TextMuted, t)
	out.Accent = blendColor(a.Accent, b.Accent, t)
	out.AccentHover = blendColor(a.AccentHover, b.AccentHover, t)
	out.AccentPressed = blendColor(a.AccentPressed, b.AccentPressed, t)
	out.AccentText = blendColor(a.AccentText, b.AccentText, t)
	out.InactiveAccent = blendColor(a.InactiveAccent, b.InactiveAccent, t)
	out.Danger = blendColor(a.Danger, b.Danger, t)
	out.Warning = blendColor(a.Warning, b.Warning, t)
	out.Success = blendColor(a.Success, b.Success, t)
	out.Selection = blendColor(a.Selection, b.Selection, t)
	out.Focus = blendColor(a.Focus, b.Focus, t)
	out.Inverse = blendColor(a.Inverse, b.Inverse, t)
	out.InverseText = blendColor(a.InverseText, b.InverseText, t)
	out.Scrollbar = blendColor(a.Scrollbar, b.Scrollbar, t)
	return &out
}

func blendColor(a, b ui.Color, t float32) ui.Color {
	mix := func(x, y uint8) uint8 {
		return uint8(float32(x) + (float32(y)-float32(x))*t + 0.5)
	}
	return ui.Color{R: mix(a.R, b.R), G: mix(a.G, b.G), B: mix(a.B, b.B), A: mix(a.A, b.A)}
}

// themeSource maps the saved preference to the library's source.
func themeSource(m settings.ThemeMode) mygo.ThemeSource {
	switch m {
	case settings.ThemeLight:
		return mygo.ThemeLight
	case settings.ThemeDark:
		return mygo.ThemeDark
	default:
		return mygo.ThemeSystem
	}
}
