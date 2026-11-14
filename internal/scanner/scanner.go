// Package scanner enumerates the files GoSFV hashes.
//
// It uses filepath.WalkDir, os.Stat and filepath.Match from the standard
// library. There is no private directory-walking code: the speed of the tool
// comes from the hashing pipeline, not from reimplementing the filesystem.
package scanner

import (
	"context"
	"io/fs"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
)

// File is one discovered file.
type File struct {
	FullPath string
	Name     string
	RelPath  string
	Size     int64
}

// Options controls enumeration.
type Options struct {
	Recursive  bool
	SkipHidden bool
	Include    string
	Exclude    string
	// Sort orders results by relative path, making job output deterministic.
	Sort bool
}

// Collect enumerates inputs. Directories are walked; files are added directly.
// Inaccessible entries are skipped, as a file manager would.
//
// onFound, when not nil, is called periodically with the number of files found
// so far. It is meant for a UI status line, not for per-file work.
func Collect(ctx context.Context, inputs []string, base string, opt Options, onFound func(int)) ([]File, error) {
	filter := NewFilter(opt.Include, opt.Exclude)
	seen := make(map[string]struct{}, 1024)
	out := make([]File, 0, 1024)
	nextNotice := 2048

	add := func(full string, size int64) {
		key := pathKey(full)
		if _, dup := seen[key]; dup {
			return
		}
		name := filepath.Base(full)
		if !filter.Allow(name) {
			return
		}
		seen[key] = struct{}{}
		rel, err := filepath.Rel(base, full)
		if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
			rel = name
		}
		out = append(out, File{FullPath: full, Name: name, RelPath: rel, Size: size})
		if onFound != nil && len(out) >= nextNotice {
			nextNotice += 2048
			onFound(len(out))
		}
	}

	for _, input := range inputs {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		abs, err := filepath.Abs(input)
		if err != nil {
			continue
		}
		info, err := os.Stat(abs)
		if err != nil {
			continue
		}
		if !info.IsDir() {
			add(abs, info.Size())
			continue
		}
		root := abs
		err = filepath.WalkDir(root, func(path string, d fs.DirEntry, walkErr error) error {
			if walkErr != nil {
				return nil // unreadable entry: skip it and its subtree
			}
			if err := ctx.Err(); err != nil {
				return err
			}
			if d.IsDir() {
				if path == root {
					return nil
				}
				if opt.SkipHidden && isHidden(path, d) {
					return fs.SkipDir
				}
				if !opt.Recursive {
					return fs.SkipDir
				}
				return nil
			}
			if opt.SkipHidden && isHidden(path, d) {
				return nil
			}
			entry, err := d.Info()
			if err != nil || !entry.Mode().IsRegular() {
				return nil
			}
			add(path, entry.Size())
			return nil
		})
		if err != nil {
			return nil, err
		}
	}
	if opt.Sort && len(out) > 1 {
		slices.SortFunc(out, func(a, b File) int { return strings.Compare(a.RelPath, b.RelPath) })
	}
	if onFound != nil {
		onFound(len(out))
	}
	return out, nil
}

// CommonBase returns the directory relative paths are computed from.
//
// One dropped directory is its own base; one dropped file uses its parent.
// Several items use the common ancestor of their parents, so a checksum file
// laid beside the dropped items can address them by relative path.
func CommonBase(inputs []string) string {
	if len(inputs) == 0 {
		return ""
	}
	abs := func(p string) string {
		if a, err := filepath.Abs(p); err == nil {
			return filepath.Clean(a)
		}
		return filepath.Clean(p)
	}
	if len(inputs) == 1 {
		p := abs(inputs[0])
		if info, err := os.Stat(p); err == nil && info.IsDir() {
			return p
		}
		return filepath.Dir(p)
	}
	base := ""
	for _, input := range inputs {
		q := filepath.Dir(abs(input))
		if base == "" {
			base = q
			continue
		}
		if !strings.EqualFold(filepath.VolumeName(base), filepath.VolumeName(q)) {
			// Different drives have no common path; keep the first base
			// and let relative paths fall back to filenames.
			return base
		}
		for !under(base, q) {
			parent := filepath.Dir(base)
			if parent == base {
				return base
			}
			base = parent
		}
	}
	return base
}

// under reports whether p is base or below it. filepath.Rel compares path
// elements case-insensitively on Windows.
func under(base, p string) bool {
	rel, err := filepath.Rel(base, p)
	if err != nil {
		return false
	}
	return rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator))
}

func pathKey(p string) string {
	p = filepath.Clean(p)
	if runtime.GOOS == "windows" {
		return strings.ToLower(p)
	}
	return p
}

// Filter applies include and exclude wildcard lists. Patterns are separated
// by semicolons or commas; an empty list allows every name.
//
// filepath.Match is the matcher. Patterns and names are lowercased because
// the original tool treated masks case-insensitively on Windows.
type Filter struct {
	include []string
	exclude []string
}

// NewFilter builds a filter from the advanced options.
func NewFilter(include, exclude string) Filter {
	return Filter{include: splitPatterns(include), exclude: splitPatterns(exclude)}
}

func splitPatterns(s string) []string {
	if strings.TrimSpace(s) == "" {
		return nil
	}
	var out []string
	start := 0
	for i := 0; i <= len(s); i++ {
		if i == len(s) || s[i] == ';' || s[i] == ',' {
			if p := strings.TrimSpace(s[start:i]); p != "" {
				out = append(out, strings.ToLower(p))
			}
			start = i + 1
		}
	}
	return out
}

// Allow reports whether name passes the exclude and include patterns.
func (f Filter) Allow(name string) bool {
	if len(f.include) == 0 && len(f.exclude) == 0 {
		return true
	}
	name = strings.ToLower(name)
	for _, p := range f.exclude {
		if matchPattern(name, p) {
			return false
		}
	}
	if len(f.include) == 0 {
		return true
	}
	for _, p := range f.include {
		if matchPattern(name, p) {
			return true
		}
	}
	return false
}

// Match matches a filename against a wildcard mask with * and ?. It is
// case-insensitive and special-cases the Windows "*.*" mask, which means
// every file rather than only names with a dot.
func Match(name, pattern string) bool {
	return matchPattern(strings.ToLower(name), strings.ToLower(pattern))
}

func matchPattern(name, pattern string) bool {
	if pattern == "" || pattern == "*" || pattern == "*.*" {
		return true
	}
	ok, err := filepath.Match(pattern, name)
	return err == nil && ok
}
