package engine

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/lrweck/gosfv/internal/hasher"
)

// BenchmarkCRCChunkSizes answers one question: how large should a CRC-32
// chunk be before the overhead of a handle and a goroutine stops mattering?
// The file is created once in the parent benchmark's temp directory and is
// reused by every sub-benchmark.
func benchChunkSizes(b *testing.B, fileSize int64, sizes []int64) {
	path := filepath.Join(b.TempDir(), "chunks.bin")
	f, err := os.Create(path)
	if err != nil {
		b.Fatal(err)
	}
	if err := f.Truncate(fileSize); err != nil {
		_ = f.Close()
		b.Fatal(err)
	}
	_ = f.Close()

	workers := runtime.GOMAXPROCS(0)
	for _, mib := range sizes {
		b.Run(fmt.Sprintf("%dMiB", mib), func(b *testing.B) {
			b.SetBytes(fileSize)
			b.ReportAllocs()
			for i := 0; i < b.N; i++ {
				e := New([]Task{{Index: 0, Path: path, Size: fileSize}}, Options{
					Kind:         hasher.CRC32,
					Workers:      workers,
					CRCChunkSize: mib << 20,
				})
				e.Run(context.Background())
				if err := e.Result(0).Err; err != nil {
					b.Fatal(err)
				}
			}
		})
	}
}

func BenchmarkCRCChunkSizes512MiB(b *testing.B) {
	benchChunkSizes(b, 512<<20, []int64{1, 2, 4, 8, 16, 32, 64})
}

func BenchmarkCRCChunkSizes20MiB(b *testing.B) {
	benchChunkSizes(b, 20<<20, []int64{1, 2, 4, 8, 16})
}
