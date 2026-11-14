// GoSFV is a checksum creator and verifier: a Go port of SharpSFV, with native
// UI from MyGo.
package main

import (
	"fmt"
	"os"
	"runtime"
	"strings"

	"github.com/egoist/mygo"

	"gosfv/internal/cli"
	"gosfv/internal/gui"
	"gosfv/internal/hasher"
	"gosfv/internal/hexfmt"
	"gosfv/internal/settings"
)

const version = "1.0.0"

func main() {
	var (
		headless bool
		create   bool
		quiet    bool
		output   string
		iniPath  string
		algo     = hasher.XXH3
		algoSet  bool
		paths    []string
		showVer  bool
	)
	args := os.Args[1:]
	for i := 0; i < len(args); i++ {
		arg := args[i]
		switch {
		case arg == "-headless" || arg == "--headless":
			headless = true
		case arg == "-create" || arg == "--create":
			create = true
		case arg == "-quiet" || arg == "--quiet":
			quiet = true
		case arg == "-version" || arg == "--version":
			showVer = true
		case arg == "-h" || arg == "--help":
			usage()
			return
		case arg == "--":
			paths = append(paths, args[i+1:]...)
			i = len(args)
		case strings.HasPrefix(arg, "-algo=") || strings.HasPrefix(arg, "--algo="):
			val := arg[strings.IndexByte(arg, '=')+1:]
			k, ok := hasher.Parse(val)
			if !ok {
				fail("unknown algorithm %q (xxh3, crc32, md5, sha1, sha256)", val)
			}
			algo, algoSet = k, true
		case arg == "-algo" || arg == "--algo":
			if i+1 >= len(args) {
				fail("-algo needs a value")
			}
			i++
			k, ok := hasher.Parse(args[i])
			if !ok {
				fail("unknown algorithm %q (xxh3, crc32, md5, sha1, sha256)", args[i])
			}
			algo, algoSet = k, true
		case strings.HasPrefix(arg, "-o=") || strings.HasPrefix(arg, "--output="):
			output = arg[strings.IndexByte(arg, '=')+1:]
		case arg == "-o" || arg == "--output":
			if i+1 >= len(args) {
				fail("-o needs a value")
			}
			i++
			output = args[i]
		case strings.HasPrefix(arg, "-ini=") || strings.HasPrefix(arg, "--ini="):
			iniPath = arg[strings.IndexByte(arg, '=')+1:]
		case arg == "-ini" || arg == "--ini":
			if i+1 >= len(args) {
				fail("-ini needs a value")
			}
			i++
			iniPath = args[i]
		case strings.HasPrefix(arg, "-"):
			fail("unknown option %q", arg)
		default:
			paths = append(paths, arg)
		}
	}
	if showVer {
		fmt.Printf("GoSFV %s (MyGo %s, %s)\n", version, mygo.Version, runtimeVersion())
		fmt.Printf("Hex codec: %s\n", hexfmt.Backend())
		fmt.Printf("Workers: %d (GOMAXPROCS), %d CPUs\n", runtime.GOMAXPROCS(0), runtime.NumCPU())
		return
	}
	if iniPath == "" {
		iniPath = settings.DefaultPath()
	}
	st := settings.Load(iniPath)

	if headless {
		os.Exit(cli.Run(cli.Options{
			Create:       create,
			Algo:         algo,
			AlgoSet:      algoSet,
			Output:       output,
			Paths:        paths,
			Quiet:        quiet,
			Settings:     st,
			SettingsPath: iniPath,
		}))
	}

	if create && len(paths) == 0 {
		fail("-create needs at least one file or folder")
	}
	if err := gui.Run(gui.Config{
		Paths:        paths,
		CreateMode:   create,
		CreateOutput: output,
		SettingsPath: iniPath,
		Algo:         algo,
		AlgoSet:      algoSet,
	}); err != nil {
		fmt.Fprintln(os.Stderr, "GoSFV:", err)
		os.Exit(1)
	}
}

func runtimeVersion() string { return runtime.Version() }

func fail(format string, args ...any) {
	fmt.Fprintf(os.Stderr, "GoSFV: "+format+"\n", args...)
	os.Exit(2)
}

func usage() {
	fmt.Print(`GoSFV - checksum creator and verifier

Usage:
  GoSFV [options] [files or folders]
  GoSFV -headless [options] [files or folders]

Options:
  -create              create checksums (the mini window from Explorer)
  -headless            run without a window and print a CLI report
  -quiet               no progress bar (summary still printed)
  -algo NAME           xxh3 (default), crc32, md5, sha1, sha256
  -o PATH              output checksum file (create/headless)
  -ini PATH            configuration file (default: GoSFV.ini beside the exe)
  -version             print the version
  -h, --help           show this help

Examples:
  GoSFV.exe -headless C:\LinuxDistros\ubuntu.md5
  GoSFV.exe -headless -create C:\Backups\2023
  GoSFV.exe -headless -create -algo sha256 -o digests.sha256 C:\Builds
`)
}
