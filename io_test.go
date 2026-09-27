package bash_test

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	bash "github.com/adrianliechti/go-bash"
)

func TestRunIOInteractive(t *testing.T) {
	b := newShell(t, bash.Options{Commands: map[string]bash.CommandFunc{
		"conversation": func(ctx context.Context, cmd *bash.Command) (int, error) {
			if _, err := io.WriteString(cmd.Stdout, "ready\n"); err != nil {
				return 1, err
			}
			var answer [3]byte
			if _, err := io.ReadFull(cmd.Stdin, answer[:]); err != nil {
				return 1, err
			}
			_, err := fmt.Fprintf(cmd.Stdout, "answer:%s\n", answer)
			return 7, err
		},
	}})
	input, inputWriter := io.Pipe()
	output, outputWriter := io.Pipe()
	defer input.Close()
	defer inputWriter.Close()
	defer output.Close()
	defer outputWriter.Close()
	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()
	stop := context.AfterFunc(ctx, func() { input.CloseWithError(ctx.Err()); output.CloseWithError(ctx.Err()) })
	defer stop()
	type execution struct {
		result bash.Result
		err    error
	}
	done := make(chan execution, 1)
	go func() {
		r, err := b.RunIO(ctx, "conversation", bash.IO{Stdin: input, Stdout: outputWriter})
		outputWriter.Close()
		done <- execution{r, err}
	}()
	var ready [6]byte
	if _, err := io.ReadFull(output, ready[:]); err != nil || string(ready[:]) != "ready\n" {
		t.Fatalf("output before input: %q, %v", ready, err)
	}
	if _, err := io.WriteString(inputWriter, "yes"); err != nil {
		t.Fatal(err)
	}
	rest, err := io.ReadAll(output)
	if err != nil || string(rest) != "answer:yes\n" {
		t.Fatalf("output %q, %v", rest, err)
	}
	got := <-done
	if got.err != nil || got.result != (bash.Result{ExitCode: 7}) {
		t.Fatalf("result: %+v", got)
	}
}

func TestRunIOStreamsAndFinish(t *testing.T) {
	b := newShell(t, bash.Options{})
	var out, stderr bytes.Buffer
	streams := bash.IO{Stdin: strings.NewReader("Ada\n"), Stdout: &out, Stderr: &stderr}
	r, err := b.RunIO(t.Context(), `read name; printf 'hello %s\n' "$name" | cat; echo error >&2; exec 3>&1; exec > /work/log; echo file; trap 'echo cleanup >&3' EXIT`, streams)
	if err != nil || r.ExitCode != 0 || r.Stdout != "" || r.Stderr != "" || out.String() != "hello Ada\n" || stderr.String() != "error\n" {
		t.Fatalf("run: %+v %v, output %q / %q", r, err, &out, &stderr)
	}
	var next bytes.Buffer
	r, err = b.FinishIO(t.Context(), bash.IO{Stdout: &next})
	if err != nil || r.ExitCode != 0 || next.String() != "cleanup\n" {
		t.Fatalf("finish: %+v %v %q", r, err, &next)
	}
	data, err := b.ReadFile(t.Context(), "/work/log")
	if err != nil || string(data) != "file\n" {
		t.Fatalf("file: %q %v", data, err)
	}
	if _, err := b.FinishIO(t.Context(), bash.IO{Stdout: &next}); err != nil || next.String() != "cleanup\n" {
		t.Fatalf("repeated finish: %q %v", &next, err)
	}
	if err := b.Close(t.Context()); err != nil {
		t.Fatal(err)
	}
	if _, err := b.RunIO(t.Context(), "true", bash.IO{}); !errors.Is(err, fs.ErrClosed) {
		t.Fatalf("closed: %v", err)
	}
}

type fdReader struct {
	io.Reader
	fd uintptr
}

func (f fdReader) Fd() uintptr { return f.fd }

type fdWriter struct {
	io.Writer
	fd uintptr
}

func (f fdWriter) Fd() uintptr { return f.fd }

func TestRunIOTerminalDescriptors(t *testing.T) {
	fd := func(v any) uintptr {
		if f, ok := v.(interface{ Fd() uintptr }); ok {
			return f.Fd()
		}
		return ^uintptr(0)
	}
	b := newShell(t, bash.Options{Commands: map[string]bash.CommandFunc{
		"fds": func(ctx context.Context, cmd *bash.Command) (int, error) {
			fmt.Fprintf(cmd.Stderr, "%d,%d;", fd(cmd.Stdin), fd(cmd.Stdout))
			return 0, nil
		},
	}})
	var out, stderr bytes.Buffer
	streams := bash.IO{Stdin: fdReader{strings.NewReader(""), 41}, Stdout: fdWriter{&out, 42}, Stderr: &stderr}
	for _, tc := range []struct{ script, want string }{
		{"fds", "41,42;"},
		{"fds < /dev/null", fmt.Sprintf("%d,42;", ^uintptr(0))},
		{"fds > /work/file", fmt.Sprintf("41,%d;", ^uintptr(0))},
		{"true | fds", fmt.Sprintf("%d,42;", ^uintptr(0))},
		{"fds | cat", fmt.Sprintf("41,%d;", ^uintptr(0))},
		{"value=$(fds)", fmt.Sprintf("41,%d;", ^uintptr(0))},
		{"exec 3>&1; fds >&3", "41,42;"},
	} {
		stderr.Reset()
		r, err := b.RunIO(t.Context(), tc.script, streams)
		if err != nil || r.ExitCode != 0 || stderr.String() != tc.want {
			t.Errorf("%s: %+v %v %q; want %q", tc.script, r, err, &stderr, tc.want)
		}
	}
	// Captured execution never grants access to a previous call's terminal.
	r, err := b.Exec(t.Context(), "fds")
	want := fmt.Sprintf("%d,%d;", ^uintptr(0), ^uintptr(0))
	if err != nil || r.Stderr != want {
		t.Fatalf("capture: %+v %v; want %q", r, err, want)
	}
}

func TestRunIOLimitsAndCancellation(t *testing.T) {
	b := newShell(t, bash.Options{MaxOutputBytes: 8, Commands: map[string]bash.CommandFunc{
		"untilcancel": func(ctx context.Context, _ *bash.Command) (int, error) { <-ctx.Done(); return 1, ctx.Err() },
	}})
	var out, stderr bytes.Buffer
	r, err := b.RunIO(t.Context(), "printf 12345; printf 67890 >&2", bash.IO{Stdout: &out, Stderr: &stderr})
	if !errors.Is(err, bash.ErrOutputLimit) || r.ExitCode == 0 || out.String()+stderr.String() != "12345678" {
		t.Fatalf("limit: %+v %v %q %q", r, err, &out, &stderr)
	}
	ctx, cancel := context.WithTimeout(t.Context(), 20*time.Millisecond)
	defer cancel()
	if _, err := b.RunIO(ctx, "untilcancel", bash.IO{}); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("cancel: %v", err)
	}
	if r, err := b.RunIO(t.Context(), "true", bash.IO{}); err != nil || r.ExitCode != 0 {
		t.Fatalf("reuse: %+v %v", r, err)
	}
}

func TestRunIOCancelUnblocksInput(t *testing.T) {
	b := newShell(t, bash.Options{MaxOutputBytes: 8, Timeout: 100 * time.Millisecond, Commands: map[string]bash.CommandFunc{
		"receive": func(_ context.Context, cmd *bash.Command) (int, error) {
			_, err := io.Copy(io.Discard, cmd.Stdin)
			return 1, err
		},
		"flood": func(_ context.Context, cmd *bash.Command) (int, error) {
			_, err := io.WriteString(cmd.Stdout, "output exceeds limit")
			return 1, err
		},
	}})
	for _, tc := range []struct {
		script string
		want   error
	}{
		{"receive | flood", bash.ErrOutputLimit},
		{"receive", context.DeadlineExceeded},
	} {
		t.Run(tc.script, func(t *testing.T) {
			input, writer := io.Pipe()
			defer input.Close()
			defer writer.Close()
			var calls atomic.Int32
			streams := bash.IO{Stdin: input, Cancel: func() {
				calls.Add(1)
				input.CloseWithError(context.Canceled)
			}}
			if _, err := b.RunIO(t.Context(), "true", streams); err != nil || calls.Load() != 0 {
				t.Fatalf("successful execution cancelled streams: %v, %d calls", err, calls.Load())
			}
			ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
			defer cancel()
			stop := context.AfterFunc(ctx, func() { input.CloseWithError(ctx.Err()) })
			defer stop()
			r, err := b.RunIO(ctx, tc.script, streams)
			if !errors.Is(err, tc.want) || r.ExitCode == 0 || calls.Load() != 1 || ctx.Err() != nil {
				t.Fatalf("did not unblock stdin promptly: %+v %v; %d callbacks; context %v", r, err, calls.Load(), ctx.Err())
			}
		})
	}
}
