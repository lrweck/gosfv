package hexfmt

import (
	"bytes"
	"encoding/hex"
	"math/rand"
	"testing"
)

func TestRoundTrip(t *testing.T) {
	r := rand.New(rand.NewSource(1))
	for n := 0; n < 300; n++ {
		src := make([]byte, n)
		r.Read(src)
		enc := Upper(src)
		if want := bytes.ToUpper([]byte(hex.EncodeToString(src))); enc != string(want) {
			t.Fatalf("Upper(%x) = %q, want %q", src, enc, want)
		}
		got, ok := DecodeString(enc)
		if !ok || !bytes.Equal(got, src) {
			t.Fatalf("DecodeString(%q) = %x, %v", enc, got, ok)
		}
		var dst []byte
		var ok2 bool
		dst, ok2 = Decode(dst, []byte(enc))
		if !ok2 || !bytes.Equal(dst, src) {
			t.Fatalf("Decode(%q) = %x, %v", enc, dst, ok2)
		}
		var buf [64]byte
		if n*2 <= len(buf) {
			EncodeUpper(buf[:n*2], src)
			if string(buf[:n*2]) != enc {
				t.Fatalf("EncodeUpper = %q, want %q", buf[:n*2], enc)
			}
		}
	}
}

func TestDecodeRejects(t *testing.T) {
	for _, s := range []string{"0", "0g", "zz", "0x12", "12 34", "ABCDEFG", "GGGGGGGG", "1234567G", "::::::::", "0000000:"} {
		if _, ok := DecodeString(s); ok {
			t.Errorf("DecodeString(%q) accepted", s)
		}
	}
	if _, ok := Decode(nil, []byte("abc")); ok {
		t.Error("odd length accepted")
	}
	// A failed decode leaves the accepted prefix as it was.
	dst := []byte{1, 2}
	if got, ok := Decode(dst, []byte("aabbccddgg")); ok || !bytes.Equal(got, dst) {
		t.Fatalf("Decode after invalid = %x, %v", got, ok)
	}
}

func FuzzDecode(f *testing.F) {
	f.Add("00112233")
	f.Add("DEADBEEF")
	f.Fuzz(func(t *testing.T, s string) {
		got, ok := DecodeString(s)
		if !ok {
			return
		}
		if string(bytes.ToUpper([]byte(hex.EncodeToString(got)))) != s && !bytes.EqualFold([]byte(hex.EncodeToString(got)), []byte(s)) {
			t.Fatalf("decoded %x from %q re-encodes differently", got, s)
		}
	})
}
