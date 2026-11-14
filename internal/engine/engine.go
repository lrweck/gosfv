// Package engine hashes a slice of files with a fixed pool of workers.
//
// The hot path is allocation free. Each worker owns one hasher and one read
// buffer for its whole life; digests are written into a caller-provided arena;
// results live in a preallocated slice; and work is handed out with one atomic
// increment per file. Bytes are counted atomically, so a UI can sample progress
// without touching worker state.
//
// The algorithms themselves are hardware accelerated: xxHash3-128 in assembly
// (AVX2/AVX-512), CRC-32 with PCLMULQDQ on amd64 and CRC instructions on arm64,
// and MD5/SHA-1/SHA-256 with the Go runtime's block routines, including SHA-NI
// where the CPU provides it.
package engine

import (
	"context"
	"hash"
	"io"
	"os"
	"runtime"
	"sync"
	"sync/atomic"
	"time"

	"gosfv/internal/hasher"
)

// LargeFileThreshold is when per-file progress is reported, matching the
// Active Jobs panel of SharpSFV.
const LargeFileThreshold = 50 << 20

// Task is one file to hash.
type Task struct {
	// Index is the caller's identity for the file. It is passed back by
	// OnFileProgress; results are stored by position in the tasks slice.
	Index int32
	Path  string
	Size  int64
}

// Result is the outcome of one task.
type Result struct {
	Err     error
	Elapsed time.Duration
	Bytes   int64
}

// Progress is a consistency snapshot for the UI.
type Progress struct {
	Done       int
	Total      int
	BytesDone  int64
	BytesTotal int64
	SpeedBPS   float64
	ETA        time.Duration
	Elapsed    time.Duration
}

// Gate pauses and resumes all workers between reads.
type Gate struct {
	mu     sync.Mutex
	paused bool
	resume chan struct{}
}

// NewGate returns a running gate.
func NewGate() *Gate { return &Gate{resume: make(chan struct{})} }

// Wait blocks while the gate is paused.
func (g *Gate) Wait(ctx context.Context) error {
	if g == nil {
		return ctx.Err()
	}
	for {
		g.mu.Lock()
		paused, ch := g.paused, g.resume
		g.mu.Unlock()
		if !paused {
			return ctx.Err()
		}
		select {
		case <-ch:
		case <-ctx.Done():
			return ctx.Err()
		}
	}
}

// Set pauses or resumes.
func (g *Gate) Set(paused bool) {
	if g == nil {
		return
	}
	g.mu.Lock()
	defer g.mu.Unlock()
	if g.paused == paused {
		return
	}
	g.paused = paused
	if !paused {
		close(g.resume)
		g.resume = make(chan struct{})
	}
}

// Paused reports the gate's state.
func (g *Gate) Paused() bool {
	if g == nil {
		return false
	}
	g.mu.Lock()
	defer g.mu.Unlock()
	return g.paused
}

// Options configures an Engine.
type Options struct {
	Kind hasher.Kind
	// Sequential forces one worker, for mechanical disks and network shares.
	Sequential bool
	// Workers overrides the worker count; 0 derives it from the CPU count.
	Workers int
	// BufferSize overrides the per-worker read buffer.
	BufferSize int
	Gate       *Gate
	// HashArena receives every digest: task i's digest is
	// HashArena[i*HashSize:(i+1)*HashSize]. If nil, New allocates it.
	HashArena []byte
	HashSize  int
	// OnFileProgress is called for files at least LargeFileThreshold bytes
	// long, at about 1 MiB intervals. Path is the task's path, so callers
	// need not share mutable file state with workers.
	OnFileProgress func(index int32, path string, done, total int64)
	// CRCChunkSize is the minimum size of a CRC-32 chunk when one file is
	// split across workers. Zero uses DefaultCRCChunkSize (16 MiB). If a
	// file has fewer chunks than workers, it uses fewer workers instead of
	// shrinking the chunks further.
	CRCChunkSize int64
}

// Engine hashes tasks.
type Engine struct {
	tasks []Task
	opt   Options
	buf   int
	arena []byte

	results   []Result
	done      []atomic.Uint32
	completed atomic.Int64
	processed atomic.Int64
	total     int64
	next      atomic.Int64
	start     time.Time

	// usedCRCParallel is set when a CRC-32 run split files into chunks.
	usedCRCParallel bool
	crcWorkers      int
}

// New prepares an engine. Tasks must not be mutated until Run returns.
func New(tasks []Task, opt Options) *Engine {
	size := opt.HashSize
	if size == 0 {
		size = opt.Kind.Size()
	}
	opt.HashSize = size
	if len(opt.HashArena) < len(tasks)*size {
		opt.HashArena = make([]byte, len(tasks)*size)
	}
	workers := workerCount(opt)
	buf := opt.BufferSize
	if buf == 0 {
		buf = defaultBuffer(opt.Sequential, workers)
	}
	var total int64
	for i := range tasks {
		if tasks[i].Size > 0 {
			total += tasks[i].Size
		}
	}
	return &Engine{
		tasks:   tasks,
		opt:     opt,
		buf:     buf,
		arena:   opt.HashArena,
		results: make([]Result, len(tasks)),
		done:    make([]atomic.Uint32, len(tasks)),
		total:   total,
		start:   time.Now(),
	}
}

// HashArena returns the digest arena, for callers that let New allocate it.
func (e *Engine) HashArena() []byte { return e.arena }

// Workers returns the number of hashing workers this run uses.
func (e *Engine) Workers() int { return workerCount(e.opt) }

// UsedCRCParallel reports whether a CRC-32 run split files into chunks so that
// one file used several workers.
func (e *Engine) UsedCRCParallel() bool { return e.usedCRCParallel }

// CRCWorkers returns how many workers actually hashed chunks in a split
// CRC-32 run. It is 0 when the normal one-file-per-worker path ran.
func (e *Engine) CRCWorkers() int { return e.crcWorkers }

// Done reports whether task i finished (successfully or not).
func (e *Engine) Done(i int) bool { return e.done[i].Load() == 1 }

// Result returns task i's result once Done(i) is true.
func (e *Engine) Result(i int) Result { return e.results[i] }

// Hash returns task i's digest, a slice of the arena.
func (e *Engine) Hash(i int) []byte {
	size := e.opt.HashSize
	return e.arena[i*size : (i+1)*size]
}

// Progress snapshots the counters.
func (e *Engine) Progress() Progress {
	done := int(e.completed.Load())
	bytesDone := e.processed.Load()
	elapsed := time.Since(e.start)
	p := Progress{
		Done:       done,
		Total:      len(e.tasks),
		BytesDone:  bytesDone,
		BytesTotal: e.total,
		Elapsed:    elapsed,
	}
	if elapsed > 0 {
		p.SpeedBPS = float64(bytesDone) / elapsed.Seconds()
	}
	if p.SpeedBPS > 0 && e.total > bytesDone {
		seconds := float64(e.total-bytesDone) / p.SpeedBPS
		if seconds < 86400 {
			p.ETA = time.Duration(seconds * float64(time.Second))
		}
	}
	return p
}

// Run hashes every task and blocks until all workers stop. Cancelling ctx stops
// workers between reads; tasks not started stay unfinished.
func (e *Engine) Run(ctx context.Context) {
	if e.opt.Kind == hasher.CRC32 && e.runCRCParallel(ctx) {
		return
	}
	workers := workerCount(e.opt)
	var wg sync.WaitGroup
	wg.Add(workers)
	for w := 0; w < workers; w++ {
		go func() {
			defer wg.Done()
			e.worker(ctx)
		}()
	}
	wg.Wait()
}

func (e *Engine) worker(ctx context.Context) {
	h := e.opt.Kind.New()
	if h == nil {
		return
	}
	buf := make([]byte, e.buf)
	for {
		i := int(e.next.Add(1)) - 1
		if i >= len(e.tasks) {
			return
		}
		if ctx.Err() != nil {
			return
		}
		if err := e.opt.Gate.Wait(ctx); err != nil {
			return
		}
		t := &e.tasks[i]
		start := time.Now()
		n, err := e.hashOne(ctx, h, t, buf, i)
		e.results[i] = Result{Err: err, Elapsed: time.Since(start), Bytes: n}
		e.done[i].Store(1)
		e.completed.Add(1)
	}
}

func (e *Engine) hashOne(ctx context.Context, h hash.Hash, t *Task, buf []byte, pos int) (int64, error) {
	f, err := os.Open(t.Path)
	if err != nil {
		return 0, err
	}
	defer f.Close()

	size := t.Size
	if size <= 0 {
		if info, statErr := f.Stat(); statErr == nil {
			size = info.Size()
		}
	}
	report := e.opt.OnFileProgress != nil && size >= LargeFileThreshold
	h.Reset()

	var read, lastReport int64
	for {
		if err := e.opt.Gate.Wait(ctx); err != nil {
			return read, err
		}
		n, err := f.Read(buf)
		if n > 0 {
			if _, werr := h.Write(buf[:n]); werr != nil {
				return read, werr
			}
			read += int64(n)
			e.processed.Add(int64(n))
			if report && read-lastReport >= 1<<20 {
				lastReport = read
				e.opt.OnFileProgress(t.Index, t.Path, read, size)
			}
		}
		if err == io.EOF {
			break
		}
		if err != nil {
			return read, err
		}
		if n == 0 {
			break
		}
	}

	slot := e.arena[pos*e.opt.HashSize : (pos+1)*e.opt.HashSize]
	sum := h.Sum(slot[:0])
	if len(sum) != e.opt.HashSize {
		// A hasher returning another size is a programming error; keep the
		// first bytes rather than panic in a worker.
		copy(slot, sum)
	}
	if report {
		e.opt.OnFileProgress(t.Index, t.Path, size, size)
	}
	return read, nil
}

func workerCount(opt Options) int {
	if opt.Sequential {
		return 1
	}
	n := opt.Workers
	if n <= 0 {
		// GOMAXPROCS follows the machine and, in Go 1.27, container CPU
		// limits. NumCPU can over-subscribe a container.
		n = runtime.GOMAXPROCS(0)
	}
	if n < 1 {
		n = 1
	}
	return n
}

func defaultBuffer(sequential bool, workers int) int {
	if sequential {
		return 8 << 20
	}
	b := (16 << 20) / workers
	if b < 256<<10 {
		b = 256 << 10
	}
	if b > 2<<20 {
		b = 2 << 20
	}
	return b
}
