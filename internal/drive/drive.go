// Package drive detects whether the volume holding a path is rotational.
//
// The answer picks the reader model: one sequential reader for a hard disk,
// many parallel readers for flash. On Windows the detection queries the volume
// with IOCTL_STORAGE_QUERY_PROPERTY, then the physical disk behind it when a
// volume does not answer (BitLocker and RAID often hide it), and falls back to
// the device's product name. Other systems assume flash, and the processing
// mode can always be forced in the settings.
package drive

import "fmt"

// Mode is the user's processing preference.
type Mode uint8

const (
	Auto Mode = iota
	HDD
	SSD
)

func (m Mode) String() string {
	switch m {
	case HDD:
		return "HDD"
	case SSD:
		return "SSD"
	}
	return "Auto"
}

// ParseMode accepts the INI values.
func ParseMode(s string) Mode {
	switch s {
	case "HDD", "hdd":
		return HDD
	case "SSD", "ssd":
		return SSD
	}
	return Auto
}

// Info describes what detection found.
type Info struct {
	Root       string
	Rotational bool
	Known      bool
	Source     string
	Product    string
	Detail     string
}

func (i Info) String() string {
	kind := "SSD/NVMe"
	if i.Rotational {
		kind = "HDD"
	}
	known := ""
	if !i.Known {
		known = " (assumed)"
	}
	s := fmt.Sprintf("%s: %s%s\nDecision: %s", i.Root, kind, known, i.Source)
	if i.Product != "" {
		s += "\nDevice: " + i.Product
	}
	if i.Detail != "" {
		s += "\n" + i.Detail
	}
	return s
}

// Detect inspects the volume that contains path.
func Detect(path string) Info {
	return detect(path)
}

// Sequential decides the engine's mode from the user's preference and the
// detected hardware. Auto mode follows the device.
func Sequential(mode Mode, info Info) bool {
	switch mode {
	case HDD:
		return true
	case SSD:
		return false
	}
	return info.Rotational
}

// IsRotational is the common case: the hardware answer, with an unknown device
// treated as flash. The settings let a user override it.
func IsRotational(path string) bool { return Detect(path).Rotational }
