//go:build windows

// Package fio opens files for the hashing engine.
//
// On Windows it uses CreateFile and ReadFile directly instead of os.File: the
// handle needs no finalizer or poller registration, and FILE_FLAG_SEQUENTIAL_SCAN
// lets the cache manager prefetch the next chunks. On other systems it is a
// thin wrapper over os.File.
package fio

import (
	"errors"
	"io"
	"os"
	"strings"

	"golang.org/x/sys/windows"
)

// File is an open file read sequentially from the start.
type File struct {
	h    windows.Handle
	size int64
}

// Open opens path for reading.
func Open(path string) (*File, error) {
	p, err := windows.UTF16PtrFromString(longPath(path))
	if err != nil {
		return nil, err
	}
	h, err := windows.CreateFile(
		p,
		windows.GENERIC_READ,
		windows.FILE_SHARE_READ,
		nil,
		windows.OPEN_EXISTING,
		windows.FILE_ATTRIBUTE_NORMAL|windows.FILE_FLAG_SEQUENTIAL_SCAN,
		0,
	)
	if err != nil {
		return nil, err
	}
	var info windows.ByHandleFileInformation
	if err := windows.GetFileInformationByHandle(h, &info); err != nil {
		// Some network shares do not answer by handle. Fall back to one
		// stat rather than failing the file.
		st, serr := os.Stat(path)
		if serr != nil {
			_ = windows.CloseHandle(h)
			return nil, err
		}
		return &File{h: h, size: st.Size()}, nil
	}
	size := int64(info.FileSizeHigh)<<32 | int64(info.FileSizeLow)
	return &File{h: h, size: size}, nil
}

// Size returns the file length in bytes.
func (f *File) Size() int64 { return f.size }

// Read fills p. It returns io.EOF at the end of the file.
func (f *File) Read(p []byte) (int, error) {
	return f.readAt(p, -1)
}

// ReadAt fills p from off. On Windows it uses the OVERLAPPED offset, so
// concurrent chunk readers share the handle without moving a file pointer.
func (f *File) ReadAt(p []byte, off int64) (int, error) {
	if off < 0 {
		return 0, errors.New("fio: negative offset")
	}
	return f.readAt(p, off)
}

func (f *File) readAt(p []byte, off int64) (int, error) {
	if len(p) == 0 {
		return 0, nil
	}
	var n uint32
	var ov *windows.Overlapped
	if off >= 0 {
		ov = &windows.Overlapped{Offset: uint32(off), OffsetHigh: uint32(uint64(off) >> 32)}
	}
	err := windows.ReadFile(f.h, p, &n, ov)
	if err != nil {
		if errors.Is(err, windows.ERROR_HANDLE_EOF) {
			return int(n), io.EOF
		}
		return int(n), err
	}
	if n == 0 {
		return 0, io.EOF
	}
	return int(n), nil
}

// Close releases the handle.
func (f *File) Close() error { return windows.CloseHandle(f.h) }

var (
	_ io.Reader   = (*File)(nil)
	_ io.ReaderAt = (*File)(nil)
)

// longPath lets CreateFile reach paths over MAX_PATH.
func longPath(path string) string {
	if len(path) < 248 || strings.HasPrefix(path, `\\?\`) {
		return path
	}
	if strings.HasPrefix(path, `\\`) {
		return `\\?\UNC\` + path[2:]
	}
	return `\\?\` + path
}
