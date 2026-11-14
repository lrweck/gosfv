package cli

import (
	"fmt"
	"io"
	"strings"

	"github.com/spf13/pflag"

	"gosfv/internal/hasher"
	"gosfv/internal/settings"
)

// Flags is the parsed command line. The same flags serve the window and
// the headless runner; main chooses between them.
type Flags struct {
	Headless bool
	Create   bool
	Quiet    bool
	Output   string
	Ini      string
	Algo     hasher.Kind
	AlgoSet  bool
	Theme    settings.ThemeMode
	ThemeSet bool
	Version  bool
	Help     bool
	Paths    []string

	// algoText and themeText hold the strings the flag set parses; the
	// validated values are Algo and Theme.
	algoText  string
	themeText string
}

// longNames are the flags that older releases and the README spell with a
// single dash. pflag reads a single dash as a group of shorthands, so the
// parser rewrites "-headless" to "--headless" before parsing and keeps both
// spellings working.
var longNames = map[string]bool{
	"headless": true,
	"create":   true,
	"quiet":    true,
	"version":  true,
	"help":     true,
	"algo":     true,
	"output":   true,
	"ini":      true,
	"theme":    true,
}

// Parse parses args, which are os.Args without the program name.
func Parse(args []string) (*Flags, error) {
	f := &Flags{Algo: hasher.XXH3, Theme: settings.ThemeSystem}
	fs := pflag.NewFlagSet("gosfv", pflag.ContinueOnError)
	fs.SetOutput(io.Discard)
	define(fs, f)
	if err := fs.Parse(normalize(args)); err != nil {
		return nil, err
	}
	f.Paths = fs.Args()

	if f.algoText != "" {
		k, ok := hasher.Parse(f.algoText)
		if !ok {
			return nil, fmt.Errorf("unknown algorithm %q (xxh3, crc32, md5, sha1, sha256)", f.algoText)
		}
		f.Algo, f.AlgoSet = k, true
	}
	if f.themeText != "" {
		switch strings.ToLower(f.themeText) {
		case "system", "auto":
			f.Theme = settings.ThemeSystem
		case "light":
			f.Theme = settings.ThemeLight
		case "dark":
			f.Theme = settings.ThemeDark
		default:
			return nil, fmt.Errorf("unknown theme %q (system, light, dark)", f.themeText)
		}
		f.ThemeSet = true
	}
	return f, nil
}

// define declares every flag once, for parsing and for the help text.
func define(fs *pflag.FlagSet, f *Flags) {
	fs.BoolVar(&f.Headless, "headless", false, "run without a window and print a CLI report")
	fs.BoolVar(&f.Create, "create", false, "create checksums (the mini window from Explorer)")
	fs.BoolVarP(&f.Quiet, "quiet", "q", false, "no progress bar (the summary is still printed)")
	fs.StringVarP(&f.Output, "output", "o", "", "output checksum file (create and headless)")
	fs.StringVar(&f.Ini, "ini", "", "configuration file (default: GoSFV.ini beside the executable)")
	fs.StringVar(&f.algoText, "algo", "", "xxh3 (default), crc32, md5, sha1 or sha256")
	fs.StringVar(&f.themeText, "theme", "", "system (default), light or dark")
	fs.BoolVar(&f.Version, "version", false, "print the version")
	fs.BoolVarP(&f.Help, "help", "h", false, "show this help")
}

// Usage is the complete help text, with each flag and its default.
func Usage() string {
	f := &Flags{}
	fs := pflag.NewFlagSet("gosfv", pflag.ContinueOnError)
	define(fs, f)
	usage := formatUsages(fs.FlagUsages())
	return `GoSFV - checksum creator and verifier

Usage:
  GoSFV [options] [files or folders]
  GoSFV -headless [options] [files or folders]

Options:
` + usage + `
Examples:
  GoSFV.exe -headless C:\LinuxDistros\ubuntu.md5
  GoSFV.exe -headless -create C:\Backups\2023
  GoSFV.exe -headless -create -algo sha256 -o digests.sha256 C:\Builds
  GoSFV.exe -theme dark C:\Backups
`
}

// formatUsages turns pflag's help into the conventional two-column list:
// the boolean "[=true|false]" pflag v1.1 appends is removed, then the flag
// column is padded to one width. Without this the columns shift on every
// boolean flag.
func formatUsages(raw string) string {
	type row struct{ left, right string }
	var rows []row
	for _, line := range strings.Split(strings.TrimRight(raw, "\n"), "\n") {
		line = strings.ReplaceAll(line, "[=true|false]", "")
		left, right := strings.TrimRight(line, " "), ""
		indent := len(line) - len(strings.TrimLeft(line, " "))
		for i := indent; i+2 < len(line); i++ {
			if line[i] == ' ' && line[i+1] == ' ' && line[i+2] == ' ' {
				left, right = strings.TrimRight(line[:i], " "), strings.TrimSpace(line[i:])
				break
			}
		}
		rows = append(rows, row{left, right})
	}
	width := 0
	for _, r := range rows {
		if len(r.left) > width {
			width = len(r.left)
		}
	}
	var b strings.Builder
	for _, r := range rows {
		fmt.Fprintf(&b, "%-*s   %s\n", width, r.left, r.right)
	}
	return b.String()
}

// normalize rewrites the long flags written with a single dash, as earlier
// GoSFV releases documented them, into the GNU form pflag expects.
func normalize(args []string) []string {
	out := make([]string, len(args))
	copy(out, args)
	for i, arg := range out {
		if len(arg) < 2 || arg[0] != '-' || arg[1] == '-' {
			continue
		}
		name := arg[1:]
		if j := strings.IndexByte(name, '='); j >= 0 {
			name = name[:j]
		}
		if longNames[name] {
			out[i] = "-" + arg
		}
	}
	return out
}
