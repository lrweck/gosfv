//go:build !windows

// Package fio opens files for the hashing engine. On non-Windows systems it is
// a thin wrapper over os.File.
package fio

import "os"

// File is an open file.
type File struct {
	f    *os.File
	size int64
}

// Open opens path for reading.
func Open(path string) (*File, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	info, err := f.Stat()
	if err != nil {
		_ = f.Close()
		return nil, err
	}
	return &File{f: f, size: info.Size()}, nil
}

// Size returns the file length in bytes.
func (f *File) Size() int64 { return f.size }

// Read fills p.
func (f *File) Read(p []byte) (int, error) { return f.f.Read(p) }

// ReadAt fills p from off.
func (f *File) ReadAt(p []byte, off int64) (int, error) { return f.f.ReadAt(p, off) }

// Close closes the file.
func (f *File) Close() error { return f.f.Close() }
