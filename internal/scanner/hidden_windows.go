//go:build windows

package scanner

import (
	"io/fs"
	"strings"

	"golang.org/x/sys/windows"
)

// isHidden keeps SharpSFV's behavior: dot names, hidden files and system files
// are skipped. The attributes come from one Win32 call; walking itself remains
// filepath.WalkDir.
func isHidden(path string, d fs.DirEntry) bool {
	if strings.HasPrefix(d.Name(), ".") {
		return true
	}
	p, err := windows.UTF16PtrFromString(path)
	if err != nil {
		return false
	}
	attrs, err := windows.GetFileAttributes(p)
	if err != nil {
		return false
	}
	return attrs&(windows.FILE_ATTRIBUTE_HIDDEN|windows.FILE_ATTRIBUTE_SYSTEM) != 0
}
