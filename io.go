package bash

import (
	"context"
	"io"
	"io/fs"
	"strings"
)

// IO connects a shell execution to live streams. Nil stdin means EOF; nil
// stdout/stderr discard output. The caller owns the streams and must arrange
// for blocked reads and writes to unblock when the execution context ends,
// either through Cancel or through its own cancellation mechanism.
// Streams are borrowed until the call returns and are never closed by the shell.
type IO struct {
	Stdin          io.Reader
	Stdout, Stderr io.Writer
	// Cancel optionally unblocks pending I/O on cancellation, timeout, or an
	// output-limit failure. It may run concurrently with reads and writes,
	// is called at most once, and finishes before RunIO/FinishIO returns.
	// It is not called after a successful execution.
	Cancel func()
}

// RunIO executes a script with live I/O, for consoles and interactive Go commands.
// Result contains only status; output goes directly to the supplied writers.
// Redirections, pipelines, execution deadlines, and MaxOutputBytes still apply.
// Command streams forward Fd() when the underlying stream provides it; otherwise
// they return ^uintptr(0). A Go TUI can use this to manage the caller's terminal.
// This does not allocate a PTY or add terminal support to WASM executables.
func (s *Shell) RunIO(ctx context.Context, script string, streams IO) (Result, error) {
	return s.runIO(ctx, script, streams, false)
}

// FinishIO is Finish with live streams, including output from the EXIT trap.
func (s *Shell) FinishIO(ctx context.Context, streams IO) (Result, error) {
	return s.runIO(ctx, "", streams, true)
}

func (s *Shell) runIO(ctx context.Context, script string, streams IO, finish bool) (Result, error) {
	ctx, cancel := context.WithTimeout(ctx, s.opts.Timeout)
	defer cancel()
	if err := s.lock(ctx); err != nil {
		return Result{}, err
	}
	defer s.unlock()
	if s.closed {
		return Result{}, fs.ErrClosed
	}
	inSet := streams.Stdin != nil
	if streams.Stdin == nil {
		streams.Stdin = strings.NewReader("")
	}
	if streams.Stdout == nil {
		streams.Stdout = io.Discard
	}
	if streams.Stderr == nil {
		streams.Stderr = io.Discard
	}
	if streams.Cancel != nil {
		done := make(chan struct{})
		stop := context.AfterFunc(ctx, func() { streams.Cancel(); close(done) })
		defer func() {
			if !stop() {
				<-done
			}
		}()
	}
	return s.runWithIO(ctx, cancel, script, streams, inSet, finish)
}

func (w captureWriter) Fd() uintptr {
	out := w.c.stdout
	if w.stderr {
		out = w.c.stderr
	}
	if f, ok := out.(interface{ Fd() uintptr }); ok {
		return f.Fd()
	}
	return ^uintptr(0)
}
