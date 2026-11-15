// Package model holds the data GoSFV keeps about files and jobs.
//
// Files are stored in one slice, and the UI keeps a slice of indices into it.
// Hash bytes live in arenas owned by the caller (the engine's output arena or
// the parser's arena), so a slice of a million files does not create a million
// small heap objects.
package model

import (
	"path/filepath"
	"time"
)

// Status is the state of one file in the list.
type Status uint8

const (
	Queued Status = iota
	Pending
	OK
	Bad
	Missing
	Error
)

func (s Status) String() string {
	switch s {
	case Queued:
		return "WAITING"
	case Pending:
		return "PENDING"
	case OK:
		return "OK"
	case Bad:
		return "BAD"
	case Missing:
		return "MISSING"
	case Error:
		return "ERROR"
	}
	return "?"
}

// IsProblem reports whether the status needs attention in the bad-file tools.
func (s Status) IsProblem() bool { return s == Bad || s == Error }

// File is one entry in the list.
type File struct {
	ID   uint64
	Name string // basename
	Rel  string // path relative to Dir
	Dir  string // base directory
	// Full is the absolute path. The scanner already builds it, so the
	// runner transfers that string instead of joining Dir and Rel again for
	// every file. It may be cleared after a rename or move.
	Full     string
	Size     int64
	Expected []byte // expected digest, nil when creating
	Hash     []byte // calculated digest, a slice of an arena
	Status   Status
	// Err is why the last hash or stat failed, when Status is Error or
	// Missing. It is the operating system's message, so the UI can say
	// that a row was retried and failed again for the same reason instead
	// of showing a bare ERROR.
	Err     string
	Elapsed time.Duration
}

// FullPath returns the absolute path, reconstructing it only when Full was
// cleared by a rename or move.
func (f *File) FullPath() string {
	if f.Full != "" {
		return f.Full
	}
	if f.Dir == "" {
		return f.Rel
	}
	return filepath.Join(f.Dir, f.Rel)
}

// JobStatus is the state of a job-queue entry.
type JobStatus uint8

const (
	JobQueued JobStatus = iota
	JobScanning
	JobInProgress
	JobPaused
	JobDone
	JobError
)

func (s JobStatus) String() string {
	switch s {
	case JobQueued:
		return "QUEUED"
	case JobScanning:
		return "SCANNING"
	case JobInProgress:
		return "IN PROGRESS"
	case JobPaused:
		return "PAUSED"
	case JobDone:
		return "DONE"
	case JobError:
		return "ERROR"
	}
	return "?"
}

// Job is one queued folder or file set in job-queue mode.
type Job struct {
	ID       int
	Name     string
	Root     string
	Inputs   []string
	Progress float64
	Status   JobStatus
	Output   string
	Elapsed  time.Duration
}
