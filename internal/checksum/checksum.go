// Package checksum parses and writes checksum files.
//
// The parser accepts the two formats found in the wild: "HASH *filename" and,
// for CRC-32 files, "filename HASH". It reads the file once, decodes every
// digest into a single arena, and allocates only the filename strings that the
// list needs.
package checksum

import (
	"bufio"
	"bytes"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/lrweck/gosfv/internal/hasher"
	"github.com/lrweck/gosfv/internal/hexfmt"
	"github.com/lrweck/gosfv/internal/model"
)

// Entry is one parsed checksum line.
type Entry struct {
	Name string
	Hash []byte
}

// Parsed is the result of reading a checksum file.
type Parsed struct {
	Algo     hasher.Kind
	Entries  []Entry
	Comments []string
}

// ParseFile reads and parses a checksum file.
func ParseFile(path string, algo hasher.Kind) (*Parsed, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	return ParseReader(f, algo)
}

// ParseReader parses a checksum stream, such as a file or standard input.
func ParseReader(r io.Reader, algo hasher.Kind) (*Parsed, error) {
	data, err := io.ReadAll(r)
	if err != nil {
		return nil, err
	}
	p := Parse(data, algo)
	return &p, nil
}

// Parse parses checksum text. Malformed lines are skipped, as checksum files
// from different tools often contain blank lines, banners and comments.
func Parse(data []byte, algo hasher.Kind) Parsed {
	p := Parsed{Algo: algo}
	data = bytes.TrimPrefix(data, []byte{0xef, 0xbb, 0xbf})
	// One digest is at most 64 text bytes; a digest is at most 32 bytes.
	// A half-size arena avoids regrowth for the shortest common lines.
	arena := make([]byte, 0, len(data)/2)
	entries := make([]Entry, 0, len(data)/48)
	var comments []string

	for len(data) > 0 {
		line := data
		if i := bytes.IndexByte(data, '\n'); i >= 0 {
			line, data = data[:i], data[i+1:]
		} else {
			data = nil
		}
		line = bytes.TrimSuffix(line, []byte{'\r'})
		line = trimSpace(line)
		if len(line) == 0 {
			continue
		}
		if line[0] == ';' {
			continue
		}
		if len(line) >= 2 && line[0] == '/' && line[1] == '/' {
			comments = append(comments, string(trimSpace(line[2:])))
			continue
		}

		name, token, ok := splitLine(line, algo)
		if !ok {
			continue
		}
		start := len(arena)
		decoded, ok := hexfmt.Decode(arena, token)
		if !ok {
			arena = arena[:start]
			continue
		}
		arena = decoded
		entries = append(entries, Entry{Name: name, Hash: arena[start:]})
	}
	p.Entries = entries
	p.Comments = comments
	return p
}

// splitLine returns the filename and hexadecimal digest of a line. It handles
// "HASH *name", "HASH name" and CRC-32's "name HASH" without a regular
// expression, which keeps large verifications allocation free.
func splitLine(line []byte, algo hasher.Kind) (name string, token []byte, ok bool) {
	firstEnd := indexSpace(line)
	if firstEnd < 0 {
		return "", nil, false
	}
	first := line[:firstEnd]
	rest := trimSpace(line[firstEnd:])
	restHasStar := len(rest) > 0 && rest[0] == '*'
	if restHasStar {
		rest = trimSpace(rest[1:])
	}

	// A filename-first CRC-32 line, the canonical .sfv syntax: the last
	// token is exactly eight hexadecimal characters. An explicit "*" means
	// the line is hash-first.
	if algo == hasher.CRC32 && !restHasStar && len(rest) > 0 {
		last := lastToken(line)
		if len(last) == 8 && hexfmt.Valid(last) {
			prefix := trimSpace(line[:len(line)-len(last)])
			if len(prefix) > 0 {
				return unquote(prefix), last, true
			}
		}
	}
	if !hexfmt.Valid(first) || len(rest) == 0 {
		return "", nil, false
	}
	return unquote(rest), first, true
}

func unquote(b []byte) string {
	if len(b) >= 2 && b[0] == '"' && b[len(b)-1] == '"' {
		b = b[1 : len(b)-1]
	}
	return string(b)
}

// WriteOptions controls how a checksum file is written.
type WriteOptions struct {
	// Relative writes paths relative to SaveDir; otherwise paths are absolute.
	Relative bool
	// SaveDir is the directory of the checksum file being written.
	SaveDir string
	// Prefix is prepended to every stored path, as an archive's root folder.
	Prefix string
	// Comments writes the two header lines.
	Comments bool
	// Signature is the name written in the header comment.
	Signature string
	// OnlyOK writes only files whose status is OK. Job mode uses it.
	OnlyOK bool
}

// WriteFile writes files to path. It returns how many entries were written.
func WriteFile(path string, algo hasher.Kind, files []model.File, opt WriteOptions) (int, error) {
	f, err := os.OpenFile(path, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0o644)
	if err != nil {
		return 0, err
	}
	written, werr := WriteTo(f, algo, files, opt)
	cerr := f.Close()
	if werr != nil {
		return written, werr
	}
	return written, cerr
}

// WriteTo writes files to w, buffering internally.
func WriteTo(w io.Writer, algo hasher.Kind, files []model.File, opt WriteOptions) (int, error) {
	bw := bufio.NewWriterSize(w, 64*1024)
	written, err := writeBuffered(bw, algo, files, opt)
	if err == nil {
		err = bw.Flush()
	}
	return written, err
}

func writeBuffered(w *bufio.Writer, algo hasher.Kind, files []model.File, opt WriteOptions) (int, error) {
	if opt.Comments {
		signature := opt.Signature
		if signature == "" {
			signature = "L33T"
		}
		if _, err := fmt.Fprintf(w, "; Generated by GoSFV (Signature: %s)\r\n; Algorithm: %s\r\n", signature, algo); err != nil {
			return 0, err
		}
	}

	var hexbuf [64]byte
	written := 0
	for i := range files {
		f := &files[i]
		if f.Hash == nil || f.Status == model.Queued || f.Status == model.Pending {
			continue
		}
		if opt.OnlyOK && f.Status != model.OK {
			continue
		}
		full := f.FullPath()
		stored := full
		if opt.Relative {
			if sameDir(opt.SaveDir, f.Dir) && f.Rel != "" {
				// The common case: the checksum is written beside the
				// files, so its stored path is already relative.
				stored = f.Rel
			} else if rel, err := filepath.Rel(opt.SaveDir, full); err == nil && !filepath.IsAbs(rel) {
				stored = rel
			} else if f.Rel != "" {
				stored = f.Rel
			}
		}
		if opt.Prefix != "" {
			stored = filepath.Join(opt.Prefix, stored)
		}
		h := f.Hash
		if len(h)*2 > len(hexbuf) {
			hexbuf2 := make([]byte, len(h)*2)
			hexfmt.EncodeUpper(hexbuf2, h)
			if _, err := w.Write(hexbuf2); err != nil {
				return written, err
			}
		} else {
			enc := hexbuf[:len(h)*2]
			hexfmt.EncodeUpper(enc, h)
			if _, err := w.Write(enc); err != nil {
				return written, err
			}
		}
		if err := w.WriteByte(' '); err != nil {
			return written, err
		}
		if err := w.WriteByte('*'); err != nil {
			return written, err
		}
		// filepath.ToSlash only on Windows would change stored paths;
		// checksum files conventionally use the platform separator, as
		// the tools that created the format do.
		if _, err := w.WriteString(stored); err != nil {
			return written, err
		}
		if _, err := w.WriteString("\r\n"); err != nil {
			return written, err
		}
		written++
	}
	return written, nil
}

// DefaultName is the file name a Save dialog starts with.
func DefaultName(algo hasher.Kind) string { return "checksums" + algo.Ext() }

func trimSpace(b []byte) []byte {
	i, j := 0, len(b)
	for i < j && isSpace(b[i]) {
		i++
	}
	for j > i && isSpace(b[j-1]) {
		j--
	}
	return b[i:j]
}

func isSpace(c byte) bool { return c == ' ' || c == '\t' || c == '\r' || c == '\n' }

func indexSpace(b []byte) int {
	for i, c := range b {
		if isSpace(c) {
			return i
		}
	}
	return -1
}

func lastToken(b []byte) []byte {
	j := len(b)
	for j > 0 && isSpace(b[j-1]) {
		j--
	}
	i := j
	for i > 0 && !isSpace(b[i-1]) {
		i--
	}
	return b[i:j]
}

func sameDir(a, b string) bool {
	a, b = filepath.Clean(a), filepath.Clean(b)
	if a == "" || b == "" {
		return false
	}
	if runtime.GOOS == "windows" {
		return strings.EqualFold(a, b)
	}
	return a == b
}

// BaseDir returns the directory containing a checksum file.
func BaseDir(path string) string { return filepath.Dir(path) }

// ExtensionIsKnown reports whether the path's extension names an algorithm.
func ExtensionIsKnown(path string) bool {
	_, ok := hasher.FromExtension(strings.ToLower(filepath.Ext(path)))
	return ok
}
