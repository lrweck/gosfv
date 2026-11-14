package engine

import (
	"context"
	"os"
	"strconv"
	"testing"

	"github.com/lrweck/gosfv/internal/hasher"
)

// BenchmarkPipeline compares the serial read-then-hash loop with the
// two-buffer overlap on the same file. Set GOSFV_BENCH_FILE to a file of at
// least 16 MiB for the pipeline to apply.
func BenchmarkPipeline(b *testing.B) {
	path := os.Getenv("GOSFV_BENCH_FILE")
	if path == "" {
		b.Skip("set GOSFV_BENCH_FILE to a large file")
	}
	info, err := os.Stat(path)
	if err != nil {
		b.Fatal(err)
	}
	size := info.Size()
	if size < pipelineMinSize {
		b.Fatalf("%s is smaller than the %d-byte pipeline threshold", path, pipelineMinSize)
	}
	for _, kind := range []hasher.Kind{hasher.MD5, hasher.SHA1, hasher.SHA256} {
		for _, pipe := range []bool{false, true} {
			b.Run(kind.String()+"/pipeline="+strconv.FormatBool(pipe), func(b *testing.B) {
				b.SetBytes(size)
				b.ReportAllocs()
				for i := 0; i < b.N; i++ {
					e := New([]Task{{Index: 0, Path: path, Size: size}}, Options{
						Kind:     kind,
						Pipeline: pipe,
					})
					e.Run(context.Background())
					if err := e.Result(0).Err; err != nil {
						b.Fatal(err)
					}
				}
			})
		}
	}
}
