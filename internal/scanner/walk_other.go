//go:build !windows

package scanner

import (
	"context"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
)

func walkFiles(ctx context.Context, root string, recursive, skipHidden bool, fn func(path string, size int64) error) error {
	var walk func(dir string) error
	walk = func(dir string) error {
		if err := ctx.Err(); err != nil {
			return err
		}
		entries, err := os.ReadDir(dir)
		if err != nil {
			return nil
		}
		for _, e := range entries {
			if err := ctx.Err(); err != nil {
				return err
			}
			name := e.Name()
			if skipHidden && strings.HasPrefix(name, ".") {
				continue
			}
			full := filepath.Join(dir, name)
			if e.IsDir() {
				if recursive {
					if err := walk(full); err != nil {
						return err
					}
				}
				continue
			}
			if e.Type()&fs.ModeSymlink != 0 {
				continue
			}
			info, err := e.Info()
			if err != nil || !info.Mode().IsRegular() {
				continue
			}
			if err := fn(full, info.Size()); err != nil {
				return err
			}
		}
		return nil
	}
	return walk(root)
}
