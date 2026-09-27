package shell

import (
	"bytes"
	"context"
	"errors"
	"io"
	"testing"
	"time"
)

func TestPipeBufferedWriteAndClose(t *testing.T) {
	r, w := newPipe()
	if n, err := w.Write([]byte("one:two")); n != 7 || err != nil {
		t.Fatalf("buffered write: %d, %v", n, err)
	}
	var prefix [4]byte
	if _, err := io.ReadFull(r, prefix[:]); err != nil || string(prefix[:]) != "one:" {
		t.Fatalf("read: %q, %v", prefix, err)
	}
	_ = r.Close()
	if _, err := w.Write([]byte("more")); !errors.Is(err, io.ErrClosedPipe) {
		t.Fatalf("write after reader closes: %v", err)
	}
}

func TestPipeBackpressureAndCancellation(t *testing.T) {
	r, w := newPipe()
	done := make(chan error, 1)
	go func() {
		_, err := w.Write(bytes.Repeat([]byte("x"), 128<<10))
		done <- err
	}()
	first := make([]byte, 1)
	if _, err := r.Read(first); err != nil {
		t.Fatal(err)
	}
	_ = r.CloseWithError(context.Canceled)
	select {
	case err := <-done:
		if !errors.Is(err, context.Canceled) {
			t.Fatal(err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("writer did not unblock")
	}
	r, w = newPipe()
	go func() { _, err := r.Read(first); done <- err }()
	_ = w.CloseWithError(context.Canceled)
	select {
	case err := <-done:
		if !errors.Is(err, context.Canceled) {
			t.Fatal(err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("reader did not unblock")
	}
}

func TestPipeStreamsBeyondCapacity(t *testing.T) {
	r, w := newPipe()
	want := bytes.Repeat([]byte("streaming-data\n"), 20000)
	done := make(chan error, 1)
	go func() {
		_, err := w.Write(want)
		_ = w.CloseWithError(err)
		done <- err
	}()
	got, err := io.ReadAll(r)
	if err != nil || !bytes.Equal(got, want) {
		t.Fatalf("stream differs: got %d bytes, error %v", len(got), err)
	}
	if err := <-done; err != nil {
		t.Fatal(err)
	}
}
