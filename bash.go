// Package bash embeds uutils coreutils, grep, find, diff, sed, and ripgrep in a
// virtual shell with Go filesystem mounts and custom Go commands.
package bash

import (
	"bytes"
	"context"
	"crypto/rand"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"maps"
	"math"
	"path"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/adrianliechti/go-bash/internal/fsys"
	"github.com/adrianliechti/go-bash/internal/shell"
	"github.com/adrianliechti/go-bash/internal/wasm"
	"github.com/tetratelabs/wazero"
	"github.com/tetratelabs/wazero/experimental/sysfs"
	"github.com/tetratelabs/wazero/imports/wasi_snapshot_preview1"
	"github.com/tetratelabs/wazero/sys"
)

// Mount attaches a filesystem at an absolute guest path. Filesystems implementing
// vfs.WriteFS are writable unless ReadOnly is set. Plain fs.FS mounts are read-only.
// The caller owns mounted filesystems and must keep them open until Close returns.
type Mount struct {
	Path     string
	FS       fs.FS
	ReadOnly bool
}

type Options struct {
	Mounts []Mount
	Cwd    string
	Env    map[string]string
	// Commands registers Go handlers in /bin and /usr/bin. Names cannot replace
	// embedded commands or shell builtins. New copies the map.
	Commands map[string]CommandFunc
	// Zero values use the defaults below. Negative limits are invalid.
	Timeout          time.Duration // default 10 seconds per execution
	MaxOutputBytes   int           // default 1 MiB across stdout and stderr
	MaxFileBytes     int64         // default 64 MiB across the default memory filesystem
	MaxSteps         int64         // default 10000 shell execution steps
	MemoryLimitPages uint32        // default 2048 (128 MiB) per WASM command
}

type Result struct {
	Stdout, Stderr string
	ExitCode       int
	Exited         bool // script terminated the shell; an interactive caller should end its loop
}
type Request struct{ Script, Stdin string }

// Shell is a persistent shell session. Files, variables, functions, and cwd are
// retained between executions. Calls are serialized; pipeline stages run concurrently.
type Shell struct {
	gate     chan struct{}
	runtime  wazero.Runtime
	modules  map[string]*module
	fs       *fsys.Namespace
	shell    *shell.Shell
	opts     Options
	closed   bool
	commands map[string]CommandFunc
}

// module is an embedded executable compiled on first use, so that a session
// which never runs grep or sed does not pay for compiling them.
type module struct {
	def      *wasm.Module
	mu       sync.Mutex
	compiled wazero.CompiledModule
}

func (m *module) compile(ctx context.Context, rt wazero.Runtime) (wazero.CompiledModule, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.compiled == nil {
		c, e := rt.CompileModule(ctx, m.def.Wasm)
		if e != nil {
			return nil, fmt.Errorf("compile %s: %w", m.def.Name, e)
		}
		m.compiled = c
	}
	return m.compiled, nil
}

const CoreutilsVersion = wasm.Version

var ErrOutputLimit = errors.New("output limit exceeded")

// ErrExecutionLimit reports a shell step, recursion, script, or expansion limit.
var ErrExecutionLimit = shell.ErrLimit

func New(ctx context.Context, opts Options) (*Shell, error) {
	if opts.Timeout < 0 || opts.MaxOutputBytes < 0 || opts.MaxFileBytes < 0 || opts.MaxSteps < 0 {
		return nil, errors.New("limits cannot be negative")
	}
	if opts.Timeout == 0 {
		opts.Timeout = 10 * time.Second
	}
	if opts.MaxOutputBytes == 0 {
		opts.MaxOutputBytes = 1 << 20
	}
	if opts.MaxFileBytes == 0 {
		opts.MaxFileBytes = 64 << 20
	}
	if opts.MaxSteps == 0 {
		opts.MaxSteps = 10000
	}
	if opts.MemoryLimitPages == 0 {
		opts.MemoryLimitPages = 2048
	}
	if opts.MemoryLimitPages > 65536 {
		return nil, errors.New("WASM memory exceeds 4 GiB")
	}
	if opts.Cwd == "" {
		opts.Cwd = "/work"
	}
	if !strings.HasPrefix(opts.Cwd, "/") {
		return nil, errors.New("cwd must be absolute")
	}
	opts.Cwd = path.Clean(fsys.Resolve("/", opts.Cwd))
	for k, v := range opts.Env {
		if k == "" || strings.ContainsAny(k, "=\x00") || strings.ContainsRune(v, 0) {
			return nil, errors.New("invalid environment entry")
		}
	}
	handlers := maps.Clone(opts.Commands)
	var names []string
	for name, handler := range handlers {
		if !fs.ValidPath(name) || name == "." || strings.ContainsAny(name, "/\\\x00") || handler == nil {
			return nil, fmt.Errorf("invalid custom command: %q", name)
		}
		if wasm.Lookup(name) != nil || shell.IsBuiltin(name) {
			return nil, fmt.Errorf("command already registered: %q", name)
		}
		names = append(names, name)
	}
	commands := wasm.FS(names...)
	mounts := []fsys.Mount{{Path: "/bin", FS: commands, ReadOnly: true}, {Path: "/usr/bin", FS: commands, ReadOnly: true}}
	for _, m := range opts.Mounts {
		mounts = append(mounts, fsys.Mount{Path: m.Path, FS: m.FS, ReadOnly: m.ReadOnly})
	}
	n, e := fsys.New(mounts, opts.MaxFileBytes)
	if e != nil {
		return nil, e
	}
	i, e := n.Stat(opts.Cwd)
	if e != nil {
		return nil, e
	}
	if !i.IsDir() {
		return nil, errors.New("cwd is not a directory")
	}
	wr := wazero.NewRuntimeWithConfig(ctx, wazero.NewRuntimeConfig().WithMemoryLimitPages(opts.MemoryLimitPages).WithCloseOnContextDone(true))
	if _, e = wasi_snapshot_preview1.Instantiate(ctx, wr); e != nil {
		wr.Close(ctx)
		return nil, e
	}
	s := &Shell{gate: make(chan struct{}, 1), runtime: wr, modules: make(map[string]*module), fs: n, opts: opts, commands: handlers}
	for _, def := range wasm.Modules {
		s.modules[def.Name] = &module{def: def}
	}
	// coreutils backs nearly every script, so compile it up front and surface
	// a broken artifact at construction instead of on first use.
	if _, e = s.modules["coreutils"].compile(ctx, wr); e != nil {
		wr.Close(ctx)
		return nil, e
	}
	s.shell = shell.New(n, opts.Cwd, opts.Env, s.command)
	return s, nil
}

func (s *Shell) lock(ctx context.Context) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	select {
	case s.gate <- struct{}{}:
		if err := ctx.Err(); err != nil {
			s.unlock()
			return err
		}
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}
func (s *Shell) unlock() { <-s.gate }

// Close runs pending EXIT cleanup and releases the session. Use Finish first
// when the cleanup output or exit status is needed. Close discards that output.
func (s *Shell) Close(ctx context.Context) error {
	ctx, cancel := context.WithTimeout(ctx, s.opts.Timeout)
	defer cancel()
	if e := s.lock(ctx); e != nil {
		return e
	}
	defer s.unlock()
	if s.closed {
		return nil
	}
	_, trapErr := s.runLocked(ctx, cancel, Request{}, true)
	s.closed = true
	return errors.Join(trapErr, s.shell.Close(), s.runtime.Close(ctx))
}
func (s *Shell) Exec(ctx context.Context, script string) (Result, error) {
	return s.Run(ctx, Request{Script: script})
}
func (s *Shell) Run(ctx context.Context, req Request) (Result, error) {
	return s.run(ctx, req, false)
}

// Finish supplies EOF to the shell, running its pending EXIT trap once. It
// returns the final output and status, and leaves file access available until
// Close. Calling Exec or Run again starts another batch in the same session.
func (s *Shell) Finish(ctx context.Context) (Result, error) {
	return s.run(ctx, Request{}, true)
}

func (s *Shell) run(ctx context.Context, req Request, finish bool) (Result, error) {
	ctx, cancel := context.WithTimeout(ctx, s.opts.Timeout)
	defer cancel()
	if e := s.lock(ctx); e != nil {
		return Result{}, e
	}
	defer s.unlock()
	if s.closed {
		return Result{}, fs.ErrClosed
	}
	if len(req.Stdin) > shell.MaxScript {
		return Result{}, shell.ErrLimit
	}
	return s.runLocked(ctx, cancel, req, finish)
}

func (s *Shell) runLocked(ctx context.Context, cancel context.CancelFunc, req Request, finish bool) (Result, error) {
	output := &capture{remaining: s.opts.MaxOutputBytes, cancel: cancel}
	streams := shell.IO{In: strings.NewReader(req.Stdin), Out: captureWriter{output, false}, Err: captureWriter{output, true}, InSet: req.Stdin != ""}
	var code int
	var e error
	if finish {
		code, e = s.shell.Finish(ctx, streams, s.opts.MaxSteps)
	} else {
		code, e = s.shell.Run(ctx, req.Script, streams, s.opts.MaxSteps)
	}
	if output.exceeded {
		e = ErrOutputLimit
		code = 1
	} else if ctx.Err() != nil {
		e = ctx.Err()
		code = 1
	}
	return Result{Stdout: output.out.String(), Stderr: output.err.String(), ExitCode: code, Exited: s.shell.Exited}, e
}

// ReadFile reads a guest file using an absolute path. Relative names are resolved
// against the shell's current working directory. The read is bounded by MaxFileBytes.
func (s *Shell) ReadFile(ctx context.Context, name string) ([]byte, error) {
	if e := s.lock(ctx); e != nil {
		return nil, e
	}
	defer s.unlock()
	if s.closed {
		return nil, fs.ErrClosed
	}
	f, e := s.fs.Open(fsys.Resolve(s.shell.Cwd, name), 0, 0)
	if e != nil {
		return nil, e
	}
	defer f.Close()
	limit := s.opts.MaxFileBytes
	if limit < math.MaxInt64 {
		limit++
	}
	b, e := io.ReadAll(io.LimitReader(f, limit))
	if int64(len(b)) > s.opts.MaxFileBytes {
		return nil, shell.ErrLimit
	}
	return b, e
}

// Commands lists the embedded commands: the coreutils enabled by the
// pinned WASI build plus grep, find, diff, cmp, sed, and rg. The shell also
// implements builtins such as cd, export, xargs, and exit. Listing is not a
// promise that every option is supported by WASI or by every mounted filesystem.
func Commands() []string { return wasm.Commands() }

// Versions maps each embedded project to its pinned release.
func Versions() map[string]string {
	v := make(map[string]string)
	for _, m := range wasm.Modules {
		v[m.Project] = m.Version
	}
	return v
}

func (s *Shell) command(ctx context.Context, cwd string, args []string, env map[string]string, streams shell.IO) (int, error) {
	if handler := s.commands[args[0]]; handler != nil {
		return handler(ctx, &Command{
			Args: args, Cwd: cwd, Env: env, FS: maskedCommandFS{commandFS{s.fs}, fs.FileMode(streams.Umask)},
			Stdin: streams.In, Stdout: streams.Out, Stderr: streams.Err,
		})
	}
	def := wasm.Lookup(args[0])
	if def == nil {
		fmt.Fprintln(streams.Err, args[0]+": command not found")
		return 127, nil
	}
	compiled, e := s.modules[def.Name].compile(ctx, s.runtime)
	if e != nil {
		return 1, e
	}
	argv := def.Argv(args)
	commandCtx, cancelCommand := context.WithCancel(ctx)
	defer cancelCommand()
	brokenPipe := &commandPipeState{cancel: cancelCommand}
	stdout := commandPipeWriter{streams.Out, brokenPipe}
	stderr := commandPipeWriter{streams.Err, brokenPipe}
	fc := wazero.NewFSConfig().(sysfs.FSConfig).WithSysFSMount(&fsys.WASI{NS: s.fs, Cwd: "/", Umask: fs.FileMode(streams.Umask)}, "/")
	mc := wazero.NewModuleConfig().WithName("").WithArgs(argv...).WithFSConfig(fc).WithStdin(streams.In).WithStdout(stdout).WithStderr(stderr).WithRandSource(rand.Reader).WithSysWalltime().WithSysNanotime().WithNanosleep(func(ns int64) {
		timer := time.NewTimer(time.Duration(ns))
		defer timer.Stop()
		select {
		case <-timer.C:
		case <-commandCtx.Done():
		}
	})
	for k, v := range env {
		mc = mc.WithEnv(k, v)
	}
	mc = mc.WithEnv("BASH_CWD", cwd)
	mc = mc.WithEnv("BASH_STDIN", strconv.FormatBool(streams.InSet))
	mod, e := s.runtime.InstantiateModule(commandCtx, compiled, mc)
	if mod != nil {
		_ = mod.Close(ctx)
	}
	if ctx.Err() != nil {
		return 1, ctx.Err()
	}
	if brokenPipe.closed.Load() {
		return 141, nil // virtual SIGPIPE; do not turn it into a WASI I/O diagnostic
	}
	if e != nil {
		var exit *sys.ExitError
		if errors.As(e, &exit) {
			return int(exit.ExitCode()), nil
		}
		return 1, e
	}
	return 0, nil
}

type commandPipeState struct {
	closed atomic.Bool
	cancel context.CancelFunc
}
type commandPipeWriter struct {
	w     io.Writer
	state *commandPipeState
}

func (w commandPipeWriter) Write(p []byte) (int, error) {
	if w.state.closed.Load() {
		return 0, io.ErrClosedPipe
	}
	n, err := w.w.Write(p)
	if errors.Is(err, io.ErrClosedPipe) {
		w.state.closed.Store(true)
		w.state.cancel()
	}
	return n, err
}

type capture struct {
	mu        sync.Mutex
	out, err  bytes.Buffer
	remaining int
	exceeded  bool
	cancel    context.CancelFunc
}
type captureWriter struct {
	c      *capture
	stderr bool
}

func (w captureWriter) Write(p []byte) (int, error) {
	w.c.mu.Lock()
	defer w.c.mu.Unlock()
	buf := &w.c.out
	if w.stderr {
		buf = &w.c.err
	}
	if len(p) > w.c.remaining {
		n, _ := buf.Write(p[:w.c.remaining])
		w.c.remaining = 0
		w.c.exceeded = true
		w.c.cancel()
		return n, ErrOutputLimit
	}
	n, e := buf.Write(p)
	w.c.remaining -= n
	return n, e
}
