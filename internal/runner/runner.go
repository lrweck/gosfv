// Package runner coordinates scanning, hashing and verification.
//
// It is the single implementation behind the GUI and the headless CLI: both
// receive the same file list, the same live progress and the same final report.
// The runner owns no UI state; callbacks hand ownership of the file slice to the
// caller before hashing starts.
package runner

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"time"

	"gosfv/internal/checksum"
	"gosfv/internal/engine"
	"gosfv/internal/hasher"
	"gosfv/internal/model"
	"gosfv/internal/scanner"
)

// ScanOptions controls the directory walk used by Create.
type ScanOptions struct {
	Recursive  bool
	SkipHidden bool
	Include    string
	Exclude    string
}

// Options configures a run.
type Options struct {
	Algo hasher.Kind
	// Sequential forces one worker and a large read buffer.
	Sequential bool
	// Workers overrides the worker count.
	Workers int
	Gate    *engine.Gate
	Scan    ScanOptions
	// Tick is how often progress is reported; 0 means 100ms.
	Tick time.Duration

	// OnScan reports how many files have been discovered so far.
	OnScan func(found int)
	// OnFiles hands the file list to the caller. The caller owns the slice
	// from that point; the runner only reads it.
	OnFiles func(files []model.File)
	// OnParsed reports the comments of a checksum file before hashing starts.
	OnParsed func(comments []string)
	// OnTick reports progress and the results completed since the last call.
	// The batch slice is only valid during the call. Its hashes point into
	// the run's immutable result arena and may be kept.
	OnTick func(p Progress, done []Completed)
	// OnFileProgress is called by workers for files of 50 MiB or more.
	OnFileProgress func(index int32, path string, done, total int64)
}

// Completed is one file that just finished.
type Completed struct {
	Index   int
	Status  model.Status
	Hash    []byte
	Elapsed time.Duration
	Err     error
}

// Progress is a snapshot with outcome counts.
type Progress struct {
	Done       int
	Total      int
	OK         int
	Bad        int
	Missing    int
	Error      int
	BytesDone  int64
	BytesTotal int64
	SpeedBPS   float64
	ETA        time.Duration
	Elapsed    time.Duration
}

// Problem is a file that needs attention.
type Problem struct {
	Name   string
	Status model.Status
	Err    error
}

// Report is the outcome of a run.
type Report struct {
	Progress Progress
	Problems []Problem
	Legacy   bool
	Algo     hasher.Kind
	Comments []string
	Checksum string // the checksum file read by Verify
	// Workers and ParallelCRC describe the scheduler that ran: for CRC-32,
	// one file may have been split across workers.
	Workers     int
	ParallelCRC bool
}

// Create scans inputs and hashes every file.
func Create(ctx context.Context, inputs []string, base string, opt Options) (*Report, error) {
	start := time.Now()
	scanned, err := scanner.Collect(ctx, inputs, base, scanner.Options{
		Recursive:  opt.Scan.Recursive,
		SkipHidden: opt.Scan.SkipHidden,
		Include:    opt.Scan.Include,
		Exclude:    opt.Scan.Exclude,
		Sort:       true,
	}, opt.OnScan)
	if err != nil {
		return nil, err
	}
	files := make([]model.File, len(scanned))
	for i := range scanned {
		s := &scanned[i]
		files[i] = model.File{
			ID:     uint64(i + 1),
			Name:   s.Name,
			Rel:    s.RelPath,
			Dir:    base,
			Full:   s.FullPath,
			Size:   s.Size,
			Status: model.Queued,
		}
	}
	return run(ctx, files, opt, start, false, "", nil)
}

// Verify parses a checksum file and hashes the files it names.
func Verify(ctx context.Context, checksumPath string, opt Options) (*Report, error) {
	start := time.Now()
	parsed, err := checksum.ParseFile(checksumPath, opt.Algo)
	if err != nil {
		return nil, err
	}
	if opt.OnParsed != nil {
		opt.OnParsed(parsed.Comments)
	}
	base := filepath.Dir(checksumPath)
	files := make([]model.File, len(parsed.Entries))
	for i := range parsed.Entries {
		e := &parsed.Entries[i]
		name := e.Name
		dir := base
		rel := name
		full := filepath.Join(dir, rel)
		if filepath.IsAbs(name) {
			dir, rel, full = "", name, name
		}
		f := model.File{
			ID:       uint64(i + 1),
			Name:     filepath.Base(name),
			Rel:      rel,
			Dir:      dir,
			Full:     full,
			Expected: e.Hash,
			Status:   model.Queued,
		}
		info, statErr := os.Stat(full)
		if statErr != nil {
			f.Status = model.Missing
		} else if info.IsDir() {
			f.Status = model.Error
		} else {
			f.Size = info.Size()
		}
		files[i] = f
	}
	slices.SortFunc(files, func(a, b model.File) int { return comparePath(a.Rel, b.Rel) })
	return run(ctx, files, opt, start, true, checksumPath, parsed.Comments)
}

func comparePath(a, b string) int {
	n := len(a)
	if len(b) < n {
		n = len(b)
	}
	for i := 0; i < n; i++ {
		ca, cb := a[i], b[i]
		if ca >= 'A' && ca <= 'Z' {
			ca += 'a' - 'A'
		}
		if cb >= 'A' && cb <= 'Z' {
			cb += 'a' - 'A'
		}
		if ca != cb {
			if ca < cb {
				return -1
			}
			return 1
		}
	}
	switch {
	case len(a) < len(b):
		return -1
	case len(a) > len(b):
		return 1
	}
	return 0
}

// run is shared by Create and Verify.
func run(ctx context.Context, files []model.File, opt Options, start time.Time, verify bool, checksumPath string, comments []string) (*Report, error) {
	algo := opt.Algo
	if algo.Size() == 0 {
		return nil, fmt.Errorf("unknown algorithm %d", algo)
	}
	opt = withDefaults(opt)
	if opt.OnFiles != nil {
		opt.OnFiles(files)
	}

	// Missing files were decided before the list was handed over, so the
	// caller already shows them. Build tasks only for files that exist.
	tasks := make([]engine.Task, 0, len(files))
	for i := range files {
		if files[i].Status == model.Missing || files[i].Status == model.Error {
			continue
		}
		tasks = append(tasks, engine.Task{
			Index: int32(i),
			Path:  files[i].FullPath(),
			Size:  files[i].Size,
		})
	}

	report := &Report{Algo: algo, Comments: comments, Checksum: checksumPath}
	var missing, errorsPre int
	for i := range files {
		switch files[i].Status {
		case model.Missing:
			missing++
			report.Problems = append(report.Problems, Problem{Name: files[i].FullPath(), Status: model.Missing})
		case model.Error:
			errorsPre++
			report.Problems = append(report.Problems, Problem{Name: files[i].FullPath(), Status: model.Error})
		}
	}

	if len(tasks) == 0 {
		p := Progress{Total: len(files), Missing: missing, Error: errorsPre, Elapsed: time.Since(start)}
		report.Progress = p
		if opt.OnTick != nil {
			opt.OnTick(p, nil)
		}
		return report, nil
	}

	eng := engine.New(tasks, engine.Options{
		Kind:           algo,
		Sequential:     opt.Sequential,
		Workers:        opt.Workers,
		Gate:           opt.Gate,
		OnFileProgress: opt.OnFileProgress,
	})

	var (
		applied   = make([]bool, len(tasks))
		completed int
		legacy    bool
		batch     []Completed
		counts    = Progress{Total: len(files), Missing: missing, Error: errorsPre}
	)

	tick := func() {
		batch = batch[:0]
		for pos := range tasks {
			if applied[pos] || !eng.Done(pos) {
				continue
			}
			applied[pos] = true
			completed++
			r := eng.Result(pos)
			fileIdx := int(tasks[pos].Index)
			hash := eng.Hash(pos)
			status := model.OK
			switch {
			case r.Err != nil:
				status = model.Error
				counts.Error++
				report.Problems = append(report.Problems, Problem{Name: files[fileIdx].FullPath(), Status: model.Error, Err: r.Err})
			case !verify:
				counts.OK++
			default:
				expected := files[fileIdx].Expected
				if expected == nil || bytes.Equal(hash, expected) {
					counts.OK++
				} else if algo == hasher.CRC32 && len(expected) == 4 && len(hash) == 4 &&
					hash[0] == expected[3] && hash[1] == expected[2] &&
					hash[2] == expected[1] && hash[3] == expected[0] {
					// Legacy SFV tools wrote CRC-32 in the other byte
					// order.
					counts.OK++
					legacy = true
				} else {
					status = model.Bad
					counts.Bad++
					report.Problems = append(report.Problems, Problem{Name: files[fileIdx].FullPath(), Status: model.Bad})
				}
			}
			batch = append(batch, Completed{
				Index:   fileIdx,
				Status:  status,
				Hash:    hash,
				Elapsed: r.Elapsed,
				Err:     r.Err,
			})
		}
		p := progressFromEngine(eng, counts, missing+errorsPre, completed)
		p.Elapsed = time.Since(start)
		report.Progress = p
		if opt.OnTick != nil {
			opt.OnTick(p, batch)
		}
	}

	stop := make(chan struct{})
	stopped := make(chan struct{})
	go func() {
		defer close(stopped)
		t := time.NewTicker(opt.Tick)
		defer t.Stop()
		for {
			select {
			case <-stop:
				return
			case <-t.C:
				tick()
			}
		}
	}()

	eng.Run(ctx)
	close(stop)
	<-stopped
	tick()

	report.Legacy = legacy
	report.Workers = eng.Workers()
	report.ParallelCRC = eng.UsedCRCParallel()
	report.Progress.Done = completed + missing + errorsPre
	report.Progress.OK = counts.OK
	report.Progress.Bad = counts.Bad
	report.Progress.Missing = missing
	report.Progress.Error = counts.Error
	report.Progress.Elapsed = time.Since(start)
	slices.SortFunc(report.Problems, func(a, b Problem) int { return comparePath(a.Name, b.Name) })
	return report, nil
}

func progressFromEngine(eng *engine.Engine, counts Progress, missing, completed int) Progress {
	p := eng.Progress()
	counts.Done = completed + missing
	counts.BytesDone = p.BytesDone
	counts.BytesTotal = p.BytesTotal
	counts.SpeedBPS = p.SpeedBPS
	counts.ETA = p.ETA
	counts.Elapsed = p.Elapsed
	return counts
}

func withDefaults(opt Options) Options {
	if opt.Tick <= 0 {
		opt.Tick = 100 * time.Millisecond
	}
	return opt
}

// IsCanceled reports whether err is a context cancellation.
func IsCanceled(err error) bool { return errors.Is(err, context.Canceled) }
