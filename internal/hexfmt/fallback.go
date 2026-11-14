//go:build !(goexperiment.simd && amd64)

package hexfmt

// encodeUpperSIMD is the portable fallback: it handles no blocks and leaves
// everything to the table encoder.
func encodeUpperSIMD(dst, src []byte) int { return 0 }

// decodeSIMD is the portable fallback: it handles no blocks and leaves
// everything to the table decoder.
func decodeSIMD(dst, src []byte) (int, bool) { return 0, true }

// Backend names the vector path compiled into this binary.
func Backend() string { return "portable" }
