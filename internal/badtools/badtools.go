// Package badtools moves, renames and scripts the files a verification marked
// as BAD or ERROR.
package badtools

import (
	"bufio"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// BadDir is the subfolder corrupt files are moved to.
const BadDir = "_BAD_FILES"

// Move moves path into a _BAD_FILES folder beside it and returns the new path.
// An existing file of the same name gets a timestamp suffix.
func Move(path string) (string, error) {
	dir := filepath.Dir(path)
	badDir := filepath.Join(dir, BadDir)
	if err := os.MkdirAll(badDir, 0o755); err != nil {
		return "", err
	}
	name := filepath.Base(path)
	dest := filepath.Join(badDir, name)
	if _, err := os.Stat(dest); err == nil {
		ext := filepath.Ext(name)
		base := strings.TrimSuffix(name, ext)
		stamp := fmt.Sprintf("%d", time.Now().UnixNano())
		dest = filepath.Join(badDir, fmt.Sprintf("%s_%s%s", base, stamp, ext))
	}
	if err := os.Rename(path, dest); err != nil {
		return "", err
	}
	return dest, nil
}

// Rename appends ".CORRUPT" to path and returns the new path.
func Rename(path string) (string, error) {
	dest := path + ".CORRUPT"
	if err := os.Rename(path, dest); err != nil {
		return "", err
	}
	return dest, nil
}

// WriteDeleteScript writes a .bat that deletes paths.
func WriteDeleteScript(path string, files []string) error {
	return writeScript(path, func(w *bufio.Writer) {
		_, _ = w.WriteString("@echo off\r\nchcp 65001 >nul\r\necho Deleting BAD files...\r\n")
		for _, f := range files {
			_, _ = fmt.Fprintf(w, "del \"%s\"\r\n", f)
		}
		_, _ = w.WriteString("pause\r\n")
	})
}

// WriteCopyDuplicatesScript writes a .bat that copies the first file of each
// hash group over the others. Each group is a list of absolute paths.
func WriteCopyDuplicatesScript(path string, groups [][]string) error {
	return writeScript(path, func(w *bufio.Writer) {
		_, _ = w.WriteString("@echo off\r\nchcp 65001 >nul\r\necho Copying duplicates (first file to the others)...\r\n")
		for _, g := range groups {
			if len(g) < 2 {
				continue
			}
			for _, dest := range g[1:] {
				_, _ = fmt.Fprintf(w, "copy /y \"%s\" \"%s\"\r\n", g[0], dest)
			}
		}
		_, _ = w.WriteString("pause\r\n")
	})
}

// WriteDeleteDuplicatesScript writes a .bat that keeps the first file of each
// hash group and deletes the rest.
func WriteDeleteDuplicatesScript(path string, groups [][]string) error {
	return writeScript(path, func(w *bufio.Writer) {
		_, _ = w.WriteString("@echo off\r\nchcp 65001 >nul\r\necho Deleting duplicate files (keeping the first)...\r\n")
		for _, g := range groups {
			for _, f := range g[1:] {
				_, _ = fmt.Fprintf(w, "del \"%s\"\r\n", f)
			}
		}
		_, _ = w.WriteString("pause\r\n")
	})
}

func writeScript(path string, fn func(*bufio.Writer)) error {
	f, err := os.Create(path)
	if err != nil {
		return err
	}
	w := bufio.NewWriterSize(f, 32*1024)
	fn(w)
	if err := w.Flush(); err != nil {
		_ = f.Close()
		return err
	}
	return f.Close()
}

// NewRelativePath returns the path a moved file has relative to its old
// relative path, for updating the list after a move.
func NewRelativePath(rel string) string {
	name := filepath.Base(rel)
	dir := filepath.Dir(rel)
	if dir == "." || dir == "" {
		return filepath.Join(BadDir, name)
	}
	return filepath.Join(dir, BadDir, name)
}
