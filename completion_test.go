package bash_test

import (
	"context"
	"errors"
	"io/fs"
	"slices"
	"testing"
	"testing/fstest"

	bash "github.com/adrianliechti/go-bash"
)

func TestCompleteVirtualState(t *testing.T) {
	b := newShell(t, bash.Options{Env: map[string]string{"EXAMPLE_BAD;name": "value"}, Commands: map[string]bash.CommandFunc{
		"examplecmd": func(context.Context, *bash.Command) (int, error) { return 0, nil },
	}, Mounts: []bash.Mount{{Path: "/data", FS: fstest.MapFS{
		"examplefile":   {Data: []byte("file"), Mode: 0644},
		"examplescript": {Data: []byte("#!/bin/sh\n"), Mode: 0755},
		"dir/file":      {Data: []byte("nested")},
		".hidden":       {}, "two words": {}, "café": {},
	}}}})
	r, err := b.Exec(t.Context(), `cd /data; PATH=/data:/usr/bin:/bin; examplefn() { :; }; EXAMPLE_VAR=value; false`)
	if err != nil || r.ExitCode != 1 {
		t.Fatalf("setup: %+v %v", r, err)
	}
	for _, tc := range []struct {
		prefix string
		kind   bash.CompletionKind
		want   []string
	}{
		{"example", bash.CompleteCommands, []string{"examplecmd", "examplefn", "examplescript"}},
		{"cd", bash.CompleteCommands, []string{"cd"}},
		{"./example", bash.CompleteCommands, []string{"./examplescript"}},
		{"example", bash.CompleteFiles, []string{"examplefile", "examplescript"}},
		{"", bash.CompleteDirectories, []string{"dir/"}},
		{"dir/", bash.CompleteFiles, []string{"dir/file"}},
		{"/data/two", bash.CompleteFiles, []string{"/data/two words"}},
		{"caf", bash.CompleteFiles, []string{"café"}},
		{".", bash.CompleteFiles, []string{".hidden"}},
		{"EXAMPLE", bash.CompleteVariables, []string{"EXAMPLE_VAR"}},
		{"/missing/", bash.CompleteFiles, nil},
		{"$(touch /work/nope)", bash.CompleteFiles, nil},
	} {
		got, err := b.Complete(t.Context(), tc.prefix, tc.kind)
		if err != nil || !slices.Equal(got, tc.want) {
			t.Errorf("%q (%d): %q %v; want %q", tc.prefix, tc.kind, got, err, tc.want)
		}
	}
	// Completion must not alter $? or run expansion text.
	r, err = b.Exec(t.Context(), `printf '%s\n' "$?"; test ! -e /work/nope`)
	if err != nil || r.Stdout != "1\n" || r.ExitCode != 0 {
		t.Fatalf("completion side effects: %+v %v", r, err)
	}
	if _, err := b.Exec(t.Context(), "PATH=/missing; exec > /work/log; false"); err != nil {
		t.Fatal(err)
	}
	got, err := b.Complete(t.Context(), "example", bash.CompleteCommands)
	if err != nil || !slices.Equal(got, []string{"examplefn"}) {
		t.Fatalf("changed PATH: %q %v", got, err)
	}
	data, err := b.ReadFile(t.Context(), "/work/log")
	if err != nil || len(data) != 0 {
		t.Fatalf("completion wrote redirected output: %q %v", data, err)
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if _, err := b.Complete(ctx, "", bash.CompleteFiles); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancellation: %v", err)
	}
	if _, err := b.Complete(t.Context(), "", bash.CompletionKind(255)); !errors.Is(err, fs.ErrInvalid) {
		t.Fatalf("invalid kind: %v", err)
	}
	if err := b.Close(t.Context()); err != nil {
		t.Fatal(err)
	}
	if _, err := b.Complete(t.Context(), "", bash.CompleteFiles); !errors.Is(err, fs.ErrClosed) {
		t.Fatalf("closed: %v", err)
	}
}
