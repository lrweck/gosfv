// Package cli runs GoSFV without a window.
//
// It shares scanning, hashing, verification and checksum writing with the GUI,
// so the two modes cannot drift apart.
package cli

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"github.com/lrweck/gosfv/internal/checksum"
	"github.com/lrweck/gosfv/internal/drive"
	"github.com/lrweck/gosfv/internal/hasher"
	"github.com/lrweck/gosfv/internal/model"
	"github.com/lrweck/gosfv/internal/runner"
	"github.com/lrweck/gosfv/internal/scanner"
	"github.com/lrweck/gosfv/internal/settings"
)

// Options is a parsed command line.
type Options struct {
	Create  bool
	Algo    hasher.Kind
	AlgoSet bool
	Output  string
	Paths   []string
	// Quiet disables the terminal progress bar; the summary is still printed.
	Quiet    bool
	Settings settings.Settings
	// SettingsPath is where the GUI would save settings; headless reads it
	// for defaults but never writes it.
	SettingsPath string
}

// Run executes the headless command and returns the process exit code.
func Run(opt Options) int {
	if len(opt.Paths) == 0 {
		fmt.Fprintln(os.Stderr, "GoSFV: no input paths. Try -h for help.")
		return 2
	}
	if opt.Create {
		return runCreate(opt)
	}
	return runVerify(opt)
}

func runVerify(opt Options) int {
	path := opt.Paths[0]
	algo := opt.Algo
	if k, ok := hasher.FromExtension(filepath.Ext(path)); ok {
		algo = k
	}
	info := drive.Detect(filepath.Dir(path))
	seq := drive.Sequential(opt.Settings.ProcMode, info)
	fmt.Printf("Verifying %s (%s, %s, %d workers)\n", path, algo.ShortLabel(), modeName(seq), workerCount(seq))
	ui := newProgressUI(!opt.Quiet && isTerminal(os.Stderr), true, "Verifying")
	defer ui.finish()

	var files []model.File
	start := time.Now()
	rep, err := runner.Verify(context.Background(), path, runner.Options{
		Algo:       algo,
		Sequential: seq,
		Scan:       scanOptions(opt.Settings),
		OnFiles: func(f []model.File) {
			files = f
			ui.startFiles(files)
		},
		OnTick: func(p runner.Progress, done []runner.Completed) {
			applyResults(files, done)
			ui.tick(p)
		},
	})
	if err != nil {
		ui.finish()
		fmt.Fprintf(os.Stderr, "\nGoSFV: %v\n", err)
		return 2
	}
	ui.finish()
	fmt.Println()
	printSummary(rep, time.Since(start))
	printProblems(rep)
	return exitCode(rep)
}

func runCreate(opt Options) int {
	base := scanner.CommonBase(opt.Paths)
	if base == "" {
		base = "."
	}
	algo := opt.Algo
	output := opt.Output
	if output != "" {
		if !opt.AlgoSet {
			if k, ok := hasher.FromExtension(filepath.Ext(output)); ok {
				algo = k
			}
		}
		if filepath.Ext(output) == "" {
			output += algo.Ext()
		}
	} else {
		name := defaultJobName(opt.Paths, base)
		output = filepath.Join(base, name+algo.Ext())
	}
	info := drive.Detect(base)
	seq := drive.Sequential(opt.Settings.ProcMode, info)
	fmt.Printf("Creating %s (%s, %s, %d workers)\n", output, algo.ShortLabel(), modeName(seq), workerCount(seq))
	ui := newProgressUI(!opt.Quiet && isTerminal(os.Stderr), true, "Creating")
	ui.startScan()
	defer ui.finish()

	var files []model.File
	start := time.Now()
	rep, err := runner.Create(context.Background(), opt.Paths, base, runner.Options{
		Algo:       algo,
		Sequential: seq,
		Scan:       scanOptions(opt.Settings),
		OnScan:     ui.scanned,
		OnFiles: func(f []model.File) {
			files = f
			ui.startFiles(files)
		},
		OnTick: func(p runner.Progress, done []runner.Completed) {
			applyResults(files, done)
			ui.tick(p)
		},
	})
	if err != nil {
		ui.finish()
		fmt.Fprintf(os.Stderr, "\nGoSFV: %v\n", err)
		return 2
	}
	ui.finish()
	fmt.Println()
	if rep.Progress.Total == 0 {
		fmt.Fprintln(os.Stderr, "GoSFV: no files matched.")
		return 1
	}
	relative := opt.Settings.PathMode == settings.Relative
	if _, err := checksum.WriteFile(output, algo, files, checksum.WriteOptions{
		Relative:  relative,
		SaveDir:   filepath.Dir(output),
		Prefix:    pathPrefix(opt.Settings),
		Comments:  opt.Settings.Comments,
		Signature: opt.Settings.Signature,
		OnlyOK:    true,
	}); err != nil {
		fmt.Fprintf(os.Stderr, "GoSFV: writing %s: %v\n", output, err)
		return 2
	}
	fmt.Printf("Wrote %s\n", output)
	printSummary(rep, time.Since(start))
	printProblems(rep)
	return exitCode(rep)
}

func scanOptions(s settings.Settings) runner.ScanOptions {
	o := runner.ScanOptions{
		Recursive:  s.Recursive,
		SkipHidden: true,
	}
	if s.ShowAdvanced {
		o.Include = s.IncludePattern
		o.Exclude = s.ExcludePattern
	}
	return o
}

func pathPrefix(s settings.Settings) string {
	if s.ShowAdvanced {
		return s.PathPrefix
	}
	return ""
}

func defaultJobName(paths []string, base string) string {
	if len(paths) == 1 {
		p := paths[0]
		if info, err := os.Stat(p); err == nil && info.IsDir() {
			return filepath.Base(filepath.Clean(p))
		}
		name := filepath.Base(p)
		return strings.TrimSuffix(name, filepath.Ext(name))
	}
	if name := filepath.Base(base); name != "" && name != "." && name != string(filepath.Separator) {
		return name
	}
	return "checksums"
}

func modeName(sequential bool) string {
	if sequential {
		return "HDD/Sequential"
	}
	return "SSD/Parallel"
}

func workerCount(sequential bool) int {
	if sequential {
		return 1
	}
	return runtime.GOMAXPROCS(0)
}
