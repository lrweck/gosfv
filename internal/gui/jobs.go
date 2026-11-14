package gui

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"

	"gosfv/internal/checksum"
	"gosfv/internal/drive"
	"gosfv/internal/engine"
	"gosfv/internal/hasher"
	"gosfv/internal/model"
	"gosfv/internal/runner"
	"gosfv/internal/scanner"
)

type jobRun struct {
	id     int
	ctx    context.Context
	inputs []string
	root   string
	name   string
	algo   hasher.Kind
	seq    bool
	scan   runner.ScanOptions
	gate   *engine.Gate
}

func (a *App) addJob(paths []string) {
	if a.closing.Load() {
		return
	}
	base, name := jobBase(paths)
	a.nextJob++
	job := model.Job{
		ID:     a.nextJob,
		Name:   name,
		Root:   base,
		Inputs: append([]string(nil), paths...),
		Status: model.JobQueued,
	}
	a.jobs = append(a.jobs, job)
	a.queueStop.Store(false)
	a.updateTitle()
	a.updateProblemMenus()
	a.invalidate()
	if !a.jobRunning {
		a.jobRunning = true
		go a.runJobs()
	}
}

func jobBase(paths []string) (string, string) {
	if len(paths) == 0 {
		return "", "Job"
	}
	if len(paths) == 1 {
		abs, err := filepath.Abs(paths[0])
		if err != nil {
			abs = paths[0]
		}
		if info, err := os.Stat(abs); err == nil && info.IsDir() {
			return filepath.Dir(abs), filepath.Base(abs)
		}
		return filepath.Dir(abs), filepath.Base(abs)
	}
	base := scanner.CommonBase(paths)
	name := filepath.Base(base)
	if name == "." || name == string(filepath.Separator) || name == "" {
		name = "Multi_Files"
	}
	return base, name
}

func (a *App) runJobs() {
	for {
		if a.closing.Load() {
			return
		}
		id := 0
		mygo.RunOnMain(func() { id = a.nextQueuedJob() })
		if id == 0 {
			mygo.RunOnMain(a.jobsIdle)
			return
		}
		a.runOneJob(id)
		if a.queueStop.Load() {
			mygo.RunOnMain(a.jobsIdle)
			return
		}
	}
}

func (a *App) jobsIdle() {
	a.jobRunning = false
	a.processing = false
	if !a.queueStop.Load() && a.nextQueuedJob() != 0 {
		a.jobRunning = true
		go a.runJobs()
		return
	}
	a.updateTitle()
	a.updateProblemMenus()
	a.syncAlgoMenu()
	a.syncViewMenu()
	a.invalidate()
}

func (a *App) nextQueuedJob() int {
	for i := range a.jobs {
		if a.jobs[i].Status == model.JobQueued {
			return a.jobs[i].ID
		}
	}
	return 0
}

func (a *App) findJob(id int) *model.Job {
	for i := range a.jobs {
		if a.jobs[i].ID == id {
			return &a.jobs[i]
		}
	}
	return nil
}

func (a *App) prepareJob(id int) (jobRun, bool) {
	var run jobRun
	j := a.findJob(id)
	if j == nil {
		return run, false
	}
	j.Status = model.JobScanning
	j.Progress = 0
	a.currentJob = id
	a.processing = true
	a.paused = false
	a.cancelled = false
	a.scanning = true
	a.scanCount = 0
	a.stats = runner.Progress{}
	a.elapsed = 0
	a.active = map[int32]activeFile{}
	a.activeView = nil
	a.store = nil
	a.viewRows = nil
	a.gate = engine.NewGate()
	ctx, cancel := context.WithCancel(context.Background())
	a.cancel = cancel
	run = jobRun{
		id:     id,
		ctx:    ctx,
		inputs: append([]string(nil), j.Inputs...),
		root:   j.Root,
		name:   j.Name,
		algo:   a.algo,
		scan:   a.scanOptions(),
		gate:   a.gate,
	}
	seqPath := j.Root
	if seqPath == "" && len(j.Inputs) > 0 {
		seqPath = j.Inputs[0]
	}
	run.seq = drive.Sequential(a.settings.ProcMode, drive.Detect(seqPath))
	a.updateTitle()
	a.updateProblemMenus()
	a.syncAlgoMenu()
	a.syncViewMenu()
	a.invalidate()
	return run, true
}

func (a *App) runOneJob(id int) {
	var run jobRun
	ok := false
	mygo.RunOnMain(func() { run, ok = a.prepareJob(id) })
	if !ok {
		return
	}
	opt := runner.Options{
		Algo:       run.algo,
		Sequential: run.seq,
		Gate:       run.gate,
		Scan:       run.scan,
		OnScan: func(found int) {
			mygo.RunOnMain(func() { a.scanCount = found })
			a.invalidate()
		},
		OnFiles: func(files []model.File) {
			mygo.RunOnMain(func() {
				a.installFiles(files)
				if j := a.findJob(id); j != nil {
					j.Status = model.JobInProgress
				}
			})
			a.invalidate()
		},
		OnTick: func(p runner.Progress, done []runner.Completed) {
			mygo.RunOnMain(func() {
				a.applyTick(p, done)
				a.updateJobProgress(p)
			})
			a.invalidate()
		},
		OnFileProgress: a.onFileProgress,
	}
	rep, err := runner.Create(run.ctx, run.inputs, run.root, opt)
	mygo.RunOnMain(func() { a.finishJob(id, run, rep, err) })
	a.invalidate()
}

func (a *App) finishJob(id int, run jobRun, rep *runner.Report, err error) {
	j := a.findJob(id)
	a.processing = false
	a.scanning = false
	a.paused = false
	a.cancel = nil
	a.currentJob = 0
	a.clearActive()
	if j == nil {
		a.updateTitle()
		a.invalidate()
		return
	}
	if err != nil {
		j.Status = model.JobError
		if rep != nil {
			j.Elapsed = rep.Progress.Elapsed
		}
		a.store = nil
		a.viewRows = nil
		a.notice = fmt.Sprintf("Job %s failed: %v", j.Name, err)
		a.updateTitle()
		a.updateProblemMenus()
		a.syncAlgoMenu()
		a.syncViewMenu()
		a.invalidate()
		return
	}
	if run.ctx.Err() != nil {
		j.Status = model.JobError
		if rep != nil {
			j.Elapsed = rep.Progress.Elapsed
		}
		a.store = nil
		a.viewRows = nil
		a.notice = fmt.Sprintf("Job %s stopped; no checksum file was written.", j.Name)
		a.updateTitle()
		a.updateProblemMenus()
		a.syncAlgoMenu()
		a.syncViewMenu()
		a.invalidate()
		return
	}
	if rep != nil {
		a.stats = rep.Progress
		a.elapsed = rep.Progress.Elapsed
		j.Elapsed = rep.Progress.Elapsed
	}
	root := run.root
	if root == "" {
		root = "."
	}
	safe := sanitizeName(j.Name)
	out := filepath.Join(root, fmt.Sprintf("%d.%s%s", j.ID, safe, run.algo.Ext()))
	n, werr := checksum.WriteFile(out, run.algo, a.store, checksum.WriteOptions{
		Relative:  true,
		SaveDir:   filepath.Dir(out),
		Prefix:    a.pathPrefix(),
		Comments:  a.settings.Comments,
		Signature: a.settings.Signature,
		OnlyOK:    true,
	})
	if werr != nil {
		j.Status = model.JobError
		a.notice = fmt.Sprintf("Job %s: writing %s: %v", j.Name, out, werr)
	} else {
		j.Output = out
		j.Progress = 100
		if rep != nil && (rep.Progress.Error > 0 || rep.Progress.Bad > 0 || rep.Progress.Done < rep.Progress.Total) {
			j.Status = model.JobError
			a.notice = fmt.Sprintf("Job %s wrote %d entries with %d problem(s).", j.Name, n, rep.Progress.Error+rep.Progress.Bad)
		} else {
			j.Status = model.JobDone
			a.notice = fmt.Sprintf("Job %s wrote %s (%d entries).", j.Name, out, n)
		}
	}
	a.store = nil
	a.viewRows = nil
	a.updateTitle()
	a.updateProblemMenus()
	a.syncAlgoMenu()
	a.syncViewMenu()
	a.invalidate()
}

func (a *App) updateJobProgress(p runner.Progress) {
	if a.currentJob == 0 {
		return
	}
	j := a.findJob(a.currentJob)
	if j == nil {
		return
	}
	if j.Status != model.JobPaused {
		j.Status = model.JobInProgress
	}
	if p.Total > 0 {
		j.Progress = float64(p.Done) / float64(p.Total) * 100
		if j.Progress > 100 {
			j.Progress = 100
		}
	}
}

func (a *App) setCurrentJobPaused(paused bool) {
	if a.currentJob == 0 {
		return
	}
	if j := a.findJob(a.currentJob); j != nil {
		if paused {
			j.Status = model.JobPaused
		} else if j.Status == model.JobPaused {
			j.Status = model.JobInProgress
		}
	}
}

func (a *App) clearCompletedJobs() {
	if a.processing {
		return
	}
	keep := a.jobs[:0]
	oldLen := len(a.jobs)
	for i := 0; i < oldLen; i++ {
		if a.jobs[i].Status != model.JobDone {
			keep = append(keep, a.jobs[i])
		}
	}
	for i := len(keep); i < oldLen; i++ {
		a.jobs[i] = model.Job{}
	}
	a.jobs = keep
	a.jobSel.Clear()
	a.updateTitle()
	a.updateProblemMenus()
	a.invalidate()
}

func (a *App) jobContextMenu(m *ui.Menu, row int) {
	if row < 0 || row >= len(a.jobs) {
		return
	}
	j := &a.jobs[row]
	if m.Item("Open Output Folder").Disabled(j.Output == "").Chosen() {
		output := j.Output
		a.deferAction(func() { mygo.Shell.ShowItemInFolder(output) })
	}
	m.Separator()
	if m.Item("Remove from Queue").Disabled(a.processing && j.Status != model.JobQueued).Chosen() {
		id := j.ID
		a.deferAction(func() { a.removeJob(id) })
	}
	if m.Item("Clear Completed Jobs").Disabled(!a.hasCompletedJobs()).Chosen() {
		a.deferAction(a.clearCompletedJobs)
	}
}

func (a *App) removeJob(id int) {
	for i := range a.jobs {
		if a.jobs[i].ID == id {
			if a.jobs[i].Status != model.JobQueued {
				return
			}
			a.jobs = append(a.jobs[:i], a.jobs[i+1:]...)
			a.jobSel.Clear()
			a.invalidate()
			return
		}
	}
}

func sanitizeName(name string) string {
	clean := strings.Map(func(r rune) rune {
		if r < 32 || strings.ContainsRune(`<>:"/\|?*`, r) {
			return '_'
		}
		return r
	}, name)
	clean = strings.Trim(clean, " .")
	if clean == "" {
		return "job"
	}
	return clean
}
