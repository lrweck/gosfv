package cli

import (
	"strings"
	"testing"

	"gosfv/internal/hasher"
	"gosfv/internal/settings"
)

func TestParseSingleDashLongFlags(t *testing.T) {
	f, err := Parse([]string{"-headless", "-create", "-quiet", "-algo=crc32", "-o", "out.sfv", `C:\a`, `C:\b`})
	if err != nil {
		t.Fatal(err)
	}
	if !f.Headless || !f.Create || !f.Quiet {
		t.Errorf("booleans = %+v", f)
	}
	if f.Algo != hasher.CRC32 || !f.AlgoSet {
		t.Errorf("algo = %v set=%v", f.Algo, f.AlgoSet)
	}
	if f.Output != "out.sfv" {
		t.Errorf("output = %q", f.Output)
	}
	if len(f.Paths) != 2 || f.Paths[0] != `C:\a` {
		t.Errorf("paths = %v", f.Paths)
	}
}

func TestParseDoubleDashAndSeparateAlgo(t *testing.T) {
	f, err := Parse([]string{"--headless", "--algo", "sha256", "--theme", "dark", "--", "-odd-name"})
	if err != nil {
		t.Fatal(err)
	}
	if !f.Headless || f.Algo != hasher.SHA256 || !f.AlgoSet {
		t.Errorf("parsed = %+v", f)
	}
	if !f.ThemeSet || f.Theme != settings.ThemeDark {
		t.Errorf("theme = %v set=%v", f.Theme, f.ThemeSet)
	}
	if len(f.Paths) != 1 || f.Paths[0] != "-odd-name" {
		t.Errorf("paths = %v", f.Paths)
	}
}

func TestParseShorthands(t *testing.T) {
	f, err := Parse([]string{"-q", "-h", "-o=x.sfv"})
	if err != nil {
		t.Fatal(err)
	}
	if !f.Quiet || !f.Help || f.Output != "x.sfv" {
		t.Errorf("parsed = %+v", f)
	}
}

func TestParseRejectsUnknown(t *testing.T) {
	if _, err := Parse([]string{"-nope"}); err == nil {
		t.Error("unknown flag accepted")
	}
	if _, err := Parse([]string{"-algo", "crc64"}); err == nil {
		t.Error("unknown algorithm accepted")
	}
	if _, err := Parse([]string{"-theme", "sepia"}); err == nil {
		t.Error("unknown theme accepted")
	}
}

func TestUsageListsEveryFlag(t *testing.T) {
	text := Usage()
	for _, want := range []string{"--headless", "--create", "--quiet", "--algo", "--output", "--ini", "--theme", "--version"} {
		if !strings.Contains(text, want) {
			t.Errorf("usage lacks %s", want)
		}
	}
}
