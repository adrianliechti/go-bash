package shell

import (
	"context"
	"errors"
	"fmt"
	"strings"
)

// trap supports EXIT/0. Host signals and asynchronous jobs are deliberately not
// installed by a virtual shell embedded in someone else's Go process.
func (s *Shell) trap(args []string, streams IO) (int, error) {
	if len(args) > 0 && args[0] == "--" {
		args = args[1:]
	}
	print := len(args) == 0
	if len(args) > 0 && args[0] == "-p" {
		print = true
		args = args[1:]
	}
	if print {
		for _, sig := range args {
			if sig != "EXIT" && sig != "0" {
				fmt.Fprintln(streams.Err, "trap: unsupported signal:", sig)
				return 1, nil
			}
		}
		if s.trapSet {
			_, err := fmt.Fprintf(streams.Out, "trap -- '%s' EXIT\n", strings.ReplaceAll(s.exitTrap, "'", "'\\''"))
			return 0, err
		}
		return 0, nil
	}
	if len(args) == 1 && args[0] == "0" {
		s.exitTrap, s.trapSet, s.trapInherited = "", false, false
		return 0, nil
	}
	if len(args) < 2 {
		fmt.Fprintln(streams.Err, "trap: expected a command and EXIT")
		return 2, nil
	}
	for _, sig := range args[1:] {
		if sig != "EXIT" && sig != "0" {
			fmt.Fprintln(streams.Err, "trap: unsupported signal:", sig)
			return 1, nil
		}
	}
	s.exitTrap, s.trapSet, s.trapInherited = args[0], args[0] != "-", false
	return 0, nil
}

func (s *Shell) finish(r *run, code int, err error) (int, error) {
	var control flow
	if errors.As(err, &control) && control.terminates() {
		code, err = control.code, nil
		if control.kind == "exec" {
			s.trapSet = false
			return code, nil
		}
	}
	// Cancellation, output limits and other embedding errors cannot be hidden
	// by cleanup code. The same run budget applies to a script and its trap.
	if err != nil {
		return code, err
	}
	if !s.trapSet || s.trapInherited {
		return code, nil
	}
	text := s.exitTrap
	s.exitTrap, s.trapSet = "", false
	s.status = code
	defer func() { s.exitTrap, s.trapSet = "", false }()
	if text == "" {
		return code, nil
	}
	trapCode, trapErr := s.runText(r, text, s.streams())
	// Ordinary commands in a trap preserve the original exit status. An
	// explicit exit or an errexit failure can replace it.
	if errors.As(trapErr, &control) && control.terminates() {
		return trapCode, nil
	}
	return code, trapErr
}

// Finish represents EOF for a persistent session. Run alone does not end it.
func (s *Shell) Finish(ctx context.Context, streams IO, maxSteps int64) (int, error) {
	*s.baseIO = streams
	s.Exited = true
	code, err := s.finish(&run{ctx: ctx, maxSteps: maxSteps}, s.status, nil)
	s.status = code
	return code, err
}

func (s *Shell) exec(r *run, args []string, streams IO) (int, error) {
	if len(args) > 0 && args[0] == "--" {
		args = args[1:]
	}
	if len(args) > 0 && strings.HasPrefix(args[0], "-") {
		fmt.Fprintln(streams.Err, "exec: unsupported option:", args[0])
		return 2, nil
	}
	if len(args) == 0 {
		if s.redirectFrame != nil {
			if err := s.redirectFrame.commit(); err != nil {
				return 1, err
			}
		}
		return 0, nil
	}
	if args[0] != "bash" && args[0] != "sh" {
		if names, _ := s.lookup(args[0], false, false); len(names) == 0 {
			code, err := s.external(r, args, streams)
			if err == nil {
				err = flow{"exit", code}
			}
			return code, err
		}
	}
	var code int
	var err error
	if args[0] == "bash" || args[0] == "sh" {
		code, err = s.nestedShell(r, args, streams)
	} else {
		code, err = s.external(r, args, streams)
	}
	if err != nil {
		return code, err
	}
	if s.redirectFrame != nil {
		if err := s.redirectFrame.commit(); err != nil {
			return 1, err
		}
	}
	return code, flow{"exec", code}
}
