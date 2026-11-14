// Package hexfmt encodes and decodes hexadecimal without allocating.
//
// The decoder is table driven and branchless: it folds eight ASCII characters
// into four bytes per iteration, which matters when a verification file holds
// hundreds of thousands of hashes. The encoder writes into a caller-provided
// buffer, so the hot paths of saving and copying never build intermediate
// strings.
package hexfmt

import "encoding/binary"

const upperDigits = "0123456789ABCDEF"

var (
	// upperPair maps a byte to its two uppercase hexadecimal characters,
	// packed as little-endian uint16.
	upperPair [256]uint16
	// nibble maps an ASCII hexadecimal character to its value, or 0xFF.
	nibble [256]byte
)

func init() {
	for i := range upperPair {
		upperPair[i] = uint16(upperDigits[i>>4]) | uint16(upperDigits[i&0x0f])<<8
	}
	for i := range nibble {
		nibble[i] = 0xff
	}
	for i := 0; i < 10; i++ {
		nibble['0'+i] = byte(i)
	}
	for i := 0; i < 6; i++ {
		nibble['A'+i] = byte(10 + i)
		nibble['a'+i] = byte(10 + i)
	}
}

// AppendUpper appends the uppercase hexadecimal form of src to dst.
func AppendUpper(dst, src []byte) []byte {
	n := len(src) * 2
	if cap(dst)-len(dst) < n {
		grown := make([]byte, len(dst), len(dst)+n)
		copy(grown, dst)
		dst = grown
	}
	dst = dst[:len(dst)+n]
	EncodeUpper(dst[len(dst)-n:], src)
	return dst
}

// EncodeUpper writes the uppercase hexadecimal form of src into dst, which
// must be at least 2*len(src) bytes long. On amd64 built with
// GOEXPERIMENT=simd it converts 16 bytes per iteration with
// simd/archsimd; the table loop finishes the tail.
func EncodeUpper(dst, src []byte) {
	if len(src) == 0 {
		return
	}
	_ = dst[2*len(src)-1]
	i := encodeUpperSIMD(dst, src)
	for ; i < len(src); i++ {
		p := upperPair[src[i]]
		j := i * 2
		dst[j] = byte(p)
		dst[j+1] = byte(p >> 8)
	}
}

// Upper returns the uppercase hexadecimal form of src in a string. Use it
// only where a string is unavoidable, such as a UI label.
func Upper(src []byte) string {
	if len(src) == 0 {
		return ""
	}
	buf := make([]byte, len(src)*2)
	EncodeUpper(buf, src)
	return string(buf)
}

// Decode appends the bytes encoded in the hexadecimal text src to dst. It
// reports false, leaving dst unchanged, when src is not valid hexadecimal or
// has an odd length.
func Decode(dst, src []byte) ([]byte, bool) {
	if len(src)%2 != 0 {
		return dst, false
	}
	need := len(src) / 2
	if cap(dst)-len(dst) < need {
		grown := make([]byte, len(dst), len(dst)+need)
		copy(grown, dst)
		dst = grown
	}
	start := len(dst)
	dst = dst[:start+need]
	i := 0
	// The SIMD decoder handles blocks of 16 characters; the table loop
	// handles the tail and is the whole decoder on other architectures.
	if consumed, ok := decodeSIMD(dst[start:], src); !ok {
		return dst[:start], false
	} else {
		i = consumed
	}
	for ; i+8 <= len(src); i += 8 {
		// Eight ASCII characters fit in one uint64. The SWAR path extracts
		// and validates all eight nibbles in parallel, which matters for
		// CRC-32 SFV lines whose hashes are exactly eight characters.
		if !decode8(dst[start+i/2:], src[i:]) {
			return dst[:start], false
		}
	}
	for ; i < len(src); i += 2 {
		n0, n1 := nibble[src[i]], nibble[src[i+1]]
		if n0|n1 > 0x0f {
			return dst[:start], false
		}
		dst[start+i/2] = n0<<4 | n1
	}
	return dst, true
}

// DecodeString decodes hexadecimal text into a new slice.
func DecodeString(s string) ([]byte, bool) {
	if len(s)%2 != 0 {
		return nil, false
	}
	dst := make([]byte, len(s)/2)
	i := 0
	for ; i+8 <= len(s); i += 8 {
		n0 := nibble[s[i]]
		n1 := nibble[s[i+1]]
		n2 := nibble[s[i+2]]
		n3 := nibble[s[i+3]]
		n4 := nibble[s[i+4]]
		n5 := nibble[s[i+5]]
		n6 := nibble[s[i+6]]
		n7 := nibble[s[i+7]]
		if n0|n1|n2|n3|n4|n5|n6|n7 > 0x0f {
			return nil, false
		}
		j := i / 2
		dst[j] = n0<<4 | n1
		dst[j+1] = n2<<4 | n3
		dst[j+2] = n4<<4 | n5
		dst[j+3] = n6<<4 | n7
	}
	for ; i < len(s); i += 2 {
		n0, n1 := nibble[s[i]], nibble[s[i+1]]
		if n0|n1 > 0x0f {
			return nil, false
		}
		dst[i/2] = n0<<4 | n1
	}
	return dst, true
}

// decode8 decodes eight ASCII hexadecimal characters into four bytes with
// SWAR. It returns false for any non-hexadecimal byte, including a letter past
// 'f' whose low nibble would otherwise look valid.
func decode8(dst, src []byte) bool {
	const (
		lowNibbles = 0x0f0f0f0f0f0f0f0f
		alphaBits  = 0x0101010101010101
		highNibs   = 0xf0f0f0f0f0f0f0f0
		tenantBits = 0x1010101010101010
		digitBase  = 0x3030303030303030
		lowerBit   = 0x2020202020202020
	)
	v := binary.LittleEndian.Uint64(src)
	alpha := (v >> 6) & alphaBits
	nib := (v & lowNibbles) + alpha*9
	if nib&highNibs != 0 {
		return false
	}
	// n+6 carries into bit 4 exactly when n >= 10; after the shift each
	// byte is 0x01 for a letter and 0x00 for a digit.
	letters := ((nib + 0x0606060606060606) & tenantBits) >> 4
	expected := nib + digitBase + letters*0x27 // 'a'-'0'-10 == 0x27
	if (v | lowerBit) != expected {
		return false
	}
	// Pack the value of each 16-bit lane into its low byte, then gather the
	// four bytes. Every shift stays inside a lane, so no lane borrows.
	packed := ((nib & 0x00ff00ff00ff00ff) << 4) | ((nib >> 8) & 0x000f000f000f000f)
	out := uint32(byte(packed)) |
		uint32(byte(packed>>16))<<8 |
		uint32(byte(packed>>32))<<16 |
		uint32(byte(packed>>48))<<24
	binary.LittleEndian.PutUint32(dst, out)
	return true
}

// Valid reports whether s is an even-length hexadecimal string.
func Valid(s []byte) bool {
	if len(s)%2 != 0 {
		return false
	}
	i := 0
	for ; i+8 <= len(s); i += 8 {
		if nibble[s[i]]|nibble[s[i+1]]|nibble[s[i+2]]|nibble[s[i+3]]|
			nibble[s[i+4]]|nibble[s[i+5]]|nibble[s[i+6]]|nibble[s[i+7]] > 0x0f {
			return false
		}
	}
	for ; i < len(s); i++ {
		if nibble[s[i]] == 0xff {
			return false
		}
	}
	return true
}
