//go:build windows

package scanner

import (
	"context"
	"errors"
	"path/filepath"
	"strings"

	"golang.org/x/sys/windows"
)

// walkFiles walks root with the Win32 directory listing, which carries each
// entry's attributes and size. It never follows reparse points.
func walkFiles(ctx context.Context, root string, recursive, skipHidden bool, fn func(path string, size int64) error) error {
	return walkWin(ctx, root, recursive, skipHidden, fn)
}

func walkWin(ctx context.Context, dir string, recursive, skipHidden bool, fn func(path string, size int64) error) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	search := filepath.Join(dir, "*")
	if len(search) >= 248 && !strings.HasPrefix(search, `\\?\`) {
		if strings.HasPrefix(search, `\\`) {
			search = `\\?\UNC\` + search[2:]
		} else {
			search = `\\?\` + search
		}
	}
	pattern, err := windows.UTF16PtrFromString(search)
	if err != nil {
		return nil // invalid path: skip it, like an unreadable directory
	}
	var data windows.Win32finddata
	h, err := windows.FindFirstFile(pattern, &data)
	if err != nil {
		return nil
	}
	defer windows.FindClose(h)

	for {
		if err := ctx.Err(); err != nil {
			return err
		}
		name := windows.UTF16ToString(data.FileName[:])
		if name != "" && name != "." && name != ".." {
			attr := data.FileAttributes
			skip := attr&windows.FILE_ATTRIBUTE_REPARSE_POINT != 0
			if skipHidden && attr&(windows.FILE_ATTRIBUTE_HIDDEN|windows.FILE_ATTRIBUTE_SYSTEM) != 0 {
				skip = true
			}
			if !skip {
				full := filepath.Join(dir, name)
				if attr&windows.FILE_ATTRIBUTE_DIRECTORY != 0 {
					if recursive {
						if err := walkWin(ctx, full, recursive, skipHidden, fn); err != nil {
							return err
						}
					}
				} else {
					size := int64(data.FileSizeHigh)<<32 | int64(data.FileSizeLow)
					if err := fn(full, size); err != nil {
						return err
					}
				}
			}
		}
		if err := windows.FindNextFile(h, &data); err != nil {
			if errors.Is(err, windows.ERROR_NO_MORE_FILES) {
				return nil
			}
			return nil
		}
	}
}
