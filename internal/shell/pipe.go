package shell

import (
	"io"
	"sync"
)

// A virtual pipe acknowledges bytes once buffered, like an OS pipe. io.Pipe
// waits for every byte to be consumed, making even a completed short write
// fail when read -d stops partway through it. Storage stays bounded and both
// blocked readers and writers are released by close or context cancellation.
type pipeBuffer struct {
	mu                 sync.Mutex
	readable, writable *sync.Cond
	data               [64 << 10]byte
	start, size        int
	readErr, writeErr  error
}

type pipeReader struct{ p *pipeBuffer }
type pipeWriter struct{ p *pipeBuffer }

func newPipe() (*pipeReader, *pipeWriter) {
	p := new(pipeBuffer)
	p.readable, p.writable = sync.NewCond(&p.mu), sync.NewCond(&p.mu)
	return &pipeReader{p}, &pipeWriter{p}
}

func (r *pipeReader) Read(dst []byte) (int, error) {
	p := r.p
	p.mu.Lock()
	defer p.mu.Unlock()
	for p.size == 0 && p.readErr == nil && p.writeErr == nil && len(dst) > 0 {
		p.readable.Wait()
	}
	if p.readErr != nil {
		return 0, p.readErr
	}
	if p.size == 0 && p.writeErr != nil {
		return 0, p.writeErr
	}
	n := min(len(dst), p.size, len(p.data)-p.start)
	copy(dst, p.data[p.start:p.start+n])
	p.start = (p.start + n) % len(p.data)
	p.size -= n
	p.writable.Signal()
	return n, nil
}

func (w *pipeWriter) Write(src []byte) (int, error) {
	p := w.p
	p.mu.Lock()
	defer p.mu.Unlock()
	n := 0
	for {
		if p.readErr != nil {
			return n, p.readErr
		}
		if p.writeErr != nil {
			return n, io.ErrClosedPipe
		}
		if n == len(src) {
			return n, nil
		}
		if p.size == len(p.data) {
			p.writable.Wait()
			continue
		}
		end := (p.start + p.size) % len(p.data)
		count := min(len(src)-n, len(p.data)-p.size, len(p.data)-end)
		copy(p.data[end:end+count], src[n:n+count])
		n += count
		p.size += count
		p.readable.Signal()
	}
}

func (r *pipeReader) Close() error { return r.CloseWithError(nil) }
func (r *pipeReader) CloseWithError(err error) error {
	if err == nil {
		err = io.ErrClosedPipe
	}
	p := r.p
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.readErr == nil {
		p.readErr = err
	}
	p.readable.Broadcast()
	p.writable.Broadcast()
	return nil
}

func (w *pipeWriter) CloseWithError(err error) error {
	if err == nil {
		err = io.EOF
	}
	p := w.p
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.writeErr == nil {
		p.writeErr = err
	}
	p.readable.Broadcast()
	p.writable.Broadcast()
	return nil
}
