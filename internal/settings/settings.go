// Package settings persists GoSFV's INI file.
//
// The file is portable and lives beside the executable. Keys keep the names
// of the original Windows SFV tool where they overlap, so an existing INI can
// be copied over and keeps the user's preferences.
package settings

import (
	"bufio"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"

	"github.com/lrweck/gosfv/internal/drive"
	"github.com/lrweck/gosfv/internal/hasher"
)

// PathMode is how checksum files store paths.
type PathMode uint8

const (
	Relative PathMode = iota
	Absolute
)

func (m PathMode) String() string {
	if m == Absolute {
		return "Absolute"
	}
	return "Relative"
}

// ThemeMode is the window appearance.
type ThemeMode uint8

const (
	// ThemeSystem follows the desktop's light or dark setting.
	ThemeSystem ThemeMode = iota
	ThemeLight
	ThemeDark
)

func (m ThemeMode) String() string {
	switch m {
	case ThemeLight:
		return "Light"
	case ThemeDark:
		return "Dark"
	default:
		return "System"
	}
}

// ParseTheme reads a theme name, keeping the default for unknown values.
func ParseTheme(s string) ThemeMode {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "light":
		return ThemeLight
	case "dark":
		return ThemeDark
	default:
		return ThemeSystem
	}
}

// Settings is the portable configuration.
type Settings struct {
	ShowTime        bool
	ShowThroughput  bool
	ShowFilter      bool
	ShowAdvanced    bool
	ShowFullPaths   bool
	ShowHashCol     bool
	ShowExpectedCol bool
	LockColumns     bool
	PathMode        PathMode
	ProcMode        drive.Mode
	Comments        bool
	PathPrefix      string
	IncludePattern  string
	ExcludePattern  string
	Recursive       bool
	Signature       string
	Theme           ThemeMode
	DefaultAlgo     hasher.Kind
	ColumnOrder     []string
	ColumnWidths    map[string]float32
	WindowStateKey  string
}

// Defaults returns the settings a new installation starts with.
func Defaults() Settings {
	return Settings{
		ShowHashCol:     true,
		ShowExpectedCol: true,
		LockColumns:     true,
		PathMode:        Relative,
		ProcMode:        drive.Auto,
		Recursive:       true,
		Signature:       "L33T",
		Theme:           ThemeSystem,
		DefaultAlgo:     hasher.XXH3,
		ColumnWidths:    map[string]float32{},
		WindowStateKey:  "main",
	}
}

// DefaultPath is GoSFV.ini beside the executable.
func DefaultPath() string {
	exe, err := os.Executable()
	if err == nil {
		return filepath.Join(filepath.Dir(exe), "GoSFV.ini")
	}
	return "GoSFV.ini"
}

// Load reads path. Missing or malformed keys keep their defaults.
func Load(path string) Settings {
	s := Defaults()
	f, err := os.Open(path)
	if err != nil {
		return s
	}
	defer f.Close()
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 4096), 1<<20)
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" || strings.HasPrefix(line, "[") || strings.HasPrefix(line, ";") {
			continue
		}
		key, val, ok := strings.Cut(line, "=")
		if !ok {
			continue
		}
		s.set(strings.TrimSpace(key), strings.TrimSpace(val))
	}
	return s
}

func (s *Settings) set(key, val string) {
	switch strings.ToLower(key) {
	case "timetab":
		s.ShowTime = val == "1"
	case "showthroughputstats":
		s.ShowThroughput = val == "1"
	case "showfilterpanel":
		s.ShowFilter = val == "1"
	case "showadvancedbar":
		s.ShowAdvanced = val == "1"
	case "showfullpaths":
		s.ShowFullPaths = val == "1"
	case "showhashcol":
		s.ShowHashCol = val == "1"
	case "showexpectedhashcol":
		s.ShowExpectedCol = val == "1"
	case "lockcolumns":
		s.LockColumns = val == "1"
	case "pathstoragemode":
		if strings.EqualFold(val, "Absolute") {
			s.PathMode = Absolute
		} else if strings.EqualFold(val, "Relative") {
			s.PathMode = Relative
		}
	case "useabsolutepaths":
		if val == "1" {
			s.PathMode = Absolute
		}
	case "processingmode":
		s.ProcMode = drive.ParseMode(val)
	case "optimizeforhdd":
		if val == "1" {
			s.ProcMode = drive.HDD
		}
	case "enablechecksumcomments":
		s.Comments = val == "1"
	case "pathprefix":
		s.PathPrefix = val
	case "includepattern":
		s.IncludePattern = val
	case "excludepattern":
		s.ExcludePattern = val
	case "scanrecursive":
		s.Recursive = val == "1"
	case "signature":
		if val != "" {
			s.Signature = val
		}
	case "theme":
		s.Theme = ParseTheme(val)
	case "darkmode":
		// Older builds stored the appearance as a boolean.
		switch strings.ToLower(val) {
		case "1", "true", "yes":
			s.Theme = ThemeDark
		case "0", "false", "no":
			s.Theme = ThemeLight
		}
	case "defaultalgo":
		if k, ok := hasher.Parse(val); ok {
			s.DefaultAlgo = k
		}
	case "columnorder":
		s.ColumnOrder = s.ColumnOrder[:0]
		for _, pair := range strings.Split(val, ",") {
			name, _, _ := strings.Cut(pair, ":")
			if name = strings.TrimSpace(name); name != "" {
				s.ColumnOrder = append(s.ColumnOrder, name)
			}
		}
	case "columnwidths":
		for _, pair := range strings.Split(val, ",") {
			name, width, ok := strings.Cut(pair, ":")
			if !ok {
				continue
			}
			if w, err := strconv.ParseFloat(strings.TrimSpace(width), 32); err == nil {
				s.ColumnWidths[strings.TrimSpace(name)] = float32(w)
			}
		}
	}
}

// Save writes path atomically.
func (s Settings) Save(path string) error {
	var b strings.Builder
	b.WriteString("[GoSFV]\n")
	fmt.Fprintf(&b, "TimeTab=%s\n", bit(s.ShowTime))
	fmt.Fprintf(&b, "ShowThroughputStats=%s\n", bit(s.ShowThroughput))
	fmt.Fprintf(&b, "ShowFilterPanel=%s\n", bit(s.ShowFilter))
	fmt.Fprintf(&b, "ShowAdvancedBar=%s\n", bit(s.ShowAdvanced))
	fmt.Fprintf(&b, "ShowFullPaths=%s\n", bit(s.ShowFullPaths))
	fmt.Fprintf(&b, "ShowHashCol=%s\n", bit(s.ShowHashCol))
	fmt.Fprintf(&b, "ShowExpectedHashCol=%s\n", bit(s.ShowExpectedCol))
	fmt.Fprintf(&b, "LockColumns=%s\n", bit(s.LockColumns))
	fmt.Fprintf(&b, "PathStorageMode=%s\n", s.PathMode)
	fmt.Fprintf(&b, "UseAbsolutePaths=%s\n", bit(s.PathMode == Absolute))
	fmt.Fprintf(&b, "ProcessingMode=%s\n", s.ProcMode)
	fmt.Fprintf(&b, "EnableChecksumComments=%s\n", bit(s.Comments))
	fmt.Fprintf(&b, "PathPrefix=%s\n", s.PathPrefix)
	fmt.Fprintf(&b, "IncludePattern=%s\n", s.IncludePattern)
	fmt.Fprintf(&b, "ExcludePattern=%s\n", s.ExcludePattern)
	fmt.Fprintf(&b, "ScanRecursive=%s\n", bit(s.Recursive))
	fmt.Fprintf(&b, "Signature=%s\n", s.Signature)
	fmt.Fprintf(&b, "Theme=%s\n", s.Theme)
	fmt.Fprintf(&b, "DefaultAlgo=%s\n", s.DefaultAlgo)
	order := make([]string, len(s.ColumnOrder))
	for i, name := range s.ColumnOrder {
		order[i] = fmt.Sprintf("%s:%d", name, i)
	}
	fmt.Fprintf(&b, "ColumnOrder=%s\n", strings.Join(order, ","))
	if len(s.ColumnWidths) > 0 {
		b.WriteString("ColumnWidths=")
		first := true
		seen := map[string]bool{}
		writeWidth := func(name string) {
			w, ok := s.ColumnWidths[name]
			if !ok || seen[name] {
				return
			}
			seen[name] = true
			if !first {
				b.WriteByte(',')
			}
			first = false
			fmt.Fprintf(&b, "%s:%g", name, w)
		}
		for _, name := range s.ColumnOrder {
			writeWidth(name)
		}
		rest := make([]string, 0, len(s.ColumnWidths))
		for name := range s.ColumnWidths {
			if !seen[name] {
				rest = append(rest, name)
			}
		}
		sort.Strings(rest)
		for _, name := range rest {
			writeWidth(name)
		}
		b.WriteByte('\n')
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, []byte(b.String()), 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

func bit(v bool) string {
	if v {
		return "1"
	}
	return "0"
}

// Reset deletes the INI file, if it exists.
func Reset(path string) error {
	err := os.Remove(path)
	if os.IsNotExist(err) {
		return nil
	}
	return err
}
