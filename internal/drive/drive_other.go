//go:build !windows

package drive

import "path/filepath"

func detect(path string) Info {
	abs, err := filepath.Abs(path)
	if err != nil {
		abs = path
	}
	return Info{
		Root:       filepath.VolumeName(abs) + string(filepath.Separator),
		Rotational: false,
		Known:      false,
		Source:     "non-Windows: assumed flash (override in Options > Processing Mode)",
	}
}
