package hexfmt

import (
	"testing"
)

func BenchmarkEncodeUpper32(b *testing.B) {
	src := make([]byte, 32)
	dst := make([]byte, 64)
	b.SetBytes(32)
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		EncodeUpper(dst, src)
	}
}

func BenchmarkDecode64(b *testing.B) {
	src := []byte("D41D8CD98F00B204E9800998ECF8427E900150983CD24FB0D6963F7D28E17F72")
	dst := make([]byte, 0, 32)
	b.SetBytes(64)
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		out, ok := Decode(dst[:0], src)
		if !ok || len(out) != 32 {
			b.Fatal("decode failed")
		}
	}
}

func TestBackend(t *testing.T) {
	if Backend() == "" {
		t.Fatal("Backend is empty")
	}
	t.Logf("hex codec backend: %s", Backend())
}
