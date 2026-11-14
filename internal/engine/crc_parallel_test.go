package engine

import (
	"bytes"
	"context"
	"hash/crc32"
	"math/rand"
	"os"
	"path/filepath"
	"testing"

	"gosfv/internal/hasher"
)

func TestParallelCRCSingleFile(t *testing.T) {
	dir := t.TempDir()
	data := make([]byte, 20<<20)
	rand.New(rand.NewSource(11)).Read(data)
	path := filepath.Join(dir, "big.bin")
	if err := os.WriteFile(path, data, 0o644); err != nil {
		t.Fatal(err)
	}
	e := New([]Task{{Index: 0, Path: path, Size: int64(len(data))}}, Options{
		Kind: hasher.CRC32, Workers: 4, CRCChunkSize: 1 << 20,
	})
	e.Run(context.Background())
	if !e.usedCRCParallel {
		t.Fatal("single CRC file was not split across workers")
	}
	if err := e.Result(0).Err; err != nil {
		t.Fatal(err)
	}
	want := crc32.ChecksumIEEE(data)
	if !bytes.Equal(e.Hash(0), []byte{byte(want >> 24), byte(want >> 16), byte(want >> 8), byte(want)}) {
		t.Fatalf("hash = %x, want %08x", e.Hash(0), want)
	}
	if p := e.Progress(); p.Done != 1 || p.BytesDone != int64(len(data)) {
		t.Errorf("progress = %+v", p)
	}
}

func TestParallelCRCManyFiles(t *testing.T) {
	dir := t.TempDir()
	var tasks []Task
	var all [][]byte
	for i := 0; i < 2; i++ {
		data := make([]byte, 12<<20)
		rand.New(rand.NewSource(int64(20 + i))).Read(data)
		path := filepath.Join(dir, string(rune('a'+i))+".bin")
		if err := os.WriteFile(path, data, 0o644); err != nil {
			t.Fatal(err)
		}
		tasks = append(tasks, Task{Index: int32(i), Path: path, Size: int64(len(data))})
		all = append(all, data)
	}
	e := New(tasks, Options{Kind: hasher.CRC32, Workers: 8, CRCChunkSize: 1 << 20})
	e.Run(context.Background())
	if !e.usedCRCParallel {
		t.Fatal("two CRC files were not split across workers")
	}
	for i := range tasks {
		want := crc32.ChecksumIEEE(all[i])
		if err := e.Result(i).Err; err != nil {
			t.Errorf("task %d: %v", i, err)
			continue
		}
		got := e.Hash(i)
		if !bytes.Equal(got, []byte{byte(want >> 24), byte(want >> 16), byte(want >> 8), byte(want)}) {
			t.Errorf("task %d hash = %x, want %08x", i, got, want)
		}
	}
}

func TestCRCDefaultChunkPolicy(t *testing.T) {
	dir := t.TempDir()
	makeFile := func(name string, size int) string {
		path := filepath.Join(dir, name)
		f, err := os.Create(path)
		if err != nil {
			t.Fatal(err)
		}
		if err := f.Truncate(int64(size)); err != nil {
			_ = f.Close()
			t.Fatal(err)
		}
		_ = f.Close()
		return path
	}
	// 8 MiB is one chunk: one worker.
	small := makeFile("small.bin", 8<<20)
	e := New([]Task{{Index: 0, Path: small, Size: 8 << 20}}, Options{Kind: hasher.CRC32, Workers: 4})
	e.Run(context.Background())
	if e.usedCRCParallel {
		t.Error("an 8 MiB file should not split with an 8 MiB minimum chunk")
	}
	// 20 MiB is two chunks: two workers.
	large := makeFile("large.bin", 20<<20)
	e = New([]Task{{Index: 0, Path: large, Size: 20 << 20}}, Options{Kind: hasher.CRC32, Workers: 4})
	e.Run(context.Background())
	if !e.usedCRCParallel {
		t.Error("a 20 MiB file should split into two chunks")
	}
	if err := e.Result(0).Err; err != nil {
		t.Fatal(err)
	}
}

func TestParallelCRCFallsBack(t *testing.T) {
	dir := t.TempDir()
	data := make([]byte, 512<<10) // below the default chunk: one chunk per file
	path := filepath.Join(dir, "small.bin")
	if err := os.WriteFile(path, data, 0o644); err != nil {
		t.Fatal(err)
	}
	e := New([]Task{{Index: 0, Path: path, Size: int64(len(data))}}, Options{
		Kind: hasher.CRC32, Workers: 8,
	})
	e.Run(context.Background())
	if e.usedCRCParallel {
		t.Fatal("a small file should use the normal worker path")
	}
	want := crc32.ChecksumIEEE(data)
	got := e.Hash(0)
	if !bytes.Equal(got, []byte{byte(want >> 24), byte(want >> 16), byte(want >> 8), byte(want)}) {
		t.Fatalf("hash = %x, want %08x", got, want)
	}
}
