package hasher

import (
	"testing"
)

var benchData = make([]byte, 8<<20)

func BenchmarkAlgorithms(b *testing.B) {
	for _, k := range All {
		b.Run(k.String(), func(b *testing.B) {
			h := k.New()
			buf := make([]byte, 0, k.Size())
			b.SetBytes(int64(len(benchData)))
			b.ReportAllocs()
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				h.Reset()
				_, _ = h.Write(benchData)
				buf = h.Sum(buf[:0])
			}
			if len(buf) != k.Size() {
				b.Fatalf("digest size %d", len(buf))
			}
		})
	}
}
