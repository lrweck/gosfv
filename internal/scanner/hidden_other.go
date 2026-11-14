//go:build !windows

package scanner

import (
	"io/fs"
	"strings"
)

func isHidden(_ string, d fs.DirEntry) bool {
	return strings.HasPrefix(d.Name(), ".")
}
