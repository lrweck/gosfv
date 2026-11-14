package gui

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/lrweck/gosfv/internal/model"
)

func TestComputeSelectionSingleStats(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "a.bin")
	if err := os.WriteFile(path, []byte("123456789"), 0o644); err != nil {
		t.Fatal(err)
	}
	f := model.File{
		ID: 1, Name: "a.bin", Rel: "a.bin", Dir: dir, Size: 9, Status: model.OK,
		Hash: []byte{0xCB, 0xF4, 0x39, 0x26},
	}
	var updates []selectionInfo
	computeSelection(context.Background(), 1, []model.File{f}, func(info selectionInfo) {
		updates = append(updates, info)
	})
	if len(updates) != 2 {
		t.Fatalf("updates = %d, want 2", len(updates))
	}
	if !updates[0].Loading || !updates[0].HasSingle {
		t.Errorf("first update = %+v", updates[0])
	}
	last := updates[len(updates)-1]
	if last.Loading || last.Progress != 1 {
		t.Errorf("last update = %+v", last)
	}
	if !last.Single.StatOK || last.Single.Size != 9 || last.Single.ModTime.IsZero() {
		t.Errorf("stat = %+v", last.Single)
	}
	if last.Single.Hash != "CBF43926" {
		t.Errorf("hash = %q", last.Single.Hash)
	}
}

func TestComputeSelectionSummary(t *testing.T) {
	files := []model.File{
		{ID: 1, Name: "a", Dir: `C:\one`, Size: 10, Status: model.OK, Elapsed: time.Second},
		{ID: 2, Name: "b", Dir: `C:\one`, Size: 20, Status: model.Bad, Elapsed: 2 * time.Second},
		{ID: 3, Name: "c", Dir: `C:\two`, Size: 30, Status: model.Missing},
	}
	var last selectionInfo
	computeSelection(context.Background(), len(files), files, func(info selectionInfo) {
		last = info
	})
	if last.Loading || last.Progress != 1 {
		t.Errorf("state = %+v", last)
	}
	if last.Count != 3 || last.Found != 3 {
		t.Errorf("counts = %d/%d", last.Count, last.Found)
	}
	if last.TotalSize != 60 || last.TotalElapsed != 3*time.Second {
		t.Errorf("totals = %d %v", last.TotalSize, last.TotalElapsed)
	}
	if last.OK != 1 || last.Bad != 1 || last.Missing != 1 {
		t.Errorf("statuses = %+v", last)
	}
	if last.Folders != 2 {
		t.Errorf("folders = %d", last.Folders)
	}
	if last.LargestName != "c" || last.LargestSize != 30 || last.SmallestName != "a" || last.SmallestSize != 10 {
		t.Errorf("largest/smallest = %s/%d %s/%d", last.LargestName, last.LargestSize, last.SmallestName, last.SmallestSize)
	}
	if last.HasSingle {
		t.Error("multi summary marked as single")
	}
	// The worker must not touch the disk for a multi-selection.
	if last.Error != 0 || last.Waiting != 0 {
		t.Errorf("unexpected statuses = %+v", last)
	}
}

func TestComputeSelectionCancel(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	called := false
	computeSelection(ctx, 1, []model.File{{Name: "a"}}, func(selectionInfo) { called = true })
	if called {
		t.Error("canceled job emitted")
	}
}

func TestSelectionThreshold(t *testing.T) {
	a := testApp()
	a.selSig = selectionSig{}
	for i := 0; i < selectionDetailLimit+1; i++ {
		a.selection.Add(uint64(1000 + i))
	}
	a.refreshSelection(true)
	if !a.sel.TooMany || a.sel.Count != selectionDetailLimit+1 {
		t.Errorf("selection = %+v", a.sel)
	}
	if a.sel.Loading {
		t.Error("large selection started a job")
	}
}

func TestSelectionFilesUsesIndex(t *testing.T) {
	a := testApp()
	a.selection.Clear()
	a.selection.Add(a.store[2].ID)
	got := a.selectionFiles()
	if len(got) != 1 || got[0].Name != "c.bin" {
		t.Fatalf("files = %+v", got)
	}
	// A removed item leaves its key behind; it must not corrupt the rest.
	a.removeIndices([]int{2})
	if got := a.selectionFiles(); len(got) != 0 {
		t.Errorf("files after removal = %+v", got)
	}
}

func TestFormatBytes(t *testing.T) {
	cases := map[int64]string{
		0:        "0 B",
		1023:     "1023 B",
		1024:     "1.0 KiB",
		18 << 30: "18.0 GiB",
		1536:     "1.5 KiB",
	}
	for in, want := range cases {
		if got := formatBytes(in); got != want {
			t.Errorf("formatBytes(%d) = %q, want %q", in, got, want)
		}
	}
}
