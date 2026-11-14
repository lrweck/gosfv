package engine

import (
	"context"
	"hash"
	"io"
	"os"
)

// hashPipelined reads the file on a goroutine while the caller hashes the
// previous block. The two buffers stay in flight in a strict ping-pong:
// the reader never runs more than one read ahead, so a slow hash becomes
// backpressure instead of unbounded memory.
//
// The digest is written by the caller's goroutine only; hash.Hash is not
// shared. The reader owns f for its whole life, and the deferred wait
// guarantees the file is not closed under it.
func (e *Engine) hashPipelined(ctx context.Context, h hash.Hash, t *Task, buf []byte, pos int) (int64, error) {
	f, err := os.Open(t.Path)
	if err != nil {
		return 0, err
	}
	defer f.Close()

	size := t.Size
	report := e.opt.OnFileProgress != nil && size >= LargeFileThreshold
	h.Reset()

	ctx, cancel := context.WithCancel(ctx)
	defer cancel()

	free := make(chan []byte, 2)
	ready := make(chan []byte, 1)
	errc := make(chan error, 1)
	readerDone := make(chan struct{})
	free <- buf
	free <- make([]byte, len(buf))

	go func() {
		defer close(readerDone)
		defer close(ready)
		for {
			if err := e.opt.Gate.Wait(ctx); err != nil {
				errc <- err
				return
			}
			var b []byte
			select {
			case b = <-free:
			case <-ctx.Done():
				errc <- ctx.Err()
				return
			}
			n, rerr := f.Read(b)
			if n > 0 {
				select {
				case ready <- b[:n]:
				case <-ctx.Done():
					errc <- ctx.Err()
					return
				}
			}
			if rerr == io.EOF {
				return
			}
			if rerr != nil {
				errc <- rerr
				return
			}
			if n == 0 {
				return
			}
		}
	}()
	defer func() {
		cancel()
		<-readerDone
	}()

	var read, lastReport int64
	for b := range ready {
		if err := e.opt.Gate.Wait(ctx); err != nil {
			return read, err
		}
		if _, werr := h.Write(b); werr != nil {
			return read, werr
		}
		read += int64(len(b))
		e.processed.Add(int64(len(b)))
		if report && read-lastReport >= 1<<20 {
			lastReport = read
			e.opt.OnFileProgress(t.Index, t.Path, read, size)
		}
		// Two buffers exist, so with one in hand this send always fits.
		free <- b[:cap(b)]
	}
	select {
	case err := <-errc:
		return read, err
	default:
	}

	slot := e.arena[pos*e.opt.HashSize : (pos+1)*e.opt.HashSize]
	sum := h.Sum(slot[:0])
	if len(sum) != e.opt.HashSize {
		copy(slot, sum)
	}
	if report {
		e.opt.OnFileProgress(t.Index, t.Path, size, size)
	}
	return read, nil
}
