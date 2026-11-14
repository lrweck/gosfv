package fio

import (
	"bytes"
	"os"
	"path/filepath"
	"sync"
	"testing"
)

func TestReadAtConcurrent(t *testing.T) {
	dir := t.TempDir()
	data := make([]byte, 1<<20)
	for i := range data {
		data[i] = byte(i * 31)
	}
	path := filepath.Join(dir, "chunks.bin")
	if err := os.WriteFile(path, data, 0o644); err != nil {
		t.Fatal(err)
	}
	f, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	if f.Size() != int64(len(data)) {
		t.Fatalf("size = %d", f.Size())
	}

	const chunks = 8
	var wg sync.WaitGroup
	errs := make(chan error, chunks)
	for c := 0; c < chunks; c++ {
		c := c
		wg.Add(1)
		go func() {
			defer wg.Done()
			size := len(data) / chunks
			off := c * size
			end := off + size
			if c == chunks-1 {
				end = len(data)
			}
			buf := make([]byte, end-off)
			if _, err := f.ReadAt(buf, int64(off)); err != nil {
				errs <- err
				return
			}
			if !bytes.Equal(buf, data[off:end]) {
				errs <- os.ErrInvalid
			}
		}()
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		t.Fatal(err)
	}
}
