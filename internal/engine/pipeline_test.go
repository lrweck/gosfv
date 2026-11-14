package engine

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/lrweck/gosfv/internal/hasher"
)

func TestPipelinedDigests(t *testing.T) {
	old := pipelineMinSize
	pipelineMinSize = 1
	defer func() { pipelineMinSize = old }()

	sizes := []int{0, 1, 64<<10 - 1, 64 << 10, 64<<10 + 1, 1<<20 + 123}
	for _, kind := range []hasher.Kind{hasher.MD5, hasher.SHA1, hasher.SHA256} {
		for _, size := range sizes {
			data := bytes.Repeat([]byte("0123456789abcdef"), size/16+1)[:size]
			path := filepath.Join(t.TempDir(), "a.bin")
			if err := os.WriteFile(path, data, 0o644); err != nil {
				t.Fatal(err)
			}

			ref := kind.New()
			_, _ = ref.Write(data)
			want := ref.Sum(nil)

			e := New([]Task{{Index: 0, Path: path, Size: int64(size)}}, Options{
				Kind:       kind,
				Pipeline:   true,
				BufferSize: 64 << 10,
			})
			e.Run(context.Background())
			r := e.Result(0)
			if r.Err != nil {
				t.Fatalf("%s %d bytes: %v", kind, size, r.Err)
			}
			if r.Bytes != int64(size) {
				t.Errorf("%s %d bytes: read %d", kind, size, r.Bytes)
			}
			if got := e.Hash(0); !bytes.Equal(got, want) {
				t.Errorf("%s %d bytes: digest mismatch", kind, size)
			}
		}
	}
}

func TestPipelinedDisabledUsesSerialPath(t *testing.T) {
	old := pipelineMinSize
	pipelineMinSize = 1
	defer func() { pipelineMinSize = old }()

	data := bytes.Repeat([]byte("x"), 1<<20)
	path := filepath.Join(t.TempDir(), "a.bin")
	if err := os.WriteFile(path, data, 0o644); err != nil {
		t.Fatal(err)
	}
	e := New([]Task{{Index: 0, Path: path, Size: int64(len(data))}}, Options{
		Kind:       hasher.SHA256,
		BufferSize: 64 << 10,
	})
	e.Run(context.Background())
	if err := e.Result(0).Err; err != nil {
		t.Fatal(err)
	}
	ref := hasher.SHA256.New()
	_, _ = ref.Write(data)
	if !bytes.Equal(e.Hash(0), ref.Sum(nil)) {
		t.Error("serial digest mismatch")
	}
}

func TestPipelinedCancel(t *testing.T) {
	old := pipelineMinSize
	pipelineMinSize = 1
	defer func() { pipelineMinSize = old }()

	path := filepath.Join(t.TempDir(), "a.bin")
	if err := os.WriteFile(path, make([]byte, 4<<20), 0o644); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	e := New([]Task{{Index: 0, Path: path, Size: 4 << 20}}, Options{
		Kind:       hasher.SHA256,
		Pipeline:   true,
		BufferSize: 64 << 10,
	})
	e.Run(ctx)
	if e.Done(0) {
		// A task that was never started stays unfinished; one that the
		// reader saw canceled returns an error.
		if e.Result(0).Err == nil {
			t.Error("canceled task finished without an error")
		}
	}
}
