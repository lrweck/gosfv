// Package hasher names the checksum algorithms GoSFV supports and builds their
// hashers.
//
// Every algorithm here is hardware accelerated by the Go runtime or its
// library: xxHash3-128 uses github.com/zeebo/xxh3's AVX2/AVX-512 assembly,
// CRC-32 uses the standard library's PCLMULQDQ (amd64) or CRC instructions
// (arm64), and MD5, SHA-1 and SHA-256 use the runtime's assembly (including
// SHA-NI on CPUs that have it). All of them implement hash.Hash, so the engine
// has a single allocation-free code path.
package hasher

import (
	"crypto/md5"
	"crypto/sha1"
	"crypto/sha256"
	"hash"
	"hash/crc32"
	"strings"

	"github.com/zeebo/xxh3"
)

// Kind is a checksum algorithm.
type Kind uint8

const (
	XXH3 Kind = iota
	CRC32
	MD5
	SHA1
	SHA256
)

// All lists the algorithms in the order the menus show them.
var All = []Kind{XXH3, CRC32, MD5, SHA1, SHA256}

// String returns the name used in checksum headers and settings.
func (k Kind) String() string {
	switch k {
	case XXH3:
		return "XXHASH3"
	case CRC32:
		return "CRC32"
	case MD5:
		return "MD5"
	case SHA1:
		return "SHA1"
	case SHA256:
		return "SHA256"
	}
	return "UNKNOWN"
}

// Label returns the human-readable name used by the UI.
func (k Kind) Label() string {
	switch k {
	case XXH3:
		return "xxHash-3 (128-bit)"
	case CRC32:
		return "CRC-32 (SFV)"
	case MD5:
		return "MD5"
	case SHA1:
		return "SHA-1"
	case SHA256:
		return "SHA-256"
	}
	return "Unknown"
}

// ShortLabel is the compact menu/toolbar name.
func (k Kind) ShortLabel() string {
	switch k {
	case XXH3:
		return "xxHash-3"
	case CRC32:
		return "CRC-32"
	case MD5:
		return "MD5"
	case SHA1:
		return "SHA-1"
	case SHA256:
		return "SHA-256"
	}
	return "Unknown"
}

// Ext returns the conventional checksum file extension.
func (k Kind) Ext() string {
	switch k {
	case CRC32:
		return ".sfv"
	case MD5:
		return ".md5"
	case SHA1:
		return ".sha1"
	case SHA256:
		return ".sha256"
	default:
		return ".xxh3"
	}
}

// Size is the length of the digest in bytes.
func (k Kind) Size() int {
	switch k {
	case XXH3:
		return 16
	case CRC32:
		return 4
	case MD5:
		return 16
	case SHA1:
		return 20
	case SHA256:
		return 32
	}
	return 0
}

// New returns a fresh hasher. The engine resets it between files, so a worker
// creates one per goroutine rather than one per file.
func (k Kind) New() hash.Hash {
	switch k {
	case XXH3:
		return xxh3.New128()
	case CRC32:
		return crc32.NewIEEE()
	case MD5:
		return md5.New()
	case SHA1:
		return sha1.New()
	case SHA256:
		return sha256.New()
	}
	return nil
}

// Parse accepts the names written in settings, menus and command lines. It is
// case-insensitive and tolerates common aliases.
func Parse(s string) (Kind, bool) {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "xxh3", "xxhash3", "xxhash-3", "xxh128", "xxhash128":
		return XXH3, true
	case "crc32", "crc-32", "sfv":
		return CRC32, true
	case "md5":
		return MD5, true
	case "sha1", "sha-1":
		return SHA1, true
	case "sha256", "sha-256":
		return SHA256, true
	}
	return 0, false
}

// FromExtension returns the algorithm a checksum file's extension implies.
func FromExtension(ext string) (Kind, bool) {
	switch strings.ToLower(ext) {
	case ".xxh3", ".xxh":
		return XXH3, true
	case ".sfv":
		return CRC32, true
	case ".md5":
		return MD5, true
	case ".sha1":
		return SHA1, true
	case ".sha256":
		return SHA256, true
	}
	return 0, false
}
