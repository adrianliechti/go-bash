// Package shell owns execution of the supported shell language. It uses mvdan's
// parser and word expansion library, never its host-executing interpreter.
package shell

import (
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"maps"
	"path"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"

	"github.com/adrianliechti/go-bash/internal/fsys"
	"mvdan.cc/sh/v3/expand"
	"mvdan.cc/sh/v3/syntax"
)

const MaxScript = 1 << 20
const maxExpansion = 1 << 20

var ErrLimit = errors.New("shell execution limit exceeded")

type IO struct {
	In       io.Reader
	Out, Err io.Writer
	// InSet distinguishes a pipe or redirect from the default empty stdin.
	InSet bool
	// Umask is per virtual process and never changes the host process mask.
	Umask uint32
}
type ExecFunc func(context.Context, string, []string, map[string]string, IO) (int, error)
type Shell struct {
	FS             *fsys.Namespace
	Exec           ExecFunc
	Cwd            string
	Exited         bool
	vars           variables
	funcs          map[string]*syntax.Stmt
	status         int
	subStatus      int
	args           []string
	argsGeneration uint64
	depth          int
	pipefail       bool
	errexit        bool
	nounset        bool
	xtrace         bool
	ignoreErrexit  int
	traceDepth     int
	sourceDepth    int
	locals         []variables // values to restore when each function returns
	umask          uint32
	optIndex       int
	optOffset      int
	optWord        string
	baseIO         *IO
	fds            [10]*descriptor
	redirectFrame  *redirectFrame
	exitTrap       string
	trapSet        bool
	trapInherited  bool
	subshell       bool
}
type run struct {
	ctx      context.Context
	steps    atomic.Int64
	maxSteps int64
}

// IsBuiltin identifies names reserved by the shell, including declarations
// handled by the parser instead of invoke.
func IsBuiltin(name string) bool {
	for builtin := range strings.FieldsSeq(builtinNames) {
		if name == builtin {
			return true
		}
	}
	return false
}

func New(f *fsys.Namespace, cwd string, env map[string]string, exec ExecFunc) *Shell {
	s := &Shell{FS: f, Exec: exec, Cwd: cwd, vars: make(variables), funcs: make(map[string]*syntax.Stmt), umask: 0022}
	for k, v := range map[string]string{"HOME": "/work", "PATH": "/usr/bin:/bin", "TMPDIR": "/tmp", "LC_ALL": "C", "TZ": "UTC"} {
		s.vars[k] = variable(v, true)
	}
	s.vars["PS1"] = variable("$ ", false)
	for k, v := range env {
		s.vars[k] = variable(v, true)
	}
	s.vars["PWD"] = variable(cwd, true)
	s.vars["PS4"] = variable("+ ", false)
	s.vars["OPTIND"] = variable("1", false)
	s.vars["OPTERR"] = variable("1", false)
	s.initDescriptors()
	return s
}
func (s *Shell) clone() *Shell {
	c := *s
	c.redirectFrame = nil
	c.trapInherited = true
	c.subshell = true
	for _, d := range c.fds {
		d.retain()
	}
	c.vars = maps.Clone(s.vars)
	c.funcs = maps.Clone(s.funcs)
	c.args = append([]string(nil), s.args...)
	c.locals = make([]variables, len(s.locals))
	for i, scope := range s.locals {
		c.locals[i] = maps.Clone(scope)
	}
	return &c
}

func Parse(src string) (*syntax.File, error) {
	if len(src) > MaxScript {
		return nil, ErrLimit
	}
	f, e := parseBashSource(src)
	if e != nil {
		return nil, e
	}
	depth, count := 0, 0
	syntax.Walk(f, func(n syntax.Node) bool {
		if e != nil {
			return false
		}
		if n == nil {
			depth--
			return true
		}
		depth++
		count++
		if depth > 128 || count > 20000 {
			e = ErrLimit
			return false
		}
		switch n := n.(type) {
		case *syntax.Stmt:
			if n.Background || n.Coprocess || n.Disown {
				e = errors.New("background jobs are not supported")
			}
		case *syntax.ProcSubst:
			e = errors.New("process substitution is not supported")
		case *syntax.Assign:
			if n.Array != nil || n.Index != nil {
				e = errors.New("array assignments are not supported")
			}
		case *syntax.FuncDecl:
			// The parser can represent an anonymous () declaration. It is not
			// an executable function in this shell and has no Name to dereference.
			if n.Name == nil || !syntax.ValidName(n.Name.Value) || n.Body == nil {
				e = errors.New("invalid function declaration")
			}
		case *syntax.ForClause:
			if n.Select {
				e = errors.New("select is not supported")
			}
		case *syntax.DeclClause:
			switch n.Variant.Value {
			case "export", "local", "declare", "typeset", "readonly":
			default:
				e = fmt.Errorf("%s is not supported", n.Variant.Value)
			}
		case *syntax.UnaryTest:
			if !supportedUnaryTest(n.Op) {
				e = fmt.Errorf("unsupported conditional operator: %s", n.Op)
			}
		case *syntax.BinaryTest:
			if !supportedBinaryTest(n.Op) {
				e = fmt.Errorf("unsupported conditional operator: %s", n.Op)
			}
		case *syntax.TimeClause, *syntax.CoprocClause, *syntax.LetClause:
			e = fmt.Errorf("unsupported shell construct %T", n)
		case *syntax.Redirect:
			e = validateRedirect(n)
		}
		return e == nil
	})
	if e == nil {
		syntax.Walk(f, func(n syntax.Node) bool {
			if test, ok := n.(*syntax.TestClause); ok {
				test.X = normalizeTest(test.X)
			}
			return true
		})
	}
	return f, e
}
func (s *Shell) Run(ctx context.Context, src string, streams IO, maxSteps int64) (int, error) {
	s.Exited = false
	*s.baseIO = streams
	f, e := Parse(src)
	if e != nil {
		return 2, e
	}
	r := &run{ctx: ctx, maxSteps: maxSteps}
	code, e := s.list(r, f.Stmts, streams)
	var control flow
	if errors.As(e, &control) && control.terminates() {
		s.Exited = true
		code, e = s.finish(r, code, e)
		s.status = code
		return code, e
	}
	return code, e
}
func (r *run) step() error {
	if e := r.ctx.Err(); e != nil {
		return e
	}
	if r.steps.Add(1) > r.maxSteps {
		return ErrLimit
	}
	return nil
}
func (s *Shell) list(r *run, stmts []*syntax.Stmt, streams IO) (int, error) {
	code := 0
	for _, stmt := range stmts {
		var e error
		code, e = s.stmt(r, stmt, streams)
		s.status = code
		if e != nil {
			return code, e
		}
	}
	return code, nil
}
func (s *Shell) stmt(r *run, stmt *syntax.Stmt, streams IO) (int, error) {
	return s.stmtWithPipe(r, stmt, streams, false)
}
func (s *Shell) stmtWithPipe(r *run, stmt *syntax.Stmt, streams IO, pipeAll bool) (code int, err error) {
	streams = s.streams()
	if stmt.Negated {
		s.ignoreErrexit++
		defer func() { s.ignoreErrexit-- }()
	}
	diagnostics := streams.Err
	var closers []io.Closer
	failedRedirect := false
	defer func() {
		var unset expand.UnsetParameterError
		var readonly readonlyError
		if errors.As(err, &unset) || errors.As(err, &readonly) {
			fmt.Fprintln(diagnostics, "go-bash:", err)
			code, err = 1, flow{"fatal", 1}
		}
		for i := len(closers) - 1; i >= 0; i-- {
			if closeErr := closers[i].Close(); closeErr != nil {
				var control flow
				if err == nil || errors.As(err, &control) {
					code, err = 1, fmt.Errorf("close redirection: %w", closeErr)
				}
			}
		}
		// Lists and compound commands already checked their executed children.
		// In particular, a skipped right side of && must not trigger errexit.
		check := failedRedirect
		switch c := stmt.Cmd.(type) {
		case nil, *syntax.CallExpr, *syntax.Subshell, *syntax.TestClause, *syntax.ArithmCmd, *syntax.DeclClause:
			check = true
		case *syntax.BinaryCmd:
			check = check || c.Op == syntax.Pipe || c.Op == syntax.PipeAll
		}
		if stmt.Negated && err == nil {
			code = conditionStatus(code != 0)
		}
		if err == nil && code != 0 && check && s.errexit && s.ignoreErrexit == 0 {
			err = flow{"errexit", code}
		}
		s.status = code
	}()
	if e := r.step(); e != nil {
		return 1, e
	}
	s.subStatus = 0
	// Simple-command words expand before redirections and prefix assignments.
	// In particular, a redirection's ${x:=...} must not affect earlier words.
	call, simple := stmt.Cmd.(*syntax.CallExpr)
	var args []string
	if simple {
		var e error
		args, e = s.fields(r, call.Args, streams)
		if e != nil {
			return 1, e
		}
	}
	io2, closers, e := s.redirect(r, stmt.Redirs, streams)
	if e != nil {
		var unset expand.UnsetParameterError
		var readonly readonlyError
		if errors.Is(e, ErrLimit) || r.ctx.Err() != nil || errors.As(e, &unset) || errors.As(e, &readonly) {
			return 1, e
		}
		fmt.Fprintln(streams.Err, "go-bash:", e)
		failedRedirect = true
		return 1, nil
	}
	// Bash's |& duplicates stderr after applying the command's redirections.
	if pipeAll {
		closers = append(closers, s.changeDescriptor(2, s.fds[1].retain()))
		io2 = s.streams()
	}
	diagnostics = io2.Err
	if simple {
		previous := s.redirectFrame
		s.redirectFrame = &redirectFrame{changes: closers}
		defer func() { s.redirectFrame = previous }()
		code, e = s.call(r, call, args, io2)
	} else {
		code, e = s.command(r, stmt.Cmd, io2)
	}
	s.status = code
	return code, e
}
func (s *Shell) command(r *run, cmd syntax.Command, streams IO) (int, error) {
	switch c := cmd.(type) {
	case nil:
		return s.subStatus, nil
	case *syntax.CallExpr:
		return 2, errors.New("internal error: simple command bypassed expansion")
	case *syntax.Block:
		return s.list(r, c.Stmts, streams)
	case *syntax.Subshell:
		child := s.clone()
		code, e := child.list(r, c.Stmts, streams)
		code, e = child.finish(r, code, e)
		child.closeResult(&code, &e)
		var f flow
		if errors.As(e, &f) && f.terminates() {
			e = nil
		}
		return code, e
	case *syntax.BinaryCmd:
		if c.Op == syntax.Pipe || c.Op == syntax.PipeAll {
			return s.pipeline(r, c, streams)
		}
		code, e := s.ignoringErrexit(func() (int, error) { return s.stmt(r, c.X, streams) })
		if e != nil {
			return code, e
		}
		if (c.Op == syntax.AndStmt && code == 0) || (c.Op == syntax.OrStmt && code != 0) {
			return s.stmt(r, c.Y, streams)
		}
		return code, nil
	case *syntax.IfClause:
		code, e := s.ignoringErrexit(func() (int, error) { return s.list(r, c.Cond, streams) })
		if e != nil {
			return code, e
		}
		if code == 0 {
			return s.list(r, c.Then, streams)
		}
		if c.Else != nil {
			return s.command(r, c.Else, streams)
		}
		return 0, nil
	case *syntax.ForClause:
		return s.forLoop(r, c, streams)
	case *syntax.CaseClause:
		return s.caseClause(r, c, streams)
	case *syntax.TestClause:
		return s.testExpr(r, c.X, streams)
	case *syntax.WhileClause:
		code := 0
		for {
			if e := r.step(); e != nil {
				return 1, e
			}
			cond, e := s.ignoringErrexit(func() (int, error) { return s.list(r, c.Cond, streams) })
			if e != nil {
				return cond, e
			}
			if (cond == 0) == c.Until {
				return code, nil
			}
			code, e = s.list(r, c.Do, streams)
			var f flow
			if errors.As(e, &f) {
				if f.kind == "break" {
					return 0, nil
				}
				if f.kind == "continue" {
					continue
				}
			}
			if e != nil {
				return code, e
			}
		}
	case *syntax.FuncDecl:
		if len(s.funcs) >= 1000 {
			return 1, ErrLimit
		}
		s.funcs[c.Name.Value] = c.Body
		return 0, nil
	case *syntax.DeclClause:
		return s.declare(r, c, streams)
	case *syntax.ArithmCmd:
		n, e := s.arithmetic(r, c.X, streams)
		if e != nil {
			return 1, e
		}
		if n == 0 {
			return 1, nil
		}
		return 0, nil
	default:
		return 2, fmt.Errorf("unsupported shell construct %T", cmd)
	}
}
func (s *Shell) ignoringErrexit(fn func() (int, error)) (int, error) {
	s.ignoreErrexit++
	defer func() { s.ignoreErrexit-- }()
	return fn()
}
func (s *Shell) assign(r *run, a *syntax.Assign, export bool, streams IO) error {
	v, e := s.literal(r, a.Value, streams)
	if e != nil {
		return e
	}
	traceName := a.Name.Value
	if a.Append {
		traceName += "+"
	}
	if err := s.traceAssignment(streams, traceName, v); err != nil {
		return err
	}
	if a.Append {
		v = s.vars.Get(a.Name.Value).String() + v
	}
	if a.Name.Value == "OPTIND" {
		s.optOffset = 0
	}
	return s.vars.Set(a.Name.Value, variable(v, export))
}
func (s *Shell) call(r *run, c *syntax.CallExpr, args []string, streams IO) (int, error) {
	if len(args) == 0 {
		for _, a := range c.Assigns {
			if e := s.assign(r, a, false, streams); e != nil {
				return 1, e
			}
		}
		return s.subStatus, nil
	}
	// Prefix assignments apply to the invoked command, after its arguments
	// have been expanded in the original environment.
	// Only the assigned variables are temporary. Builtins/functions still
	// change this shell's cwd and other variables, just as an unprefixed call.
	if len(c.Assigns) > 0 {
		before := make(map[string]expand.Variable)
		defer func() {
			for k, v := range before {
				if v.Declared() {
					s.vars[k] = v
				} else {
					delete(s.vars, k)
				}
			}
		}()
		for _, a := range c.Assigns {
			if _, saved := before[a.Name.Value]; !saved {
				before[a.Name.Value] = s.vars[a.Name.Value]
			}
			if e := s.assign(r, a, true, streams); e != nil {
				return 1, e
			}
		}
	}
	return s.invoke(r, args, streams)
}
func (s *Shell) invoke(r *run, args []string, streams IO) (int, error) {
	if err := s.trace(streams, args...); err != nil {
		return 1, err
	}
	if body := s.funcs[args[0]]; body != nil {
		if s.depth >= 64 {
			return 1, ErrLimit
		}
		old, generation := s.args, s.argsGeneration
		optIndex, optOffset, optWord := s.optIndex, s.optOffset, s.optWord
		s.args = args[1:]
		s.depth++
		s.locals = append(s.locals, make(variables))
		defer func() {
			scope := s.locals[len(s.locals)-1]
			if _, local := scope["OPTIND"]; local {
				s.optIndex, s.optOffset, s.optWord = optIndex, optOffset, optWord
			}
			for name, value := range scope {
				if value.Declared() {
					s.vars[name] = value
				} else {
					delete(s.vars, name)
				}
			}
			s.locals = s.locals[:len(s.locals)-1]
			s.args, s.argsGeneration = old, generation
			s.depth--
		}()
		code, e := s.stmt(r, body, streams)
		var f flow
		if errors.As(e, &f) && (f.kind == "exit" || s.subshell && f.kind == "errexit") {
			// Explicit exit (and errexit in a subshell) runs cleanup while
			// function locals are still visible.
			code, e = s.finish(r, code, e)
			if e == nil {
				e = flow{"exit", code}
			}
		}
		if errors.As(e, &f) && f.kind == "return" {
			e = nil
		}
		return code, e
	}
	return s.invokeBuiltin(r, args, streams)
}

func (s *Shell) invokeBuiltin(r *run, args []string, streams IO) (int, error) {
	switch args[0] {
	case ":", "true":
		return 0, nil
	case "false":
		return 1, nil
	case "set":
		return s.set(args[1:], streams)
	case "shift":
		return s.shift(args[1:], streams)
	case "read":
		return s.read(r, args[1:], streams)
	case "getopts":
		return s.getopts(args[1:], streams)
	case "trap":
		return s.trap(args[1:], streams)
	case "exec":
		return s.exec(r, args[1:], streams)
	case "umask":
		return s.mask(args[1:], streams)
	case "command":
		return s.commandBuiltin(r, args[1:], streams)
	case "type", "which":
		return s.describe(args[0], args[1:], streams)
	case "source", ".":
		return s.source(r, args[1:], streams)
	case "eval":
		return s.eval(r, args[1:], streams)
	case "export", "local", "declare", "typeset", "readonly":
		return s.declareWords(r, args[0], args[1:], streams)
	case "cd":
		if len(args) > 2 {
			fmt.Fprintln(streams.Err, "cd: too many arguments")
			return 1, nil
		}
		dest := s.vars.Get("HOME").String()
		if len(args) == 2 {
			dest = args[1]
		}
		if dest == "-" {
			dest = s.vars.Get("OLDPWD").String()
		}
		if dest == "" {
			fmt.Fprintln(streams.Err, "cd: null directory")
			return 1, nil
		}
		dest = fsys.Resolve(s.Cwd, dest)
		i, e := s.FS.Stat(dest)
		if e != nil || !i.IsDir() {
			fmt.Fprintln(streams.Err, "cd: cannot enter", dest)
			return 1, nil
		}
		oldCwd := s.Cwd
		s.Cwd = path.Clean(dest)
		if err := s.vars.Set("OLDPWD", variable(oldCwd, true)); err != nil {
			return variableFailure("cd", err, streams)
		}
		if err := s.vars.Set("PWD", variable(s.Cwd, true)); err != nil {
			return variableFailure("cd", err, streams)
		}
		return 0, nil
	case "pwd":
		if len(args) > 1 && !(len(args) == 2 && (args[1] == "-L" || args[1] == "-P")) {
			return 2, nil
		}
		_, e := fmt.Fprintln(streams.Out, s.Cwd)
		return 0, e
	case "unset":
		return s.unset(args[1:], streams)
	case "exit", "return":
		if args[0] == "return" && len(s.locals) == 0 && s.sourceDepth == 0 {
			fmt.Fprintln(streams.Err, "return: can only return from a function or sourced script")
			return 1, nil
		}
		code := s.status
		if len(args) > 2 {
			return 2, errors.New("too many arguments")
		}
		if len(args) == 2 {
			n, e := strconv.Atoi(args[1])
			if e != nil {
				return 2, e
			}
			code = int(uint8(n))
		}
		return code, flow{args[0], code}
	case "break", "continue":
		if len(args) != 1 {
			return 2, errors.New("only single-level loop control is supported")
		}
		return 0, flow{args[0], 0}
	case "bash", "sh":
		return s.nestedShell(r, args, streams)
	case "xargs":
		return s.xargs(r, args[1:], streams)
	}
	return s.external(r, args, streams)
}

// external resolves registered commands and guest shell scripts using the same
// virtual PATH lookup as command, type, and which.
func (s *Shell) external(r *run, args []string, streams IO) (int, error) {
	streams.Umask = s.umask
	env := s.exported()
	env["PWD"] = s.Cwd
	names, denied := s.lookup(args[0], false, false)
	if len(names) == 0 {
		if denied {
			fmt.Fprintln(streams.Err, args[0]+": permission denied")
			return 126, nil
		}
		fmt.Fprintln(streams.Err, args[0]+": command not found")
		return 127, nil
	}
	name := fsys.Resolve(s.Cwd, names[0])
	if path.Dir(name) == "/bin" || path.Dir(name) == "/usr/bin" {
		return s.Exec(r.ctx, s.Cwd, append([]string{path.Base(name)}, args[1:]...), env, streams)
	}
	return s.script(r, names[0], args[1:], streams)
}

// xargs is a builtin because WASI cannot spawn processes: the command lines it
// assembles run through the shell's own command lookup, so `find | xargs grep`
// works while find's -exec cannot. Supported: -0, -n N, -I R, -r, -t, and --.
// Without -0, items are separated by blanks or newlines; quotes and backslashes
// group as in GNU xargs. Each invocation reads an empty stdin.
func (s *Shell) xargs(r *run, args []string, streams IO) (int, error) {
	var nul, skipEmpty, trace bool
	per, replace := 0, ""
	fail := func(msg string) (int, error) {
		fmt.Fprintln(streams.Err, "xargs: "+msg)
		return 1, nil
	}
	i := 0
	for ; i < len(args) && strings.HasPrefix(args[i], "-") && args[i] != "-"; i++ {
		a := args[i]
		if a == "--" {
			i++
			break
		}
		if strings.HasPrefix(a, "--") {
			switch a {
			case "--null":
				nul = true
			case "--no-run-if-empty":
				skipEmpty = true
			case "--verbose":
				trace = true
			default:
				return fail("unsupported option " + a)
			}
			continue
		}
		flag, value := a[:2], a[2:]
		switch flag {
		case "-0":
			nul = true
		case "-r":
			skipEmpty = true
		case "-t":
			trace = true
		case "-n", "-I":
			if value == "" {
				if i+1 >= len(args) {
					return fail("option requires an argument -- '" + flag[1:] + "'")
				}
				i++
				value = args[i]
			}
			if flag == "-I" {
				replace = value
				per = 1
				continue
			}
			n, e := strconv.Atoi(value)
			if e != nil || n <= 0 {
				return fail("invalid number for -n option")
			}
			per = n
			value = ""
		default:
			return fail("unsupported option " + a)
		}
		if value != "" {
			return fail("unsupported option " + a)
		}
	}
	cmd := args[i:]
	if len(cmd) == 0 {
		cmd = []string{"echo"}
	}
	input, e := io.ReadAll(io.LimitReader(streams.In, MaxScript+1))
	if e != nil {
		return 1, e
	}
	if len(input) > MaxScript {
		return 1, ErrLimit
	}
	var items []string
	switch {
	case nul:
		items = strings.Split(strings.TrimSuffix(string(input), "\x00"), "\x00")
		if len(input) == 0 {
			items = nil
		}
	case replace != "":
		for _, line := range strings.Split(strings.TrimSuffix(string(input), "\n"), "\n") {
			if line = strings.TrimLeft(line, " \t"); line != "" {
				items = append(items, line)
			}
		}
	default:
		items, e = xargsSplit(string(input))
		if e != nil {
			return fail(e.Error())
		}
	}
	if len(items) == 0 && skipEmpty {
		return 0, nil
	}
	status := 0
	childInput := newDescriptor(strings.NewReader(""), nil, nil)
	childInput.defaultInput = &IO{} // xargs commands have no supplied stdin
	restore := s.changeDescriptor(0, childInput)
	defer restore.Close()
	child := s.streams()
	for first := true; first || len(items) > 0; first = false {
		if e := r.step(); e != nil {
			return 1, e
		}
		batch := items
		if per > 0 && per < len(items) {
			batch = items[:per]
		}
		items = items[len(batch):]
		argv := append([]string(nil), cmd...)
		if replace != "" {
			for j := 1; j < len(argv) && len(batch) == 1; j++ {
				argv[j] = strings.ReplaceAll(argv[j], replace, batch[0])
			}
		} else {
			argv = append(argv, batch...)
		}
		if trace {
			fmt.Fprintln(streams.Err, strings.Join(argv, " "))
		}
		code, e := s.external(r, argv, child)
		if e != nil {
			return code, e
		}
		switch {
		case code == 126 || code == 127:
			return code, nil
		case code == 255:
			fmt.Fprintln(streams.Err, "xargs: "+argv[0]+": exited with status 255; aborting")
			return 124, nil
		case code != 0:
			status = 123
		}
	}
	return status, nil
}

// xargsSplit tokenizes xargs input the way GNU does without -0: blanks and
// newlines separate items, single or double quotes group text, and a backslash
// escapes the next character.
func xargsSplit(input string) ([]string, error) {
	var items []string
	var cur strings.Builder
	inItem := false
	for i := 0; i < len(input); i++ {
		c := input[i]
		switch {
		case c == '\\':
			i++
			if i == len(input) {
				return nil, errors.New("backslash at end of input")
			}
			cur.WriteByte(input[i])
			inItem = true
		case c == '\'' || c == '"':
			end := strings.IndexByte(input[i+1:], c)
			if end < 0 || strings.Contains(input[i+1:i+1+end], "\n") {
				kind := "single"
				if c == '"' {
					kind = "double"
				}
				return nil, errors.New("unmatched " + kind + " quote; by default quotes are special to xargs unless you use the -0 option")
			}
			cur.WriteString(input[i+1 : i+1+end])
			inItem = true
			i += end + 1
		case c == ' ' || c == '\t' || c == '\n':
			if inItem {
				items = append(items, cur.String())
				cur.Reset()
				inItem = false
			}
		default:
			cur.WriteByte(c)
			inItem = true
		}
	}
	if inItem {
		items = append(items, cur.String())
	}
	return items, nil
}

func (s *Shell) exported() map[string]string {
	env := make(map[string]string)
	s.vars.Each(func(k string, v expand.Variable) bool {
		if v.Exported && v.IsSet() {
			env[k] = v.String()
		}
		return true
	})
	return env
}

type flow struct {
	kind string
	code int
}

func (f flow) Error() string { return f.kind }
func (f flow) terminates() bool {
	return f.kind == "exec" || f.kind == "exit" || f.kind == "errexit" || f.kind == "fatal"
}

func (s *Shell) pipeline(r *run, c *syntax.BinaryCmd, streams IO) (int, error) {
	reader, writer := newPipe()
	stop := context.AfterFunc(r.ctx, func() { reader.CloseWithError(r.ctx.Err()); writer.CloseWithError(r.ctx.Err()) })
	defer stop()
	leftIO := IO{In: streams.In, Out: writer, Err: streams.Err, InSet: streams.InSet}
	type result struct {
		code int
		err  error
	}
	done := make(chan result, 1)
	left, right := s.clone(), s.clone()
	_ = left.fds[1].release()
	left.fds[1] = newDescriptor(nil, writer, nil)
	_ = right.fds[0].release()
	right.fds[0] = newDescriptor(reader, nil, nil)
	go func() {
		code, e := left.stmtWithPipe(r, c.X, leftIO, c.Op == syntax.PipeAll)
		code, e = left.finish(r, code, e)
		left.closeResult(&code, &e)
		e = pipelineFlow(e)
		_ = writer.CloseWithError(e)
		done <- result{code, e}
	}()
	code, e := right.stmt(r, c.Y, IO{In: reader, Out: streams.Out, Err: streams.Err, InSet: true})
	code, e = right.finish(r, code, e)
	right.closeResult(&code, &e)
	e = pipelineFlow(e)
	_ = reader.Close()
	l := <-done
	if r.ctx.Err() != nil {
		return 1, r.ctx.Err()
	}
	if e != nil {
		return code, e
	}
	if l.err != nil && !errors.Is(l.err, io.ErrClosedPipe) {
		return l.code, l.err
	}
	if s.pipefail && code == 0 {
		if errors.Is(l.err, io.ErrClosedPipe) {
			return 141, nil // the virtual equivalent of SIGPIPE
		}
		return l.code, nil
	}
	return code, nil
}

func pipelineFlow(err error) error {
	var f flow
	if errors.As(err, &f) && f.terminates() {
		return nil
	}
	return err
}

func (s *Shell) config(r *run, streams IO) *expand.Config {
	env := &environment{s: s}
	return &expand.Config{Env: env, NoUnset: s.nounset, ReadDir2: func(p string) ([]fs.DirEntry, error) {
		if e := r.step(); e != nil {
			return nil, e
		}
		entries, e := s.FS.ReadDir(fsys.Resolve(s.Cwd, p))
		if len(entries) > 10000 {
			return nil, ErrLimit
		}
		return entries, e
	}, CmdSubst: func(w io.Writer, c *syntax.CmdSubst) error {
		if s.depth >= 64 {
			return ErrLimit
		}
		child := s.clone()
		child.depth++
		child.traceDepth++
		child.errexit = false // Bash clears -e in command substitutions by default.
		out := &boundedWriter{w: w, left: maxExpansion}
		_ = child.fds[1].release()
		child.fds[1] = newDescriptor(nil, out, nil)
		code, e := child.list(r, c.Stmts, IO{In: streams.In, Out: out, Err: streams.Err, InSet: streams.InSet})
		code, e = child.finish(r, code, e)
		child.closeResult(&code, &e)
		s.subStatus = code
		if out.err != nil {
			return out.err
		}
		var control flow
		if errors.As(e, &control) && control.terminates() {
			e = nil
		}
		return e
	}}
}
func (s *Shell) fields(r *run, words []*syntax.Word, streams IO) ([]string, error) {
	var fields []string
	size := 0
	cfg := s.config(r, streams)
	env := cfg.Env
	// The pinned FieldsSeq eagerly captures PWD for globbing, then lazily
	// expands words. Give that capture the real cwd while allowing "$PWD" in
	// words to retain its ordinary (possibly assigned or unset) variable value.
	cfg.Env = globEnvironment{cfg.Env.(*environment)}
	seq := expand.FieldsSeq(cfg, words...)
	cfg.Env = env
	for field, e := range seq {
		if e != nil {
			return nil, e
		}
		if e := r.ctx.Err(); e != nil {
			return nil, e
		}
		size += len(field)
		if len(fields) >= 10000 || size > maxExpansion {
			return nil, ErrLimit
		}
		fields = append(fields, field)
	}
	return fields, nil
}

type boundedWriter struct {
	mu   sync.Mutex
	w    io.Writer
	left int
	err  error
}

func (w *boundedWriter) Write(p []byte) (int, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	if len(p) > w.left {
		w.err = ErrLimit
		return 0, ErrLimit
	}
	n, e := w.w.Write(p)
	w.left -= n
	return n, e
}

type variables map[string]expand.Variable

func variable(v string, exported bool) expand.Variable {
	return expand.Variable{Set: true, Kind: expand.String, Str: v, Exported: exported}
}
func (v variables) Get(k string) expand.Variable { return v[k] }
func (v variables) Each(f func(string, expand.Variable) bool) {
	for k, val := range v {
		if !f(k, val) {
			return
		}
	}
}
func (v variables) Set(k string, val expand.Variable) error {
	if len(val.Str) > maxExpansion || len(v) > 10000 {
		return ErrLimit
	}
	old := v[k]
	if old.ReadOnly {
		return readonlyError(k)
	}
	val.Exported = val.Exported || old.Exported
	val.ReadOnly = val.ReadOnly || old.ReadOnly
	val.Local = val.Local || old.Local
	v[k] = val
	return nil
}

type environment struct{ s *Shell }

type globEnvironment struct{ *environment }

func (e globEnvironment) Get(k string) expand.Variable {
	if k == "PWD" {
		return variable(e.s.Cwd, true)
	}
	return e.environment.Get(k)
}

func (e *environment) Get(k string) expand.Variable {
	switch k {
	case "?":
		return variable(strconv.Itoa(e.s.status), false)
	case "#":
		return variable(strconv.Itoa(len(e.s.args)), false)
	case "-":
		flags := ""
		if e.s.errexit {
			flags += "e"
		}
		if e.s.nounset {
			flags += "u"
		}
		if e.s.xtrace {
			flags += "x"
		}
		return variable(flags, false)
	case "@", "*":
		args := e.s.args
		if args == nil {
			args = []string{} // expansion distinguishes empty "$@" from a scalar
		}
		return expand.Variable{Set: true, Kind: expand.Indexed, List: args}
	case "$", "PPID":
		return variable("1", false)
	case "0":
		if v := e.s.vars[k]; v.IsSet() {
			return v
		}
		return variable("go-bash", false)
	}
	// Satisfy the expansion library's home lookup hook even for unknown users,
	// preserving their literal tilde instead of falling back to os/user.Lookup.
	if strings.HasPrefix(k, "HOME ") {
		return variable("~"+strings.TrimPrefix(k, "HOME "), false)
	}
	if i, err := strconv.Atoi(k); err == nil && i > 0 && i <= len(e.s.args) {
		return variable(e.s.args[i-1], false)
	}
	return e.s.vars.Get(k)
}
func (e *environment) Each(f func(string, expand.Variable) bool) { e.s.vars.Each(f) }
func (e *environment) Set(k string, v expand.Variable) error     { return e.s.vars.Set(k, v) }

// Strip only literal source tabs, before expansion. Tabs supplied by a variable
// or command substitution must survive. Do not mutate a stored function's AST.
func stripHeredocTabs(word *syntax.Word) *syntax.Word {
	copyWord := *word
	copyWord.Parts = append([]syntax.WordPart(nil), word.Parts...)
	start := true
	for i, part := range copyWord.Parts {
		lit, ok := part.(*syntax.Lit)
		if !ok {
			start = false
			continue
		}
		value := *lit
		var b strings.Builder
		for _, c := range value.Value {
			if start && c == '\t' {
				continue
			}
			b.WriteRune(c)
			start = c == '\n'
		}
		value.Value = b.String()
		copyWord.Parts[i] = &value
	}
	return &copyWord
}
