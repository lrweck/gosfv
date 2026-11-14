package gui

import (
	"context"
	"testing"

	"github.com/lrweck/gosfv/internal/model"
)

func TestJobQueueState(t *testing.T) {
	a := testApp()
	a.jobs = []model.Job{
		{ID: 1, Name: "one", Root: t.TempDir(), Inputs: []string{t.TempDir()}, Status: model.JobQueued},
		{ID: 2, Name: "two", Root: t.TempDir(), Inputs: []string{t.TempDir()}, Status: model.JobQueued},
	}
	if got := a.nextQueuedJob(); got != 1 {
		t.Fatalf("nextQueuedJob = %d", got)
	}
	run, ok := a.prepareJob(1)
	if !ok {
		t.Fatal("prepareJob failed")
	}
	if a.jobs[0].Status != model.JobScanning || a.currentJob != 1 || !a.processing {
		t.Errorf("state after prepare = %+v", a.jobs[0])
	}
	a.cancel()
	a.finishJob(1, run, nil, context.Canceled)
	if a.jobs[0].Status != model.JobError || a.processing {
		t.Errorf("state after cancel = %+v processing=%v", a.jobs[0], a.processing)
	}
	a.removeJob(2)
	if len(a.jobs) != 1 {
		t.Errorf("jobs after remove = %+v", a.jobs)
	}
	a.jobs[0].Status = model.JobDone
	if !a.hasCompletedJobs() {
		t.Error("hasCompletedJobs false")
	}
	a.clearCompletedJobs()
	if len(a.jobs) != 0 {
		t.Errorf("jobs after clear = %+v", a.jobs)
	}
}
