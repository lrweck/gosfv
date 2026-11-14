package engine

import (
	"context"
	"encoding/binary"
	"hash/crc32"
	"io"
	"sync"
	"sync/atomic"
	"time"

	"gosfv/internal/fio"
)

// DefaultCRCChunkSize is the minimum size of a CRC-32 chunk. An 8 MiB
// chunk is about 0.3 ms of CRC-32 work on this reference machine, which is
// large enough that opening a handle and scheduling a goroutine do not
// dominate, and small enough that a 20 MiB file still uses two workers.
// A file that has fewer chunks than workers uses fewer workers, rather than
// making the chunks smaller.
const DefaultCRCChunkSize = 8 << 20

type crcChunk struct {
	task   int
	part   int
	offset int64
	length int64
}

type crcPart struct {
	crc    uint32
	length int64
}

// runCRCParallel hashes one or a few CRC-32 files with more workers than files,
// splitting each file into chunks and combining the block checksums. It returns
// false when the normal one-file-per-worker pool is a better fit.
//
// MD5, SHA-1, SHA-256 and xxHash3 cannot use this path: their digests are
// sequential, and combining independent piece hashes would produce a value no
// other tool recognizes. CRC-32 is linear, so it can.
func (e *Engine) runCRCParallel(ctx context.Context) bool {
	workers := workerCount(e.opt)
	n := len(e.tasks)
	if n == 0 || workers <= n {
		return false
	}

	chunkSize := e.opt.CRCChunkSize
	if chunkSize <= 0 {
		chunkSize = DefaultCRCChunkSize
	}
	maxChunks := make([]int, n)
	for i := range e.tasks {
		maxChunks[i] = int(e.tasks[i].Size / chunkSize)
		if maxChunks[i] < 1 {
			maxChunks[i] = 1
		}
	}
	chunks := make([]int, n)
	total := 0
	for i := range chunks {
		c := workers / n
		if c < 1 {
			c = 1
		}
		if c > maxChunks[i] {
			c = maxChunks[i]
		}
		chunks[i] = c
		total += c
	}
	if total <= n {
		return false
	}
	// Hand the leftover workers to files that can use another chunk.
	for total < workers {
		added := false
		for i := range chunks {
			if chunks[i] < maxChunks[i] {
				chunks[i]++
				total++
				added = true
				if total >= workers {
					break
				}
			}
		}
		if !added {
			break
		}
	}
	if total <= n {
		return false
	}
	e.usedCRCParallel = true

	plans := make([]crcChunk, 0, total)
	parts := make([][]crcPart, n)
	remaining := make([]atomic.Int32, n)
	readBytes := make([]atomic.Int64, n)
	for i, c := range chunks {
		parts[i] = make([]crcPart, c)
		remaining[i].Store(int32(c))
		size := e.tasks[i].Size
		base := size / int64(c)
		rem := size % int64(c)
		var off int64
		for p := 0; p < c; p++ {
			length := base
			if int64(p) < rem {
				length++
			}
			if p == c-1 {
				length = size - off
			}
			plans = append(plans, crcChunk{task: i, part: p, offset: off, length: length})
			off += length
		}
	}

	var (
		next    atomic.Int64
		wg      sync.WaitGroup
		errMu   sync.Mutex
		errs    = make([]error, n)
		report  = e.opt.OnFileProgress != nil
		workNum = workers
	)
	if workNum > len(plans) {
		workNum = len(plans)
	}
	wg.Add(workNum)
	for w := 0; w < workNum; w++ {
		go func() {
			defer wg.Done()
			buf := make([]byte, e.buf)
			for {
				j := int(next.Add(1)) - 1
				if j >= len(plans) {
					return
				}
				ch := plans[j]
				t := &e.tasks[ch.task]
				f, err := fio.Open(t.Path)
				if err != nil {
					errMu.Lock()
					if errs[ch.task] == nil {
						errs[ch.task] = err
					}
					errMu.Unlock()
					e.finishCRCChunk(ch.task, ch.part, crcPart{}, parts, remaining, errs, &errMu, report)
					continue
				}
				crc := uint32(0)
				var read int64
				for read < ch.length {
					if err := e.opt.Gate.Wait(ctx); err != nil {
						_ = f.Close()
						return
					}
					want := int64(len(buf))
					if ch.length-read < want {
						want = ch.length - read
					}
					nread, rerr := f.ReadAt(buf[:want], ch.offset+read)
					if nread > 0 {
						crc = crc32.Update(crc, crc32.IEEETable, buf[:nread])
						read += int64(nread)
						e.processed.Add(int64(nread))
						if report && t.Size >= LargeFileThreshold {
							done := readBytes[ch.task].Add(int64(nread))
							e.opt.OnFileProgress(t.Index, t.Path, done, t.Size)
						}
					}
					if rerr == io.EOF {
						break
					}
					if rerr != nil {
						errMu.Lock()
						if errs[ch.task] == nil {
							errs[ch.task] = rerr
						}
						errMu.Unlock()
						break
					}
					if nread == 0 {
						break
					}
				}
				_ = f.Close()
				e.finishCRCChunk(ch.task, ch.part, crcPart{crc: crc, length: read}, parts, remaining, errs, &errMu, report)
			}
		}()
	}
	wg.Wait()
	return true
}

func (e *Engine) finishCRCChunk(task, part int, value crcPart, parts [][]crcPart, remaining []atomic.Int32, errs []error, errMu *sync.Mutex, report bool) {
	parts[task][part] = value
	if remaining[task].Add(-1) != 0 {
		return
	}
	errMu.Lock()
	err := errs[task]
	errMu.Unlock()

	t := &e.tasks[task]
	res := Result{Bytes: t.Size, Elapsed: time.Since(e.start)}
	if err == nil {
		crc := parts[task][0].crc
		for p := 1; p < len(parts[task]); p++ {
			crc = combineCRC32(crc, parts[task][p].crc, parts[task][p].length)
		}
		binary.BigEndian.PutUint32(e.arena[task*4:(task+1)*4], crc)
	} else {
		res.Err = err
	}
	e.results[task] = res
	e.done[task].Store(1)
	e.completed.Add(1)
	if report && err == nil {
		e.opt.OnFileProgress(t.Index, t.Path, t.Size, t.Size)
	}
}
