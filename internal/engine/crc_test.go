package engine

import (
	"hash/crc32"
	"math/rand"
	"testing"
)

func TestCombineCRC32(t *testing.T) {
	r := rand.New(rand.NewSource(7))
	for _, size := range []int{0, 1, 2, 17, 4096, 1 << 20} {
		data := make([]byte, size)
		r.Read(data)
		full := crc32.ChecksumIEEE(data)
		for _, split := range []int{0, 1, size / 3, size / 2, size - 1, size} {
			if split < 0 || split > size {
				continue
			}
			a := crc32.ChecksumIEEE(data[:split])
			b := crc32.ChecksumIEEE(data[split:])
			got := combineCRC32(a, b, int64(size-split))
			if got != full {
				t.Fatalf("size=%d split=%d: combine=%08x want %08x", size, split, got, full)
			}
		}
	}
}

func TestCombineCRC32ManyChunks(t *testing.T) {
	data := make([]byte, 3<<20)
	rand.New(rand.NewSource(9)).Read(data)
	full := crc32.ChecksumIEEE(data)
	crc := uint32(0)
	pos := 0
	for _, n := range []int{1, 4095, 65536, 1 << 20, len(data)} {
		if pos+n > len(data) {
			break
		}
		part := crc32.ChecksumIEEE(data[pos : pos+n])
		crc = combineCRC32(crc, part, int64(n))
		pos += n
	}
	if pos < len(data) {
		part := crc32.ChecksumIEEE(data[pos:])
		crc = combineCRC32(crc, part, int64(len(data)-pos))
	}
	if crc != full {
		t.Fatalf("combine=%08x want %08x", crc, full)
	}
}
