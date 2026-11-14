package cli

import (
	"fmt"
	"os"
	"time"

	"github.com/schollz/progressbar/v3"

	"gosfv/internal/model"
	"gosfv/internal/runner"
)

// progressUI is the terminal presentation. Progress goes to stderr so that
// `GoSFV -headless ... > report.txt` still captures a clean summary on stdout.
type progressUI struct {
	enabled    bool
	throughput bool
	mode       string
	scan       *progressbar.ProgressBar
	bar        *progressbar.ProgressBar
	totalBytes int64
	lastBytes  int64
	lastDone   int
	useBytes   bool
}

func newProgressUI(enabled, throughput bool, mode string) *progressUI {
	return &progressUI{enabled: enabled, throughput: throughput, mode: mode}
}

func isTerminal(f *os.File) bool {
	fi, err := f.Stat()
	if err != nil {
		return false
	}
	return fi.Mode()&os.ModeCharDevice != 0
}

func (p *progressUI) startScan() {
	if !p.enabled {
		return
	}
	p.scan = progressbar.NewOptions64(-1,
		progressbar.OptionSetDescription("Scanning"),
		progressbar.OptionSpinnerType(14),
		progressbar.OptionSetWidth(18),
		progressbar.OptionSetWriter(os.Stderr),
		progressbar.OptionThrottle(120*time.Millisecond),
		progressbar.OptionClearOnFinish(),
	)
}

func (p *progressUI) scanned(found int) {
	if p.scan != nil {
		p.scan.Describe(fmt.Sprintf("Scanning... %d files", found))
	}
}

func (p *progressUI) startFiles(files []model.File) {
	if p.scan != nil {
		_ = p.scan.Finish()
		p.scan = nil
	}
	if !p.enabled || len(files) == 0 {
		return
	}
	for i := range files {
		if files[i].Size > 0 {
			p.totalBytes += files[i].Size
		}
	}
	opts := []progressbar.Option{
		progressbar.OptionSetDescription(p.mode),
		progressbar.OptionSetWidth(36),
		progressbar.OptionSetWriter(os.Stderr),
		progressbar.OptionThrottle(100 * time.Millisecond),
		progressbar.OptionSetPredictTime(p.throughput),
		progressbar.OptionShowElapsedTimeOnFinish(),
		progressbar.OptionClearOnFinish(),
		progressbar.OptionSetRenderBlankState(true),
	}
	if p.totalBytes > 0 {
		p.useBytes = true
		opts = append(opts,
			progressbar.OptionShowBytes(true),
			progressbar.OptionShowTotalBytes(true),
		)
		p.bar = progressbar.NewOptions64(p.totalBytes, opts...)
	} else {
		opts = append(opts,
			progressbar.OptionShowCount(),
			progressbar.OptionSetItsString("files"),
		)
		p.bar = progressbar.NewOptions64(int64(len(files)), opts...)
	}
}

func (p *progressUI) tick(prog runner.Progress) {
	if p.bar == nil {
		return
	}
	if p.useBytes {
		delta := prog.BytesDone - p.lastBytes
		if delta > 0 {
			_ = p.bar.Add64(delta)
			p.lastBytes = prog.BytesDone
		}
	} else if prog.Done > p.lastDone {
		_ = p.bar.Add(prog.Done - p.lastDone)
		p.lastDone = prog.Done
	}
	p.bar.Describe(fmt.Sprintf("%s  OK:%d BAD:%d MISSING:%d", p.mode, prog.OK, prog.Bad, prog.Missing))
}

func (p *progressUI) finish() {
	if p.scan != nil {
		_ = p.scan.Finish()
		p.scan = nil
	}
	if p.bar != nil {
		_ = p.bar.Finish()
		p.bar = nil
	}
}
