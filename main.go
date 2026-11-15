// GoSFV is a checksum creator and verifier with a native UI from MyGo.
package main

import (
	"fmt"
	"os"
	"runtime"

	"github.com/egoist/mygo"

	"github.com/lrweck/gosfv/internal/cli"
	"github.com/lrweck/gosfv/internal/gui"
	"github.com/lrweck/gosfv/internal/hexfmt"
	"github.com/lrweck/gosfv/internal/settings"
)

const version = "1.0.1"

func main() {
	flags, err := cli.Parse(os.Args[1:])
	if err != nil {
		fmt.Fprintln(os.Stderr, "GoSFV:", err)
		fmt.Fprint(os.Stderr, cli.Usage())
		os.Exit(2)
	}
	if flags.Help {
		fmt.Print(cli.Usage())
		return
	}
	if flags.Version {
		printVersion()
		return
	}

	iniPath := flags.Ini
	if iniPath == "" {
		iniPath = settings.DefaultPath()
	}
	st := settings.Load(iniPath)
	if flags.ThemeSet {
		st.Theme = flags.Theme
	}

	if flags.Headless {
		os.Exit(cli.Run(cli.Options{
			Create:       flags.Create,
			Algo:         flags.Algo,
			AlgoSet:      flags.AlgoSet,
			Output:       flags.Output,
			Paths:        flags.Paths,
			Quiet:        flags.Quiet,
			Settings:     st,
			SettingsPath: iniPath,
		}))
	}

	if flags.Create && len(flags.Paths) == 0 {
		fail("-create needs at least one file or folder")
	}
	if err := gui.Run(gui.Config{
		Paths:        flags.Paths,
		CreateMode:   flags.Create,
		CreateOutput: flags.Output,
		SettingsPath: iniPath,
		Algo:         flags.Algo,
		AlgoSet:      flags.AlgoSet,
		Theme:        st.Theme,
		ThemeSet:     flags.ThemeSet,
		Version:      version,
	}); err != nil {
		fmt.Fprintln(os.Stderr, "GoSFV:", err)
		os.Exit(1)
	}
}

func printVersion() {
	fmt.Printf("GoSFV %s (MyGo %s, %s)\n", version, mygo.Version, runtime.Version())
	fmt.Printf("Hex codec: %s\n", hexfmt.Backend())
	fmt.Printf("Workers: %d (GOMAXPROCS), %d CPUs\n", runtime.GOMAXPROCS(0), runtime.NumCPU())
}

func fail(format string, args ...any) {
	fmt.Fprintf(os.Stderr, "GoSFV: "+format+"\n", args...)
	os.Exit(2)
}
