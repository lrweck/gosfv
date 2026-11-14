package engine

import (
	"context"
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"gosfv/internal/hasher"
)

func benchCRCFile(b *testing.B, workers int) {
	dir := b.TempDir()
	data := make([]byte, 64<<20)
	path := filepath.Join(dir, "crc.bin")
	if err := os.WriteFile(path, data, 0o644); err != nil {
		b.Fatal(err)
	}
	b.SetBytes(int64(len(data)))
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		e := New([]Task{{Index: 0, Path: path, Size: int64(len(data))}}, Options{
			Kind: hasher.CRC32, Workers: workers, CRCChunkSize: 1 << 20,
		})
		e.Run(context.Background())
		if err := e.Result(0).Err; err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkCRC32OneWorker(b *testing.B) { benchCRCFile(b, 1) }

func BenchmarkCRC32AllCores(b *testing.B) {
	benchCRCFile(b, runtime.GOMAXPROCS(0))
}
