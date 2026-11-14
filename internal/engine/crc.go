package engine

// combineCRC32 returns the CRC-32 (IEEE) of the concatenation of two byte
// ranges, given the CRC of each and the length of the second. It is zlib's
// crc32_combine: a GF(2) matrix exponentiation applies len2 zero bits to crc1,
// then XORs crc2.
//
// This is what lets a single .sfv file be hashed by several cores without
// changing the checksum: CRC-32 is linear, so block checksums can be combined.
// MD5, SHA-1, SHA-256 and xxHash3 are sequential and have no such operation.
func combineCRC32(crc1, crc2 uint32, len2 int64) uint32 {
	if len2 <= 0 {
		return crc1
	}
	var even, odd [32]uint32
	odd[0] = 0xedb88320 // reflected CRC-32 polynomial
	row := uint32(1)
	for n := 1; n < 32; n++ {
		odd[n] = row
		row <<= 1
	}
	gf2Square(&even, &odd)
	gf2Square(&odd, &even)
	for {
		gf2Square(&even, &odd)
		if len2&1 != 0 {
			crc1 = gf2Times(&even, crc1)
		}
		len2 >>= 1
		if len2 == 0 {
			break
		}
		gf2Square(&odd, &even)
		if len2&1 != 0 {
			crc1 = gf2Times(&odd, crc1)
		}
		len2 >>= 1
		if len2 == 0 {
			break
		}
	}
	return crc1 ^ crc2
}

func gf2Times(mat *[32]uint32, vec uint32) uint32 {
	var sum uint32
	for i := 0; vec != 0; i++ {
		if vec&1 != 0 {
			sum ^= mat[i]
		}
		vec >>= 1
	}
	return sum
}

func gf2Square(square, mat *[32]uint32) {
	for n := 0; n < 32; n++ {
		square[n] = gf2Times(mat, mat[n])
	}
}
