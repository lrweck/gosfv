//go:build goexperiment.simd && amd64

package hexfmt

import "simd/archsimd"

// encodeUpperSIMD encodes 16-byte blocks with AVX/AVX-512 and returns how many
// source bytes it handled. The caller finishes any tail with the table code.
//
// Each input byte becomes two ASCII characters. The vector holds the low and
// high nibble in 16-bit lanes, converts both to ASCII with arithmetic, packs
// them back into bytes, and interleaves the pairs from neighbouring lanes.
func encodeUpperSIMD(dst, src []byte) int {
	if len(src) < 16 || !archsimd.X86.AVX() {
		return 0
	}
	mask0f := archsimd.BroadcastUint16x8(0x000f)
	asciiBase := archsimd.BroadcastUint16x8(0x30)
	seven := archsimd.BroadcastUint16x8(7)
	zero := archsimd.BroadcastUint16x8(0)
	nine := archsimd.BroadcastUint16x8(9)

	i := 0
	for ; i+16 <= len(src); i += 16 {
		x := archsimd.LoadUint8x16(src[i : i+16])
		w := x.AsUint16x8()
		// A 16-bit lane holds byte0 in its low half and byte1 in its high
		// half (little endian).
		a := w.And(mask0f)                   // low nibble of byte0
		b := w.ShiftAllRight(4).And(mask0f)  // high nibble of byte0
		c := w.ShiftAllRight(8).And(mask0f)  // low nibble of byte1
		d := w.ShiftAllRight(12).And(mask0f) // high nibble of byte1
		a = asciiBase.Add(seven.IfElse(a.Greater(nine), zero)).Add(a)
		b = asciiBase.Add(seven.IfElse(b.Greater(nine), zero)).Add(b)
		c = asciiBase.Add(seven.IfElse(c.Greater(nine), zero)).Add(c)
		d = asciiBase.Add(seven.IfElse(d.Greater(nine), zero)).Add(d)
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
	if len(src) < 16 || !archsimd.X86.AVX() {
		return 0, true
	}
	zeroU8 := archsimd.BroadcastUint8x16(0)
	nineU8 := archsimd.BroadcastUint8x16(9)
	lowerBit := archsimd.BroadcastUint8x16(0x20)
	digit0 := archsimd.BroadcastUint8x16('0')
	digit9 := archsimd.BroadcastUint8x16('9')
	alpha := archsimd.BroadcastUint8x16('a')
	alphaf := archsimd.BroadcastUint8x16('f')
	mask0f := archsimd.BroadcastUint8x16(0x0f)
	maskff := archsimd.BroadcastUint16x8(0x00ff)
	maskf := archsimd.BroadcastUint16x8(0x000f)

	i, out := 0, 0
	var tmp [16]byte
	for ; i+16 <= len(src); i += 16 {
		v := archsimd.LoadUint8x16(src[i : i+16])
		lower := v.Or(lowerBit)
		digits := v.GreaterEqual(digit0).And(v.LessEqual(digit9))
		letters := lower.GreaterEqual(alpha).And(lower.LessEqual(alphaf))
		if digits.Or(letters).ToBits() != 0xffff {
			return i, false
		}
		nib := v.And(mask0f)
		// 'a'..'f' have a low nibble of 1..6; adding 9 makes it 10..15.
		nib = nib.Add(nineU8.IfElse(letters, zeroU8))
		w := nib.AsUint16x8()
		packed := w.And(maskff).ShiftAllLeft(4).Or(w.ShiftAllRight(8).And(maskf))
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
