package engine

import (
	"context"
	"os"
	"testing"

	"github.com/lrweck/gosfv/internal/hasher"
)

// BenchmarkFixedFile hashes one large file with the engine's default worker
// count and buffer sizing, the path the app uses on an SSD. Set
// GOSFV_BENCH_FILE to a file of at least a few hundred MiB.
func BenchmarkFixedFile(b *testing.B) {
	path := os.Getenv("GOSFV_BENCH_FILE")
	if path == "" {
		b.Skip("set GOSFV_BENCH_FILE to a large file")
	}
	info, err := os.Stat(path)
	if err != nil {
		b.Fatal(err)
	}
	size := info.Size()
	if size < 8<<20 {
		b.Fatalf("%s is only %d bytes", path, size)
	}
	for _, kind := range []hasher.Kind{hasher.XXH3, hasher.CRC32, hasher.SHA256} {
		b.Run(kind.String(), func(b *testing.B) {
			b.SetBytes(size)
			b.ReportAllocs()
			for i := 0; i < b.N; i++ {
				e := New([]Task{{Index: 0, Path: path, Size: size}}, Options{
					Kind: kind,
				})
				e.Run(context.Background())
				if err := e.Result(0).Err; err != nil {
					b.Fatal(err)
				}
			}
		})
	}
}
