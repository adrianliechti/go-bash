package shell

import (
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"

	"github.com/adrianliechti/go-bash/internal/fsys"
	"mvdan.cc/sh/v3/expand"
	"mvdan.cc/sh/v3/syntax"
)

// Descriptors are virtual handles. Duplication shares the open file's offset;
// the last reference closes it, including references held by child shells.
type descriptor struct {
	in           io.Reader
	out          io.Writer
	closer       io.Closer
	refs         atomic.Int32
	defaultInput *IO
}

func newDescriptor(in io.Reader, out io.Writer, closer io.Closer) *descriptor {
	d := &descriptor{in: in, out: out, closer: closer}
	if in != nil {
		d.in = &descriptorReader{in: in}
	}
	d.refs.Store(1)
	return d
}

// Duplicated descriptors can be read by two pipeline stages. In particular,
// strings.Reader (stdin and heredocs) needs serialization of its shared offset.
type descriptorReader struct {
	mu sync.Mutex
	in io.Reader
}

func (r *descriptorReader) Read(p []byte) (int, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.in.Read(p)
}
func (d *descriptor) retain() *descriptor {
	if d != nil {
		d.refs.Add(1)
	}
	return d
}
func (d *descriptor) release() error {
	if d != nil && d.refs.Add(-1) == 0 && d.closer != nil {
		return d.closer.Close()
	}
	return nil
}

// The session's original stdio follows each Run's capture. Saved duplicates
// (exec 3>&1) must not keep writing into an earlier Result's output buffer.
type sessionInput struct{ io *IO }

func (r sessionInput) Read(p []byte) (int, error) { return r.io.In.Read(p) }

type sessionOutput struct {
	io     *IO
	stderr bool
}

func (w sessionOutput) Write(p []byte) (int, error) {
	if w.stderr {
		return w.io.Err.Write(p)
	}
	return w.io.Out.Write(p)
}

type closedStream struct{}

func (closedStream) Read([]byte) (int, error)  { return 0, fs.ErrClosed }
func (closedStream) Write([]byte) (int, error) { return 0, fs.ErrClosed }

func (s *Shell) initDescriptors() {
	s.baseIO = &IO{In: strings.NewReader(""), Out: io.Discard, Err: io.Discard}
	s.fds[0] = newDescriptor(sessionInput{s.baseIO}, nil, nil)
	s.fds[0].defaultInput = s.baseIO
	s.fds[1] = newDescriptor(nil, sessionOutput{s.baseIO, false}, nil)
	s.fds[2] = newDescriptor(nil, sessionOutput{s.baseIO, true}, nil)
}
func (s *Shell) streams() IO {
	x := IO{In: closedStream{}, Out: closedStream{}, Err: closedStream{}, InSet: true, Umask: s.umask}
	if d := s.fds[0]; d != nil && d.in != nil {
		x.In = d.in
		if d.defaultInput != nil {
			x.InSet = d.defaultInput.InSet
		}
	}
	if d := s.fds[1]; d != nil && d.out != nil {
		x.Out = d.out
	}
	if d := s.fds[2]; d != nil && d.out != nil {
		x.Err = d.out
	}
	return x
}
func (s *Shell) inheritDescriptors(parent *Shell) {
	_ = s.Close()
	for i, d := range parent.fds {
		s.fds[i] = d.retain()
	}
}

// Close releases virtual descriptors, never the caller's mounted filesystems
// or borrowed input and output streams.
func (s *Shell) Close() error {
	var err error
	for i, d := range s.fds {
		err = errors.Join(err, d.release())
		s.fds[i] = nil
	}
	return err
}

func (s *Shell) closeResult(code *int, err *error) {
	if closeErr := s.Close(); closeErr != nil {
		*code = 1
		var control flow
		if *err == nil || errors.As(*err, &control) {
			*err = closeErr
		} else {
			*err = errors.Join(*err, closeErr)
		}
	}
}

type redirectChange struct {
	shell     *Shell
	fd        int
	saved     *descriptor
	committed bool
}

func (s *Shell) changeDescriptor(fd int, d *descriptor) *redirectChange {
	c := &redirectChange{shell: s, fd: fd, saved: s.fds[fd]}
	s.fds[fd] = d
	return c
}
func (c *redirectChange) Close() error {
	if c.committed {
		return nil
	}
	err := c.shell.fds[c.fd].release()
	c.shell.fds[c.fd] = c.saved
	c.saved = nil
	c.committed = true
	return err
}

type redirectFrame struct{ changes []io.Closer }

func (f *redirectFrame) commit() error {
	var err error
	for _, closer := range f.changes {
		c := closer.(*redirectChange)
		err = errors.Join(err, c.saved.release())
		c.saved, c.committed = nil, true
	}
	return err
}

func (s *Shell) redirect(r *run, redirs []*syntax.Redirect, streams IO) (IO, []io.Closer, error) {
	var changes []io.Closer
	set := func(fd int, d *descriptor) {
		changes = append(changes, s.changeDescriptor(fd, d))
		streams = s.streams()
	}
	for _, rd := range redirs {
		fd := 1
		switch rd.Op {
		case syntax.RdrIn, syntax.RdrInOut, syntax.Hdoc, syntax.DashHdoc, syntax.WordHdoc, syntax.DplIn:
			fd = 0
		}
		if rd.N != nil {
			fd, _ = strconv.Atoi(rd.N.Value)
		}
		if rd.Op == syntax.Hdoc || rd.Op == syntax.DashHdoc || rd.Op == syntax.WordHdoc {
			var body string
			var err error
			if rd.Op == syntax.WordHdoc {
				body, err = s.literal(r, rd.Word, streams)
				body += "\n"
			} else {
				word := rd.Hdoc
				if rd.Op == syntax.DashHdoc {
					word = stripHeredocTabs(word)
				}
				body, err = expand.Document(s.config(r, streams), word)
			}
			if err != nil {
				return streams, changes, err
			}
			if len(body) > maxExpansion {
				return streams, changes, ErrLimit
			}
			set(fd, newDescriptor(strings.NewReader(body), nil, nil))
			continue
		}
		words, err := s.fields(r, []*syntax.Word{rd.Word}, streams)
		if err != nil {
			return streams, changes, err
		}
		if len(words) != 1 {
			return streams, changes, errors.New("ambiguous redirect")
		}
		name := words[0]
		if rd.Op == syntax.DplOut || rd.Op == syntax.DplIn {
			if name == "-" {
				set(fd, nil)
				continue
			}
			move := strings.HasSuffix(name, "-")
			src, err := strconv.Atoi(strings.TrimSuffix(name, "-"))
			if err != nil || src < 0 || src >= len(s.fds) {
				return streams, changes, fmt.Errorf("%s: bad file descriptor", name)
			}
			d := s.fds[src]
			if d == nil || (rd.Op == syntax.DplOut && d.out == nil) || (rd.Op == syntax.DplIn && d.in == nil) {
				return streams, changes, fmt.Errorf("%s: bad file descriptor", name)
			}
			set(fd, d.retain())
			if move && src != fd {
				// Bash closes a moved source even after temporary redirections unwind.
				if err := s.fds[src].release(); err != nil {
					return streams, changes, err
				}
				s.fds[src] = nil
				streams = s.streams()
			}
			continue
		}
		flag := os.O_RDONLY
		switch rd.Op {
		case syntax.RdrIn:
		case syntax.RdrInOut:
			flag = os.O_RDWR | os.O_CREATE
		case syntax.RdrOut, syntax.ClbOut, syntax.RdrAll:
			flag = os.O_WRONLY | os.O_CREATE | os.O_TRUNC
		case syntax.AppOut, syntax.AppAll:
			flag = os.O_WRONLY | os.O_CREATE | os.O_APPEND
		default:
			return streams, changes, fmt.Errorf("unsupported redirect: %s", rd.Op)
		}
		file, err := s.FS.Open(fsys.Resolve(s.Cwd, name), flag, 0666&^fs.FileMode(s.umask))
		if err != nil {
			return streams, changes, err
		}
		d := newDescriptor(nil, nil, file)
		if flag&os.O_WRONLY == 0 {
			d.in = &descriptorReader{in: file}
		}
		if flag&(os.O_WRONLY|os.O_RDWR) != 0 {
			var ok bool
			d.out, ok = file.(io.Writer)
			if !ok {
				_ = d.release()
				return streams, changes, fs.ErrPermission
			}
		}
		if rd.Op == syntax.RdrAll || rd.Op == syntax.AppAll {
			set(1, d)
			set(2, d.retain())
		} else {
			set(fd, d)
		}
	}
	return streams, changes, nil
}

func validateRedirect(rd *syntax.Redirect) error {
	if rd.N != nil {
		fd, err := strconv.Atoi(rd.N.Value)
		if err != nil || fd < 0 || fd > 9 {
			return errors.New("only descriptors 0 through 9 are supported")
		}
	}
	switch rd.Op {
	case syntax.RdrIn, syntax.RdrInOut, syntax.RdrOut, syntax.AppOut, syntax.ClbOut, syntax.DplOut, syntax.DplIn, syntax.Hdoc, syntax.DashHdoc, syntax.WordHdoc:
		return nil
	case syntax.RdrAll, syntax.AppAll:
		if rd.N == nil {
			return nil
		}
	}
	return fmt.Errorf("unsupported redirection: %s", rd.Op)
}
