package gui

import (
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

// applyPalette makes the window use GoSFV's palette for the appearance the
// frame resolved, which follows the system unless the user forced one.
func applyPalette(c *ui.Context) {
	c.SetTheme(palette(c.Theme().Dark))
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
