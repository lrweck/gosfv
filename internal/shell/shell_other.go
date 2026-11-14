//go:build !windows

// Package shell registers GoSFV in the desktop's context menu.
package shell

import (
	"errors"
	"os"
)

// ErrUnsupported is returned on systems without the Explorer registry.
var ErrUnsupported = errors.New("Explorer integration is only available on Windows")

// Register is unsupported outside Windows.
func Register(exe string) error { return ErrUnsupported }

// Unregister is unsupported outside Windows.
func Unregister() error { return ErrUnsupported }

// Registered reports whether integration exists.
func Registered() bool { return false }

// Self returns the running executable.
func Self() string {
	exe, _ := os.Executable()
	return exe
}
