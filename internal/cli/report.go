package cli

import (
	"fmt"
	"time"

	"github.com/lrweck/gosfv/internal/hasher"
	"github.com/lrweck/gosfv/internal/model"
	"github.com/lrweck/gosfv/internal/runner"
)

// applyResults copies the finished files of a tick into the list that the
// checksum writer will read. It is the only place that translates a
// runner.Completed into a model.File, so create and verify cannot drift.
func applyResults(files []model.File, done []runner.Completed) {
	for _, d := range done {
		if d.Index < 0 || d.Index >= len(files) {
			continue
		}
		f := &files[d.Index]
		if d.Status == model.Error {
			f.Hash = nil
		} else {
			f.Hash = d.Hash
		}
		f.Status = d.Status
		f.Elapsed = d.Elapsed
	}
}

func printSummary(rep *runner.Report, elapsed time.Duration) {
	p := rep.Progress
	fmt.Println("------------------------------------------------")
	if rep.Workers > 0 {
		line := fmt.Sprintf("Worker pool: %d", rep.Workers)
		if rep.ParallelCRC {
			line += fmt.Sprintf(" (CRC-32 file split across %d workers)", rep.CRCWorkers)
		}
		fmt.Println(line)
	}
	if rep.Workers > 1 && !rep.ParallelCRC && rep.Progress.Total < rep.Workers && rep.Algo != hasher.CRC32 {
		fmt.Printf("Note: %d file(s) on a %d-worker pool; %s is a sequential digest, so each file uses one worker.\n",
			rep.Progress.Total, rep.Workers, rep.Algo.ShortLabel())
	}
	fmt.Printf("Total: %d\n", p.Total)
	fmt.Printf("OK: %d\n", p.OK)
	fmt.Printf("BAD: %d\n", p.Bad)
	fmt.Printf("MISSING: %d\n", p.Missing)
	fmt.Printf("ERROR: %d\n", p.Error)
	fmt.Printf("Time: %d ms\n", elapsed.Milliseconds())
	if rep.Legacy {
		fmt.Println("Note: legacy SFV byte order detected and accepted.")
	}
	fmt.Println("------------------------------------------------")
}

func printProblems(rep *runner.Report) {
	if len(rep.Problems) == 0 {
		return
	}
	fmt.Println("Problems:")
	for i, p := range rep.Problems {
		if i == 100 {
			fmt.Printf("  ... and %d more\n", len(rep.Problems)-i)
			break
		}
		if p.Err != nil {
			fmt.Printf("  %-7s %s: %v\n", p.Status, p.Name, p.Err)
		} else {
			fmt.Printf("  %-7s %s\n", p.Status, p.Name)
		}
	}
}

func exitCode(rep *runner.Report) int {
	p := rep.Progress
	if p.Bad > 0 || p.Missing > 0 || p.Error > 0 || p.Done < p.Total {
		return 1
	}
	return 0
}
