//go:build windows

// Package shell registers GoSFV in Explorer's context menu and as a handler for
// checksum files.
//
// Everything is written under HKEY_CURRENT_USER, so no elevation is needed and
// uninstalling only removes the current user's entries.
package shell

import (
	"errors"
	"fmt"
	"os"

	"golang.org/x/sys/windows/registry"
)

// ErrUnsupported is returned on systems without the Explorer registry.
var ErrUnsupported = errors.New("Explorer integration is only available on Windows")

const (
	progID = "GoSFV.Checksum"
	base   = `Software\Classes`
)

// Register adds the context-menu entries and file associations for exe.
func Register(exe string) error {
	for _, class := range []string{`Directory`, `*`} {
		path := base + `\` + class + `\shell\GoSFV`
		k, _, err := registry.CreateKey(registry.CURRENT_USER, path, registry.WRITE)
		if err != nil {
			return err
		}
		_ = k.SetStringValue("", "GoSFV: Create Checksum")
		_ = k.SetStringValue("Icon", exe)
		_ = k.SetStringValue("MultiSelectModel", "Player")
		cmd, _, err := registry.CreateKey(k, "command", registry.WRITE)
		if err == nil {
			_ = cmd.SetStringValue("", fmt.Sprintf(`"%s" -create "%%1"`, exe))
			_ = cmd.Close()
		}
		_ = k.Close()
	}

	// The ProgID that opens checksum files.
	p, _, err := registry.CreateKey(registry.CURRENT_USER, base+`\`+progID, registry.WRITE)
	if err != nil {
		return err
	}
	_ = p.SetStringValue("", "GoSFV Checksum File")
	icon, _, err := registry.CreateKey(p, `DefaultIcon`, registry.WRITE)
	if err == nil {
		_ = icon.SetStringValue("", exe+",0")
		_ = icon.Close()
	}
	cmd, _, err := registry.CreateKey(p, `shell\open\command`, registry.WRITE)
	if err == nil {
		_ = cmd.SetStringValue("", fmt.Sprintf(`"%s" "%%1"`, exe))
		_ = cmd.Close()
	}
	_ = p.Close()

	for _, ext := range []string{".sfv", ".md5", ".sha1", ".sha256", ".xxh3"} {
		k, _, err := registry.CreateKey(registry.CURRENT_USER, base+`\`+ext, registry.READ|registry.WRITE)
		if err != nil {
			continue
		}
		// Do not take over an association the user already has. Leave it
		// alone unless it is empty or already ours.
		current, _, _ := k.GetStringValue("")
		if current == "" || current == progID {
			_ = k.SetStringValue("", progID)
		}
		_ = k.Close()
	}
	return nil
}

// Unregister removes what Register added.
func Unregister() error {
	for _, path := range []string{
		base + `\Directory\shell\GoSFV`,
		base + `\*\shell\GoSFV`,
		base + `\` + progID,
	} {
		if err := deleteTree(path); err != nil {
			return err
		}
	}
	for _, ext := range []string{".sfv", ".md5", ".sha1", ".sha256", ".xxh3"} {
		k, err := registry.OpenKey(registry.CURRENT_USER, base+`\`+ext, registry.QUERY_VALUE|registry.SET_VALUE)
		if err != nil {
			continue
		}
		if v, _, err := k.GetStringValue(""); err == nil && v == progID {
			_ = k.DeleteValue("")
		}
		_ = k.Close()
	}
	return nil
}

// Registered reports whether the current user's context menu entry exists.
func Registered() bool {
	k, err := registry.OpenKey(registry.CURRENT_USER, base+`\Directory\shell\GoSFV\command`, registry.QUERY_VALUE)
	if err != nil {
		return false
	}
	defer k.Close()
	v, _, err := k.GetStringValue("")
	return err == nil && v != ""
}

// Self returns the running executable, quoted for a registry command.
func Self() string {
	exe, err := os.Executable()
	if err != nil {
		return ""
	}
	return exe
}

func deleteTree(path string) error {
	k, err := registry.OpenKey(registry.CURRENT_USER, path, registry.ENUMERATE_SUB_KEYS|registry.QUERY_VALUE|registry.SET_VALUE)
	if err != nil {
		if errors.Is(err, registry.ErrNotExist) {
			return nil
		}
		return err
	}
	names, err := k.ReadSubKeyNames(-1)
	_ = k.Close()
	if err != nil {
		return err
	}
	for _, name := range names {
		if err := deleteTree(path + `\` + name); err != nil {
			return err
		}
	}
	if err := registry.DeleteKey(registry.CURRENT_USER, path); err != nil && !errors.Is(err, registry.ErrNotExist) {
		return err
	}
	return nil
}
