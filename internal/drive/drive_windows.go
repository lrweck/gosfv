//go:build windows

package drive

import (
	"path/filepath"
	"strings"
	"unsafe"

	"golang.org/x/sys/windows"
)

const (
	ioctlStorageQueryProperty     = 0x002d1400
	ioctlVolumeGetDiskExtents     = 0x00560000
	storageDeviceProperty         = 0
	storageDeviceSeekPenalty      = 7
	storagePropertyStandardQuery  = 0
	driveRemote                   = 4
	driveFixed                    = 3
	volumeDiskExtentsBufferLength = 1024
)

type storagePropertyQuery struct {
	PropertyID uint32
	QueryType  uint32
	_          [1]byte
}

type deviceSeekPenaltyDescriptor struct {
	Version           uint32
	Size              uint32
	IncursSeekPenalty byte
	_                 [3]byte
}

type storageDeviceDescriptor struct {
	Version               uint32
	Size                  uint32
	DeviceType            byte
	DeviceTypeModifier    byte
	RemovableMedia        byte
	CommandQueueing       byte
	VendorIDOffset        uint32
	ProductIDOffset       uint32
	ProductRevisionOffset uint32
	SerialNumberOffset    uint32
	BusType               uint32
	RawPropertiesLength   uint32
}

type diskExtent struct {
	DiskNumber     uint32
	_              uint32
	StartingOffset int64
	ExtentLength   int64
}

type volumeDiskExtents struct {
	NumberOfDiskExtents uint32
	_                   uint32
	Extents             [1]diskExtent
}

func detect(path string) Info {
	abs, err := filepath.Abs(path)
	if err != nil {
		abs = path
	}
	root := filepath.VolumeName(abs)
	if root == "" {
		return Info{Source: "no volume name", Rotational: false}
	}
	root += `\`
	info := Info{Root: root}

	if p, err := windows.UTF16PtrFromString(root); err == nil {
		switch windows.GetDriveType(p) {
		case driveRemote:
			info.Rotational = true
			info.Known = true
			info.Source = "network share (sequential is safer)"
			return info
		case driveFixed, 2: // removable
		default:
			info.Source = "unknown drive type"
			return info
		}
	}

	volume, err := openVolume(root)
	if err != nil {
		info.Source = "could not open volume: " + err.Error()
		return info
	}
	if rotational, known, product, err := queryDevice(volume); err == nil {
		info.Product = product
		if known {
			info.Rotational = rotational
			info.Known = true
			info.Source = "volume seek-penalty property"
			windows.CloseHandle(volume)
			return info
		}
		if product != "" && ssdByName(product) {
			info.Rotational = false
			info.Known = true
			info.Source = "product name"
			windows.CloseHandle(volume)
			return info
		}
	}

	// The volume did not answer (BitLocker, Storage Spaces, some USB
	// bridges). Ask the physical disk that holds it.
	if disk, err := physicalDisk(volume); err == nil {
		name := `\\.\PhysicalDrive` + itoa(int(disk))
		if p, err := windows.UTF16PtrFromString(name); err == nil {
			if ph, err := windows.CreateFile(p, 0,
				windows.FILE_SHARE_READ|windows.FILE_SHARE_WRITE, nil,
				windows.OPEN_EXISTING, 0, 0); err == nil {
				if rotational, known, product, err := queryDevice(ph); err == nil {
					info.Product = product
					if product != "" {
						info.Detail = "physical disk " + itoa(int(disk)) + ": " + product
					} else {
						info.Detail = "physical disk " + itoa(int(disk))
					}
					if known {
						info.Rotational = rotational
						info.Known = true
						info.Source = "physical disk seek-penalty property"
						windows.CloseHandle(ph)
						windows.CloseHandle(volume)
						return info
					}
					if product != "" && ssdByName(product) {
						info.Rotational = false
						info.Known = true
						info.Source = "physical disk product name"
						windows.CloseHandle(ph)
						windows.CloseHandle(volume)
						return info
					}
				}
				windows.CloseHandle(ph)
			}
		}
	}
	windows.CloseHandle(volume)

	// The registry knows the model of most disks even when the device
	// refuses the IOCTL. As a last resort, fixed drives are treated as flash
	// because that is what most machines use in 2026; the settings can force
	// sequential mode.
	info.Rotational = false
	info.Source = "assumed flash (override in Options > Processing Mode)"
	return info
}

func openVolume(root string) (windows.Handle, error) {
	name := `\\.\` + strings.TrimSuffix(root, `\`)
	p, err := windows.UTF16PtrFromString(name)
	if err != nil {
		return 0, err
	}
	return windows.CreateFile(p, 0,
		windows.FILE_SHARE_READ|windows.FILE_SHARE_WRITE, nil,
		windows.OPEN_EXISTING, 0, 0)
}

// queryDevice asks for the seek-penalty flag and the product name. known is
// false when the device answered with a product name but not with the
// rotational property.
func queryDevice(h windows.Handle) (rotational, known bool, product string, err error) {
	q := storagePropertyQuery{PropertyID: storageDeviceSeekPenalty, QueryType: storagePropertyStandardQuery}
	var out [64]byte
	var returned uint32
	if err := windows.DeviceIoControl(h, ioctlStorageQueryProperty,
		(*byte)(unsafe.Pointer(&q)), uint32(unsafe.Sizeof(q)),
		&out[0], uint32(len(out)), &returned, nil); err == nil && returned >= 12 {
		d := (*deviceSeekPenaltyDescriptor)(unsafe.Pointer(&out[0]))
		if d.Size >= 12 {
			return d.IncursSeekPenalty != 0, true, "", nil
		}
	}
	// Product name fallback.
	q.PropertyID = storageDeviceProperty
	out2 := make([]byte, 2048)
	if err := windows.DeviceIoControl(h, ioctlStorageQueryProperty,
		(*byte)(unsafe.Pointer(&q)), uint32(unsafe.Sizeof(q)),
		&out2[0], uint32(len(out2)), &returned, nil); err != nil {
		return false, false, "", err
	}
	if returned < uint32(unsafe.Sizeof(storageDeviceDescriptor{})) {
		return false, false, "", nil
	}
	d := (*storageDeviceDescriptor)(unsafe.Pointer(&out2[0]))
	start := int(d.ProductIDOffset)
	if start > 0 && start < len(out2) {
		end := start
		for end < len(out2) && out2[end] != 0 {
			end++
		}
		product = strings.TrimSpace(string(out2[start:end]))
	}
	return false, false, product, nil
}

func physicalDisk(volume windows.Handle) (uint32, error) {
	var out [volumeDiskExtentsBufferLength]byte
	var returned uint32
	if err := windows.DeviceIoControl(volume, ioctlVolumeGetDiskExtents, nil, 0,
		&out[0], uint32(len(out)), &returned, nil); err != nil {
		return 0, err
	}
	if returned < 16 {
		return 0, windows.ERROR_INVALID_DATA
	}
	d := (*volumeDiskExtents)(unsafe.Pointer(&out[0]))
	if d.NumberOfDiskExtents == 0 {
		return 0, windows.ERROR_NOT_FOUND
	}
	return d.Extents[0].DiskNumber, nil
}

func ssdByName(name string) bool {
	n := strings.ToUpper(name)
	for _, s := range []string{"SSD", "NVME", "FLASH", "M.2"} {
		if strings.Contains(n, s) {
			return true
		}
	}
	return false
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	var b [12]byte
	i := len(b)
	for n > 0 {
		i--
		b[i] = byte('0' + n%10)
		n /= 10
	}
	return string(b[i:])
}
