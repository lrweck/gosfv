package gui

import (
	"context"
	"os"
	"time"

	"github.com/lrweck/gosfv/internal/hexfmt"
	"github.com/lrweck/gosfv/internal/model"
)

// selectionDetailLimit bounds the selection the details sidebar summarizes.
// Counting is always cheap; walking the chosen files is not, so a larger
// selection shows its count and stops there. The work below runs off the
// main thread either way.
const selectionDetailLimit = 1000

// selectionSig identifies a choice without walking it: how many rows are
// chosen and which one was chosen last. The table updates Active on every
// click, so this catches a single row moving as well as a multi-selection
// growing or shrinking.
type selectionSig struct {
	Count  int
	Active int
}

// selectionInfo is a snapshot the worker publishes and the view reads on
// the main thread. It holds values only: emitting the same struct twice
// must not share a pointer the worker later mutates.
type selectionInfo struct {
	Count    int
	Found    int
	Loading  bool
	Progress float64
	TooMany  bool
	Active   int

	TotalSize    int64
	OK           int
	Bad          int
	Missing      int
	Error        int
	Waiting      int
	TotalElapsed time.Duration
	Folders      int
	LargestName  string
	LargestSize  int64
	SmallestName string
	SmallestSize int64

	HasSingle bool
	Single    fileDetail
}

// fileDetail is what the sidebar shows for one file. StatOK says whether
// os.Stat answered; the model already carries the size and status, so the
// panel has something to show the moment the row is clicked.
type fileDetail struct {
	Name     string
	Rel      string
	Path     string
	Dir      string
	Status   model.Status
	Size     int64
	Elapsed  time.Duration
	Hash     string
	Expected string
	Err      string

	StatOK  bool
	StatErr string
	ModTime time.Time
	Mode    os.FileMode
}

// refreshSelection starts a details job when the choice changed, or when
// force asks for one. The snapshot is at most selectionDetailLimit files,
// and it is taken on the main thread only for that reason.
func (a *App) refreshSelection(force bool) {
	n := a.selection.Len()
	sig := selectionSig{Count: n, Active: a.activeRow}
	if !force && sig == a.selSig {
		return
	}
	a.selSig = sig
	a.cancelSelection()
	if n == 0 {
		a.sel = selectionInfo{}
		return
	}
	if n > selectionDetailLimit {
		a.sel = selectionInfo{Count: n, TooMany: true, Active: a.activeRow}
		return
	}
	files := a.selectionFiles()
	if len(files) == 0 {
		a.sel = selectionInfo{Count: n}
		return
	}
	seq := a.selSeq
	a.selAt = time.Now()
	a.sel = selectionInfo{Count: n, Found: len(files), Loading: true, Active: a.activeRow}
	if len(files) == 1 {
		// The model already knows the name, status, size and digests; show
		// them in this frame instead of waiting for the worker's first tick.
		a.sel.HasSingle = true
		a.sel.Single = detailOf(&files[0])
	}
	ctx, cancel := context.WithCancel(context.Background())
	a.selCancel = cancel
	go computeSelection(ctx, n, files, func(info selectionInfo) {
		a.onMain(func() {
			if seq != a.selSeq {
				return
			}
			a.sel = info
			a.invalidate()
		})
	})
}

func (a *App) cancelSelection() {
	a.selSeq++
	if a.selCancel != nil {
		a.selCancel()
		a.selCancel = nil
	}
}

// selectionFiles copies the chosen model.File values for the worker. The
// copy is what makes the job race free: the main thread may update statuses
// and hashes while it runs, and the worker only sees the snapshot.
func (a *App) selectionFiles() []model.File {
	n := a.selection.Len()
	files := make([]model.File, 0, n)
	for id := range a.selection.All() {
		if f, ok := a.fileAt(id); ok {
			files = append(files, f)
		}
	}
	if len(files) == 0 && a.activeRow >= 0 && a.activeRow < len(a.viewRows) {
		files = append(files, a.store[a.viewRows[a.activeRow]])
	}
	return files
}

// fileAt resolves an ID to the file without walking the store. A freshly
// installed list has IDs 1..n, so the direct index is enough; only a list
// that lost rows needs the byID table.
func (a *App) fileAt(id uint64) (model.File, bool) {
	if id == 0 {
		return model.File{}, false
	}
	if a.byID != nil {
		if id < uint64(len(a.byID)) {
			if v := a.byID[id]; v > 0 && v-1 < len(a.store) {
				return a.store[v-1], true
			}
		}
		return model.File{}, false
	}
	if int(id) <= len(a.store) && a.store[id-1].ID == id {
		return a.store[id-1], true
	}
	// Sparse store without a table (tests and hand-built lists only).
	for i := range a.store {
		if a.store[i].ID == id {
			return a.store[i], true
		}
	}
	return model.File{}, false
}

// reindex rebuilds the ID-to-row table after rows are removed, which makes
// IDs sparse. A slice costs less than a map and needs no hashing.
func (a *App) reindex() {
	maxID := uint64(0)
	for i := range a.store {
		if a.store[i].ID > maxID {
			maxID = a.store[i].ID
		}
	}
	if maxID == 0 {
		a.byID = nil
		return
	}
	// The value is the row plus one: zero means "not in the list", which
	// saves filling the slice with -1.
	idx := make([]int, maxID+1)
	for i := range a.store {
		if id := a.store[i].ID; id < uint64(len(idx)) {
			idx[id] = i + 1
		}
	}
	a.byID = idx
}

// detailOf copies the facts the model already has. It is cheap enough to
// call on the main thread for one file.
func detailOf(f *model.File) fileDetail {
	return fileDetail{
		Name:     f.Name,
		Rel:      f.Rel,
		Path:     f.FullPath(),
		Dir:      f.Dir,
		Status:   f.Status,
		Size:     f.Size,
		Elapsed:  f.Elapsed,
		Hash:     hexfmt.Upper(f.Hash),
		Expected: hexfmt.Upper(f.Expected),
		Err:      f.Err,
	}
}

// computeSelection summarizes a selection in the background. Count is the
// number of chosen rows; files may be shorter when an item left the list
// between the click and the snapshot. Only a single file gets os.Stat;
// a multi-selection is summarized from the model.
func computeSelection(ctx context.Context, count int, files []model.File, emit func(selectionInfo)) {
	if ctx.Err() != nil {
		return
	}
	info := selectionInfo{Count: count, Found: len(files), Loading: true}
	if len(files) == 1 {
		d := detailOf(&files[0])
		info.HasSingle = true
		info.Single = d
		// The caller already painted these known facts; emitting them
		// again would only repaint the same panel before the stat answer.
		if ctx.Err() != nil {
			return
		}

		fi, err := os.Stat(d.Path)
		if err != nil {
			info.Single.StatErr = err.Error()
		} else {
			info.Single.StatOK = true
			info.Single.Size = fi.Size()
			info.Single.ModTime = fi.ModTime()
			info.Single.Mode = fi.Mode()
		}
		info.Loading = false
		info.Progress = 1
		emit(info)
		return
	}

	const batch = 256
	folders := make(map[string]struct{}, 8)
	for i := range files {
		if ctx.Err() != nil {
			return
		}
		f := &files[i]
		info.TotalSize += f.Size
		info.TotalElapsed += f.Elapsed
		switch f.Status {
		case model.OK:
			info.OK++
		case model.Bad:
			info.Bad++
		case model.Missing:
			info.Missing++
		case model.Error:
			info.Error++
		default:
			info.Waiting++
		}
		if f.Dir != "" {
			folders[f.Dir] = struct{}{}
		}
		if info.LargestName == "" || f.Size > info.LargestSize {
			info.LargestName, info.LargestSize = f.Name, f.Size
		}
		if info.SmallestName == "" || f.Size < info.SmallestSize {
			info.SmallestName, info.SmallestSize = f.Name, f.Size
		}
		if (i+1)%batch == 0 || i+1 == len(files) {
			info.Progress = float64(i+1) / float64(len(files))
			emit(info)
		}
	}
	info.Folders = len(folders)
	info.Loading = false
	info.Progress = 1
	emit(info)
}
