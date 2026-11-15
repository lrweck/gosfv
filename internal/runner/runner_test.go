package runner

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"

	"testing"
	"time"

	"github.com/lrweck/gosfv/internal/checksum"
	"github.com/lrweck/gosfv/internal/hasher"
	"github.com/lrweck/gosfv/internal/hexfmt"
	"github.com/lrweck/gosfv/internal/model"
)

type capture struct {
	files  []model.File
	done   map[int]model.Status
	hashes map[int][]byte
	ticks  int
	last   Progress
}

func newCapture() *capture {
	return &capture{done: map[int]model.Status{}, hashes: map[int][]byte{}}
}

func (c *capture) opts(algo hasher.Kind) Options {
	return Options{
		Algo: algo,
		Scan: ScanOptions{Recursive: true, SkipHidden: true},
		Tick: 5 * time.Millisecond,
		OnFiles: func(files []model.File) {
			c.files = files
		},
		OnTick: func(p Progress, done []Completed) {
			c.ticks++
			c.last = p
			for _, d := range done {
				c.done[d.Index] = d.Status
				c.hashes[d.Index] = append([]byte(nil), d.Hash...)
			}
		},
	}
}

func write(t *testing.T, path string, data []byte) {
	t.Helper()
	if err := os.WriteFile(path, data, 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestCreate(t *testing.T) {
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, "sub"), 0o755); err != nil {
		t.Fatal(err)
	}
	write(t, filepath.Join(dir, "a.bin"), []byte("alpha"))
	write(t, filepath.Join(dir, "sub", "b.bin"), []byte("beta"))

	c := newCapture()
	rep, err := Create(context.Background(), []string{dir}, dir, c.opts(hasher.MD5))
	if err != nil {
		t.Fatal(err)
	}
	if len(c.files) != 2 {
		t.Fatalf("files = %d", len(c.files))
	}
	if rep.Progress.Total != 2 || rep.Progress.OK != 2 || rep.Progress.Done != 2 {
		t.Errorf("progress = %+v", rep.Progress)
	}
	if c.ticks == 0 {
		t.Error("no ticks")
	}
	for i, f := range c.files {
		if c.done[i] != model.OK {
			t.Errorf("%s status %v", f.FullPath(), c.done[i])
		}
		want := hasher.MD5.New()
		_, _ = want.Write([]byte(map[string]string{"a.bin": "alpha", "b.bin": "beta"}[f.Name]))
		if !bytes.Equal(c.hashes[i], want.Sum(nil)) {
			t.Errorf("%s hash mismatch", f.Name)
		}
	}
}

func TestVerifyAndBadMissing(t *testing.T) {
	dir := t.TempDir()
	a := filepath.Join(dir, "a.bin")
	b := filepath.Join(dir, "b.bin")
	write(t, a, []byte("alpha"))
	write(t, b, []byte("beta"))

	// Make a checksum file with an expected digest for a, a wrong one for b,
	// and one file that does not exist.
	files := []model.File{
		{Name: "a.bin", Rel: "a.bin", Dir: dir, Status: model.OK, Hash: md5sum([]byte("alpha"))},
		{Name: "b.bin", Rel: "b.bin", Dir: dir, Status: model.OK, Hash: md5sum([]byte("wrong"))},
		{Name: "missing.bin", Rel: "missing.bin", Dir: dir, Status: model.OK, Hash: md5sum([]byte("x"))},
	}
	sfv := filepath.Join(dir, "checksums.md5")
	if _, err := checksum.WriteFile(sfv, hasher.MD5, files, checksum.WriteOptions{Relative: true, SaveDir: dir}); err != nil {
		t.Fatal(err)
	}

	c := newCapture()
	rep, err := Verify(context.Background(), sfv, c.opts(hasher.MD5))
	if err != nil {
		t.Fatal(err)
	}
	if rep.Progress.OK != 1 || rep.Progress.Bad != 1 || rep.Progress.Missing != 1 {
		t.Errorf("progress = %+v", rep.Progress)
	}
	if len(rep.Problems) != 2 {
		t.Errorf("problems = %+v", rep.Problems)
	}
	if len(c.files) != 3 || c.files[2].Status != model.Missing {
		t.Errorf("files = %+v", c.files)
	}
	if c.done[0] != model.OK || c.done[1] != model.Bad {
		t.Errorf("statuses = %v", c.done)
	}
}

// TestCreateCRC32StandardByteOrder checks that a created CRC-32 digest is
// stored MSB-first, so the .sfv text matches what cksfv, QuickSFV and the
// SFV format define. Only the parser/verifier accepts the legacy reversal.
func TestCreateCRC32StandardByteOrder(t *testing.T) {
	dir := t.TempDir()
	write(t, filepath.Join(dir, "a.bin"), []byte("123456789"))

	c := newCapture()
	rep, err := Create(context.Background(), []string{dir}, dir, c.opts(hasher.CRC32))
	if err != nil {
		t.Fatal(err)
	}
	if rep.Progress.OK != 1 || len(c.files) != 1 {
		t.Fatalf("progress = %+v files = %d", rep.Progress, len(c.files))
	}
	if got := hexfmt.Upper(c.hashes[0]); got != "CBF43926" {
		t.Errorf("hash = %s, want CBF43926", got)
	}
	if !bytes.Equal(c.hashes[0], crc32sum([]byte("123456789"))) {
		t.Error("created digest differs from the standard library CRC-32")
	}
}

func TestRecheck(t *testing.T) {
	dir := t.TempDir()
	write(t, filepath.Join(dir, "a.bin"), []byte("alpha"))
	write(t, filepath.Join(dir, "b.bin"), []byte("beta"))
	files := []model.File{
		{ID: 1, Name: "a.bin", Rel: "a.bin", Dir: dir, Status: model.OK, Expected: md5sum([]byte("alpha"))},
		{ID: 2, Name: "b.bin", Rel: "b.bin", Dir: dir, Status: model.OK, Expected: md5sum([]byte("wrong"))},
	}

	c := newCapture()
	opt := c.opts(hasher.MD5)
	var rechecked []int
	opt.OnRecheck = func(fs []model.File, idx []int) {
		rechecked = append([]int(nil), idx...)
	}
	rep, err := Recheck(context.Background(), files, nil, opt)
	if err != nil {
		t.Fatal(err)
	}
	if rep.Progress.Total != 2 || rep.Progress.OK != 1 || rep.Progress.Bad != 1 {
		t.Errorf("progress = %+v", rep.Progress)
	}
	if len(rechecked) != 2 || rechecked[0] != 0 || rechecked[1] != 1 {
		t.Errorf("rechecked indices = %v", rechecked)
	}
	if c.done[0] != model.OK || c.done[1] != model.Bad {
		t.Errorf("statuses = %v", c.done)
	}

	// A single-index recheck only touches that row.
	c = newCapture()
	opt = c.opts(hasher.MD5)
	rep, err = Recheck(context.Background(), files, []int{1}, opt)
	if err != nil {
		t.Fatal(err)
	}
	if rep.Progress.Total != 1 || rep.Progress.Bad != 1 {
		t.Errorf("single progress = %+v", rep.Progress)
	}
	if c.done[1] != model.Bad {
		t.Errorf("single status = %v", c.done)
	}
	if _, ok := c.done[0]; ok {
		t.Errorf("index 0 should not have been rechecked: %v", c.done)
	}
}

func TestRecheckProgressIndex(t *testing.T) {
	dir := t.TempDir()
	write(t, filepath.Join(dir, "a.bin"), []byte("small"))
	data := make([]byte, 60<<20)
	write(t, filepath.Join(dir, "b.bin"), data)
	files := []model.File{
		{ID: 1, Name: "a.bin", Rel: "a.bin", Dir: dir, Status: model.OK, Expected: md5sum([]byte("small"))},
		{ID: 2, Name: "b.bin", Rel: "b.bin", Dir: dir, Status: model.OK, Expected: md5sum(nil)},
	}
	var seen []int32
	opt := Options{
		Algo: hasher.MD5,
		OnFileProgress: func(index int32, path string, done, total int64) {
			seen = append(seen, index)
		},
	}
	if _, err := Recheck(context.Background(), files, []int{1}, opt); err != nil {
		t.Fatal(err)
	}
	if len(seen) == 0 {
		t.Fatal("no per-file progress was reported")
	}
	for _, i := range seen {
		if i != 1 {
			t.Fatalf("progress index = %d, want the original 1", i)
		}
	}
}

func TestRecheckMissing(t *testing.T) {
	dir := t.TempDir()
	a := filepath.Join(dir, "a.bin")
	write(t, a, []byte("alpha"))
	write(t, filepath.Join(dir, "b.bin"), []byte("beta"))
	files := []model.File{
		{ID: 1, Name: "a.bin", Rel: "a.bin", Dir: dir, Status: model.OK, Expected: md5sum([]byte("alpha"))},
		{ID: 2, Name: "b.bin", Rel: "b.bin", Dir: dir, Status: model.OK, Expected: md5sum([]byte("beta"))},
	}
	if err := os.Remove(a); err != nil {
		t.Fatal(err)
	}
	c := newCapture()
	opt := c.opts(hasher.MD5)
	var stat model.Status
	opt.OnRecheck = func(fs []model.File, idx []int) {
		for j, i := range idx {
			if i == 0 {
				stat = fs[j].Status
			}
		}
	}
	rep, err := Recheck(context.Background(), files, nil, opt)
	if err != nil {
		t.Fatal(err)
	}
	if stat != model.Missing {
		t.Errorf("stat status = %v", stat)
	}
	if rep.Progress.Missing != 1 || rep.Progress.OK != 1 {
		t.Errorf("progress = %+v", rep.Progress)
	}
}

func TestRecheckErrorStatus(t *testing.T) {
	dir := t.TempDir()
	write(t, filepath.Join(dir, "a.bin"), []byte("alpha"))
	files := []model.File{{
		ID: 1, Name: "a.bin", Rel: "a.bin", Dir: dir,
		Status: model.Error, Expected: md5sum([]byte("alpha")),
	}}
	c := newCapture()
	rep, err := Recheck(context.Background(), files, nil, c.opts(hasher.MD5))
	if err != nil {
		t.Fatal(err)
	}
	if rep.Progress.OK != 1 || rep.Progress.Error != 0 {
		t.Errorf("progress = %+v", rep.Progress)
	}
	if c.done[0] != model.OK {
		t.Errorf("status = %v, want OK", c.done[0])
	}
}

func TestStatFiles(t *testing.T) {
	dir := t.TempDir()
	write(t, filepath.Join(dir, "a.bin"), []byte("1234"))
	if err := os.Mkdir(filepath.Join(dir, "folder"), 0o755); err != nil {
		t.Fatal(err)
	}
	files := []model.File{
		{Full: filepath.Join(dir, "a.bin")},
		{Full: filepath.Join(dir, "missing.bin")},
		{Full: filepath.Join(dir, "folder")},
	}
	statFiles(context.Background(), files, 4)
	if files[0].Size != 4 || files[0].Status != model.Queued {
		t.Errorf("file = %+v", files[0])
	}
	if files[1].Status != model.Missing {
		t.Errorf("missing = %+v", files[1])
	}
	if files[2].Status != model.Error {
		t.Errorf("directory = %+v", files[2])
	}
}

func TestVerifyLegacyCRC32Endian(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "a.bin")
	data := []byte("123456789")
	write(t, p, data)
	sum := crc32sum(data)
	reversed := []byte{sum[3], sum[2], sum[1], sum[0]}
	files := []model.File{{Name: "a.bin", Rel: "a.bin", Dir: dir, Status: model.OK, Hash: reversed}}
	sfv := filepath.Join(dir, "checksums.sfv")
	if _, err := checksum.WriteFile(sfv, hasher.CRC32, files, checksum.WriteOptions{Relative: true, SaveDir: dir}); err != nil {
		t.Fatal(err)
	}

	c := newCapture()
	rep, err := Verify(context.Background(), sfv, c.opts(hasher.CRC32))
	if err != nil {
		t.Fatal(err)
	}
	if rep.Progress.OK != 1 || rep.Progress.Bad != 0 {
		t.Errorf("progress = %+v", rep.Progress)
	}
	if !rep.Legacy {
		t.Error("legacy endianness not detected")
	}
}

func TestCancelCreate(t *testing.T) {
	dir := t.TempDir()
	data := bytes.Repeat([]byte("x"), 1<<20)
	for i := 0; i < 20; i++ {
		write(t, filepath.Join(dir, string(rune('a'+i))+".bin"), data)
	}
	ctx, cancel := context.WithCancel(context.Background())
	c := newCapture()
	opt := c.opts(hasher.SHA256)
	opt.Sequential = true
	go func() {
		time.Sleep(2 * time.Millisecond)
		cancel()
	}()
	rep, err := Create(ctx, []string{dir}, dir, opt)
	if errors.Is(err, context.Canceled) {
		return // the walk itself can observe the cancel
	}
	if err != nil {
		t.Fatal(err)
	}
	if rep.Progress.Done >= rep.Progress.Total {
		t.Errorf("completed despite cancel: %+v", rep.Progress)
	}
}

func md5sum(b []byte) []byte {
	h := hasher.MD5.New()
	_, _ = h.Write(b)
	return h.Sum(nil)
}

func crc32sum(b []byte) []byte {
	h := hasher.CRC32.New()
	_, _ = h.Write(b)
	return h.Sum(nil)
}
