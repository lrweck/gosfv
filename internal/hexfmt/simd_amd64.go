//go:build goexperiment.simd && amd64

package hexfmt

import "simd/archsimd"

// The vector constants are built once, after the CPU check. Building them
// inside each call cost more than the table loop on short hashes, because a
// 32-character MD5 is only two vectors; once cached they are a register load.
var (
	simdReady bool

	encMask0f    archsimd.Uint16x8
	encASCIIBase archsimd.Uint16x8
	encSeven     archsimd.Uint16x8
	encZero      archsimd.Uint16x8
	encNine      archsimd.Uint16x8

	decZero     archsimd.Uint8x16
	decNine     archsimd.Uint8x16
	decLowerBit archsimd.Uint8x16
	decDigit0   archsimd.Uint8x16
	decDigit9   archsimd.Uint8x16
	decAlpha    archsimd.Uint8x16
	decAlphaf   archsimd.Uint8x16
	decMask0f   archsimd.Uint8x16
	decMaskFF   archsimd.Uint16x8
	decMaskF    archsimd.Uint16x8
)

func init() {
	if !archsimd.X86.AVX() {
		return
	}
	simdReady = true
	encMask0f = archsimd.BroadcastUint16x8(0x000f)
	encASCIIBase = archsimd.BroadcastUint16x8(0x30)
	encSeven = archsimd.BroadcastUint16x8(7)
	encZero = archsimd.BroadcastUint16x8(0)
	encNine = archsimd.BroadcastUint16x8(9)

	decZero = archsimd.BroadcastUint8x16(0)
	decNine = archsimd.BroadcastUint8x16(9)
	decLowerBit = archsimd.BroadcastUint8x16(0x20)
	decDigit0 = archsimd.BroadcastUint8x16('0')
	decDigit9 = archsimd.BroadcastUint8x16('9')
	decAlpha = archsimd.BroadcastUint8x16('a')
	decAlphaf = archsimd.BroadcastUint8x16('f')
	decMask0f = archsimd.BroadcastUint8x16(0x0f)
	decMaskFF = archsimd.BroadcastUint16x8(0x00ff)
	decMaskF = archsimd.BroadcastUint16x8(0x000f)
}

// encodeUpperSIMD encodes 16-byte blocks with AVX/AVX-512 and returns how many
// source bytes it handled. The caller finishes any tail with the table code.
//
// Each input byte becomes two ASCII characters. The vector holds the low and
// high nibble in 16-bit lanes, converts both to ASCII with arithmetic, packs
// them back into bytes, and interleaves the pairs from neighbouring lanes.
func encodeUpperSIMD(dst, src []byte) int {
	if len(src) < 16 || !simdReady {
		return 0
	}

	i := 0
	for ; i+16 <= len(src); i += 16 {
		x := archsimd.LoadUint8x16(src[i : i+16])
		w := x.AsUint16x8()
		// A 16-bit lane holds byte0 in its low half and byte1 in its high
		// half (little endian).
		a := w.And(encMask0f)                   // low nibble of byte0
		b := w.ShiftAllRight(4).And(encMask0f)  // high nibble of byte0
		c := w.ShiftAllRight(8).And(encMask0f)  // low nibble of byte1
		d := w.ShiftAllRight(12).And(encMask0f) // high nibble of byte1
		a = encASCIIBase.Add(encSeven.IfElse(a.Greater(encNine), encZero)).Add(a)
		b = encASCIIBase.Add(encSeven.IfElse(b.Greater(encNine), encZero)).Add(b)
		c = encASCIIBase.Add(encSeven.IfElse(c.Greater(encNine), encZero)).Add(c)
		d = encASCIIBase.Add(encSeven.IfElse(d.Greater(encNine), encZero)).Add(d)
		// The high nibble's character comes first: the little-endian
		// store writes b (high nibble) then a (low nibble) for byte0.
		w0 := a.ShiftAllLeft(8).Or(b)
		w1 := c.ShiftAllLeft(8).Or(d)
		w0.InterleaveLo(w1).AsUint8x16().Store(dst[i*2:])
		w0.InterleaveHi(w1).AsUint8x16().Store(dst[i*2+16:])
	}
	return i
}

// decodeSIMD decodes 16-character blocks. It reports how many source characters
// it consumed and whether they were all valid hexadecimal.
func decodeSIMD(dst, src []byte) (int, bool) {
	if len(src) < 16 || !simdReady {
		return 0, true
	}

	i, out := 0, 0
	var tmp [16]byte
	for ; i+16 <= len(src); i += 16 {
		v := archsimd.LoadUint8x16(src[i : i+16])
		lower := v.Or(decLowerBit)
		digits := v.GreaterEqual(decDigit0).And(v.LessEqual(decDigit9))
		letters := lower.GreaterEqual(decAlpha).And(lower.LessEqual(decAlphaf))
		if digits.Or(letters).ToBits() != 0xffff {
			return i, false
		}
		nib := v.And(decMask0f)
		// 'a'..'f' have a low nibble of 1..6; adding 9 makes it 10..15.
		nib = nib.Add(decNine.IfElse(letters, decZero))
		w := nib.AsUint16x8()
		packed := w.And(decMaskFF).ShiftAllLeft(4).Or(w.ShiftAllRight(8).And(decMaskF))
		// Compacting the low byte of each 16-bit lane would need AVX-512's
		// VPMOVWB. Store the lanes and copy the low bytes instead, which
		// keeps this path usable on AVX2 machines.
		packed.AsUint8x16().Store(tmp[:])
		dst[out+0] = tmp[0]
		dst[out+1] = tmp[2]
		dst[out+2] = tmp[4]
		dst[out+3] = tmp[6]
		dst[out+4] = tmp[8]
		dst[out+5] = tmp[10]
		dst[out+6] = tmp[12]
		dst[out+7] = tmp[14]
		out += 8
	}
	return i, true
}

// Backend names the vector path compiled into this binary.
func Backend() string {
	switch {
	case archsimd.X86.AVX512():
		return "amd64 SIMD (AVX-512)"
	case archsimd.X86.AVX2():
		return "amd64 SIMD (AVX2)"
	case archsimd.X86.AVX():
		return "amd64 SIMD (AVX)"
	default:
		return "amd64 SIMD"
	}
}
