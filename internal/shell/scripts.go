package shell

import (
	"errors"
	"fmt"
	"io"
	"io/fs"
	"path"
	"strings"

	"github.com/adrianliechti/go-bash/internal/fsys"
)

func (s *Shell) readScript(name string) (string, error) {
	f, err := s.FS.Open(fsys.Resolve(s.Cwd, name), 0, 0)
	if err != nil {
		return "", err
	}
	defer f.Close()
	info, err := f.Stat()
	if err != nil {
		return "", err
	}
	if !info.Mode().IsRegular() {
		return "", fmt.Errorf("%s: not a regular file", name)
	}
	return readScript(f)
}

func readScript(reader io.Reader) (string, error) {
	data, err := io.ReadAll(io.LimitReader(reader, MaxScript+1))
	if len(data) > MaxScript {
		return "", ErrLimit
	}
	return string(data), err
}

func (s *Shell) runText(r *run, src string, streams IO) (int, error) {
	if s.depth >= 64 {
		return 1, ErrLimit
	}
	if err := r.step(); err != nil {
		return 1, err
	}
	f, err := Parse(src)
	if err != nil {
		if errors.Is(err, ErrLimit) {
			return 2, err
		}
		fmt.Fprintln(streams.Err, "go-bash:", err)
		return 2, flow{"fatal", 2}
	}
	s.depth++
	defer func() { s.depth-- }()
	return s.list(r, f.Stmts, streams)
}

func (s *Shell) eval(r *run, args []string, streams IO) (int, error) {
	if len(args) > 0 && args[0] == "--" {
		args = args[1:]
	}
	s.traceDepth++
	defer func() { s.traceDepth-- }()
	return s.runText(r, strings.Join(args, " "), streams)
}

// Sourcing searches PATH without requiring execute bits, then falls back to
// cwd as non-POSIX Bash does. All lookup and reads remain in the guest FS.
func (s *Shell) source(r *run, args []string, streams IO) (int, error) {
	if len(args) > 0 && args[0] == "--" {
		args = args[1:]
	}
	if len(args) == 0 {
		fmt.Fprintln(streams.Err, "source: filename argument required")
		return 2, nil
	}
	name := args[0]
	if !strings.Contains(name, "/") {
		for _, dir := range strings.Split(s.vars.Get("PATH").String(), ":") {
			candidate := fsys.Resolve(s.Cwd, path.Join(dir, name))
			if info, err := s.FS.Stat(candidate); err == nil && info.Mode().IsRegular() {
				name = candidate
				break
			}
		}
	}
	src, err := s.readScript(name)
	if err != nil {
		if errors.Is(err, ErrLimit) {
			return 1, err
		}
		fmt.Fprintln(streams.Err, "source:", err)
		return 1, nil
	}
	if len(args) > 1 {
		old, generation := s.args, s.argsGeneration
		s.args = append([]string(nil), args[1:]...)
		defer func() {
			// Bash restores source arguments after shift, but preserves an
			// explicit set of new positional parameters by the sourced file.
			if s.argsGeneration == generation {
				s.args = old
			}
		}()
	}
	s.sourceDepth++
	s.traceDepth++
	defer func() { s.sourceDepth--; s.traceDepth-- }()
	code, err := s.runText(r, src, streams)
	var control flow
	if errors.As(err, &control) && control.kind == "return" {
		err = nil
	}
	return code, err
}

func (s *Shell) childShell() *Shell {
	child := New(s.FS, s.Cwd, s.exported(), s.Exec)
	child.depth = s.depth + 1
	child.umask = s.umask
	child.inheritDescriptors(s)
	return child
}

func (s *Shell) runChild(r *run, child *Shell, src string, streams IO) (int, error) {
	code, err := child.runText(r, src, streams)
	code, err = child.finish(r, code, err)
	var control flow
	if errors.As(err, &control) && control.terminates() {
		err = nil
	}
	return code, err
}

func (s *Shell) nestedShell(r *run, args []string, streams IO) (code int, err error) {
	child := s.childShell()
	defer child.closeResult(&code, &err)
	child.vars["0"] = variable(args[0], false)
	args = args[1:]
	command := false
	for len(args) > 0 && len(args[0]) > 1 && (args[0][0] == '-' || args[0][0] == '+') {
		arg := args[0]
		args = args[1:]
		if arg == "--" {
			break
		}
		for _, flag := range arg[1:] {
			if flag == 'c' && arg[0] == '-' {
				command = true
				continue
			}
			option := []string{string(arg[0]) + string(flag)}
			if flag == 'o' && len(args) > 0 {
				option = append(option, args[0])
				args = args[1:]
			}
			if code, err := child.set(option, streams); code != 0 || err != nil {
				return code, err
			}
		}
	}
	var src string
	switch {
	case command:
		if len(args) == 0 {
			fmt.Fprintln(streams.Err, "go-bash: -c requires a script")
			return 2, nil
		}
		src = args[0]
		if len(args) > 1 {
			child.vars["0"] = variable(args[1], false)
			child.args = append([]string(nil), args[2:]...)
		}
	case len(args) == 0 || args[0] == "-":
		src, err = readScript(streams.In)
		if len(args) > 0 {
			child.args = append([]string(nil), args[1:]...)
		}
	default:
		src, err = s.readScript(args[0])
		child.vars["0"] = variable(args[0], false)
		child.args = append([]string(nil), args[1:]...)
	}
	if err != nil {
		if errors.Is(err, ErrLimit) {
			return 1, err
		}
		fmt.Fprintln(streams.Err, "go-bash:", err)
		if errors.Is(err, fs.ErrNotExist) {
			return 127, nil
		}
		return 126, nil
	}
	return s.runChild(r, child, src, streams)
}

func (s *Shell) script(r *run, name string, args []string, streams IO) (code int, err error) {
	src, err := s.readScript(name)
	if err != nil {
		if errors.Is(err, ErrLimit) {
			return 1, err
		}
		fmt.Fprintln(streams.Err, name+":", err)
		return 126, nil
	}
	line, _, _ := strings.Cut(src, "\n")
	interpreter := strings.Fields(strings.TrimPrefix(line, "#!"))
	if strings.HasPrefix(line, "#!") && len(interpreter) > 0 && path.Base(interpreter[0]) == "env" {
		interpreter = interpreter[1:]
		if len(interpreter) > 0 && interpreter[0] == "-S" {
			interpreter = interpreter[1:]
		}
	}
	if !strings.HasPrefix(line, "#!") || len(interpreter) == 0 || (path.Base(interpreter[0]) != "sh" && path.Base(interpreter[0]) != "bash") {
		fmt.Fprintln(streams.Err, name+": unsupported executable (expected a sh or bash shebang)")
		return 126, nil
	}
	child := s.childShell()
	defer child.closeResult(&code, &err)
	if len(interpreter) > 1 {
		if code, err := child.set(interpreter[1:], streams); code != 0 || err != nil {
			return code, err
		}
		if len(child.args) > 0 {
			fmt.Fprintln(streams.Err, name+": unsupported shebang arguments")
			return 126, nil
		}
	}
	child.vars["0"] = variable(name, false)
	child.args = append([]string(nil), args...)
	return s.runChild(r, child, src, streams)
}
