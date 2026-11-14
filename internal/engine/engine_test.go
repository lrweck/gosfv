package engine

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/lrweck/gosfv/internal/hasher"
	"github.com/lrweck/gosfv/internal/hexfmt"
)

func writeFile(t *testing.T, path string, data []byte) {
	t.Helper()
	if err := os.WriteFile(path, data, 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestHashAllAlgorithms(t *testing.T) {
	dir := t.TempDir()
	data := bytes.Repeat([]byte("GoSFV\x00\xff"), 10000)
	path := filepath.Join(dir, "file.bin")
	writeFile(t, path, data)

	for _, kind := range hasher.All {
		t.Run(kind.String(), func(t *testing.T) {
			e := New([]Task{{Index: 0, Path: path, Size: int64(len(data))}}, Options{Kind: kind})
			e.Run(context.Background())
			if !e.Done(0) {
				t.Fatal("task not done")
			}
			if err := e.Result(0).Err; err != nil {
				t.Fatal(err)
			}
			want := kind.New()
			_, _ = want.Write(data)
			wantSum := want.Sum(nil)
			if got := e.Hash(0); !bytes.Equal(got, wantSum) {
				t.Errorf("hash = %s, want %s", hexfmt.Upper(got), hexfmt.Upper(wantSum))
			}
		})
	}
}

func TestManyFilesAndProgress(t *testing.T) {
	dir := t.TempDir()
	tasks := make([]Task, 100)
	var total int64
	for i := range tasks {
		p := filepath.Join(dir, "f"+string(rune('a'+i%26))+string(rune('0'+i/26))+".bin")
		data := bytes.Repeat([]byte{byte(i)}, 1024+i)
		writeFile(t, p, data)
		tasks[i] = Task{Index: int32(i), Path: p, Size: int64(len(data))}
		total += int64(len(data))
	}
	e := New(tasks, Options{Kind: hasher.XXH3, Workers: 4, BufferSize: 4096})
	e.Run(context.Background())
	p := e.Progress()
	if p.Done != len(tasks) || p.Total != len(tasks) {
		t.Errorf("progress = %+v", p)
	}
	if p.BytesDone != total || p.BytesTotal != total {
		t.Errorf("bytes = %d/%d, want %d", p.BytesDone, p.BytesTotal, total)
	}
	for i := range tasks {
		if !e.Done(i) || e.Result(i).Err != nil {
			t.Errorf("task %d: done=%v err=%v", i, e.Done(i), e.Result(i).Err)
		}
		if len(e.Hash(i)) != hasher.XXH3.Size() {
			t.Errorf("task %d: hash size %d", i, len(e.Hash(i)))
		}
	}
}

func TestReadError(t *testing.T) {
	e := New([]Task{{Index: 0, Path: filepath.Join(t.TempDir(), "missing.bin")}}, Options{Kind: hasher.MD5})
	e.Run(context.Background())
	if !e.Done(0) {
		t.Fatal("task not done")
	}
	if e.Result(0).Err == nil {
		t.Fatal("expected an error")
	}
}

func TestCancel(t *testing.T) {
	dir := t.TempDir()
	tasks := make([]Task, 64)
	data := bytes.Repeat([]byte("x"), 1<<20)
	for i := range tasks {
		p := filepath.Join(dir, "f"+string(rune('a'+i%26))+string(rune('0'+i/26))+".bin")
		writeFile(t, p, data)
		tasks[i] = Task{Index: int32(i), Path: p, Size: int64(len(data))}
	}
	ctx, cancel := context.WithCancel(context.Background())
	e := New(tasks, Options{Kind: hasher.SHA256, Sequential: true})
	go func() {
		time.Sleep(2 * time.Millisecond)
		cancel()
	}()
	e.Run(ctx)
	if e.Progress().Done == len(tasks) {
		t.Fatal("all tasks completed despite cancellation")
	}
}

func TestGate(t *testing.T) {
	g := NewGate()
	g.Set(true)
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()
	if err := g.Wait(ctx); err == nil {
		t.Fatal("Wait returned while paused")
	}
	g.Set(false)
	if err := g.Wait(context.Background()); err != nil {
		t.Fatalf("Wait after resume: %v", err)
	}
	if g.Paused() {
		t.Fatal("gate still paused")
	}
}

func BenchmarkHashXXH3(b *testing.B) {
	dir := b.TempDir()
	data := make([]byte, 8<<20)
	path := filepath.Join(dir, "bench.bin")
	if err := os.WriteFile(path, data, 0o644); err != nil {
		b.Fatal(err)
	}
	b.SetBytes(int64(len(data)))
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		e := New([]Task{{Index: 0, Path: path, Size: int64(len(data))}}, Options{
			Kind: hasher.XXH3, Sequential: true, BufferSize: 8 << 20,
		})
		e.Run(context.Background())
		if e.Result(0).Err != nil {
			b.Fatal(e.Result(0).Err)
		}
	}
}
