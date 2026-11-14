package scanner

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

func TestCollectAndFilters(t *testing.T) {
	dir := t.TempDir()
	must := func(p string, n int) {
		t.Helper()
		if err := os.WriteFile(p, make([]byte, n), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	sub := filepath.Join(dir, "sub")
	if err := os.MkdirAll(sub, 0o755); err != nil {
		t.Fatal(err)
	}
	must(filepath.Join(dir, "a.bin"), 3)
	must(filepath.Join(dir, "b.txt"), 4)
	must(filepath.Join(sub, "c.bin"), 5)
	must(filepath.Join(sub, "d.nfo"), 6)
	_ = os.WriteFile(filepath.Join(dir, ".hidden"), []byte("x"), 0o644)

	files, err := Collect(context.Background(), []string{dir}, dir, Options{
		Recursive: true, SkipHidden: true, Include: "*.bin;*.txt", Exclude: "*.txt", Sort: true,
	}, nil)
	if err != nil {
		t.Fatal(err)
	}
	var names []string
	for _, f := range files {
		names = append(names, f.RelPath)
	}
	want := []string{filepath.Join("a.bin"), filepath.Join("sub", "c.bin")}
	if len(names) != len(want) {
		t.Fatalf("got %v, want %v", names, want)
	}
	for i := range want {
		if names[i] != want[i] {
			t.Errorf("file %d = %q, want %q", i, names[i], want[i])
		}
	}
	if files[0].Size != 3 || files[1].Size != 5 {
		t.Errorf("sizes = %d, %d", files[0].Size, files[1].Size)
	}
}

func TestCollectDedup(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "a.bin")
	if err := os.WriteFile(p, []byte("abc"), 0o644); err != nil {
		t.Fatal(err)
	}
	files, err := Collect(context.Background(), []string{dir, p}, dir, Options{Recursive: true, SkipHidden: true}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(files) != 1 {
		t.Fatalf("got %d files: %+v", len(files), files)
	}
}

func TestCommonBase(t *testing.T) {
	dir := t.TempDir()
	a := filepath.Join(dir, "A")
	b := filepath.Join(dir, "B")
	if err := os.MkdirAll(a, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(b, 0o755); err != nil {
		t.Fatal(err)
	}
	if got := CommonBase([]string{a}); got != filepath.Clean(a) {
		t.Errorf("single dir = %q, want %q", got, a)
	}
	fa := filepath.Join(a, "x.bin")
	if err := os.WriteFile(fa, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	if got := CommonBase([]string{fa}); got != filepath.Clean(a) {
		t.Errorf("single file = %q, want %q", got, a)
	}
	fb := filepath.Join(b, "y.bin")
	if err := os.WriteFile(fb, []byte("y"), 0o644); err != nil {
		t.Fatal(err)
	}
	if got := CommonBase([]string{a, b}); got != filepath.Clean(dir) {
		t.Errorf("two dirs = %q, want %q", got, dir)
	}
}

func TestMatch(t *testing.T) {
	cases := []struct {
		name, pattern string
		want          bool
	}{
		{"file.txt", "*.txt", true},
		{"FILE.TXT", "*.txt", true},
		{"file.txt", "*.bin", false},
		{"data1.dat", "data?.dat", true},
		{"data12.dat", "data?.dat", false},
		{"a.b.c", "a.*", true},
		{"a.b.c", "*.b.*", true},
		{"anything", "*", true},
		{"", "*", true},
	}
	for _, c := range cases {
		if got := Match(c.name, c.pattern); got != c.want {
			t.Errorf("Match(%q, %q) = %v, want %v", c.name, c.pattern, got, c.want)
		}
	}
}

func TestFilter(t *testing.T) {
	f := NewFilter("*.iso; *.mkv", "sample*,*.nfo")
	for _, n := range []string{"movie.iso", "Movie.MKV"} {
		if !f.Allow(n) {
			t.Errorf("%q rejected", n)
		}
	}
	for _, n := range []string{"movie.nfo", "sample-1.mkv", "readme.txt"} {
		if f.Allow(n) {
			t.Errorf("%q accepted", n)
		}
	}
}
