package hasher

import (
	"bytes"
	"encoding/hex"
	"hash"
	"strings"
	"testing"

	"gosfv/internal/hexfmt"
)

var vectors = []struct {
	kind Kind
	in   string
	want string
}{
	{MD5, "", "d41d8cd98f00b204e9800998ecf8427e"},
	{MD5, "abc", "900150983cd24fb0d6963f7d28e17f72"},
	{SHA1, "", "da39a3ee5e6b4b0d3255bfef95601890afd80709"},
	{SHA1, "abc", "a9993e364706816aba3e25717850c26c9cd0d89d"},
	{SHA256, "", "e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855"},
	{SHA256, "abc", "ba7816bf8f01cfea414140de5dae2223b00361a396177a9cb410ff61f20015ad"},
	{CRC32, "123456789", "cbf43926"},
	{XXH3, "", "99aa06d3014798d86001c324468d497f"},
}

func sum(t *testing.T, h hash.Hash, in string) []byte {
	t.Helper()
	if _, err := h.Write([]byte(in)); err != nil {
		t.Fatal(err)
	}
	return h.Sum(nil)
}

func TestVectors(t *testing.T) {
	for _, v := range vectors {
		got := sum(t, v.kind.New(), v.in)
		if !strings.EqualFold(hexfmt.Upper(got), v.want) {
			t.Errorf("%s(%q) = %X, want %s", v.kind, v.in, got, v.want)
		}
		if len(got) != v.kind.Size() {
			t.Errorf("%s digest is %d bytes, want %d", v.kind, len(got), v.kind.Size())
		}
	}
}

func TestReuse(t *testing.T) {
	for _, k := range All {
		h := k.New()
		if _, err := h.Write([]byte("garbage")); err != nil {
			t.Fatal(err)
		}
		h.Reset()
		if _, err := h.Write([]byte("abc")); err != nil {
			t.Fatal(err)
		}
		got := h.Sum(nil)
		fresh := sum(t, k.New(), "abc")
		if !bytes.Equal(got, fresh) {
			t.Errorf("%s after Reset = %X, want %X", k, got, fresh)
		}
	}
}

func TestParse(t *testing.T) {
	for _, name := range []string{"xxh3", "XXHASH3", "xxhash-3", "crc32", "sfv", "md5", "sha1", "SHA-1", "sha256", "SHA-256"} {
		if _, ok := Parse(name); !ok {
			t.Errorf("Parse(%q) failed", name)
		}
	}
	if _, ok := Parse("sha512"); ok {
		t.Error("Parse(sha512) succeeded")
	}
	if k, _ := FromExtension(".SFV"); k != CRC32 {
		t.Errorf("FromExtension(.SFV) = %v", k)
	}
	if _, ok := FromExtension(".txt"); ok {
		t.Error("FromExtension(.txt) succeeded")
	}
}

func TestKnownHashBytes(t *testing.T) {
	got, _ := hexfmt.DecodeString("cbf43926")
	want, _ := hex.DecodeString("cbf43926")
	if !bytes.Equal(got, want) {
		t.Fatalf("hexfmt decode mismatch: %x != %x", got, want)
	}
}
