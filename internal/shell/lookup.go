package shell

import (
	"fmt"
	"path"
	"strings"

	"github.com/adrianliechti/go-bash/internal/fsys"
	"mvdan.cc/sh/v3/syntax"
)

// lookup never consults the host PATH. Both discovery and execution check the
// guest filesystem, including executable scripts on user-supplied mounts.
func (s *Shell) lookup(name string, all, defaultPath bool) (names []string, denied bool) {
	if name == "" {
		return nil, false
	}
	paths := []string{name}
	if !strings.Contains(name, "/") {
		search := s.vars.Get("PATH").String()
		if defaultPath {
			search = "/usr/bin:/bin"
		}
		paths = nil
		for _, dir := range strings.Split(search, ":") {
			candidate := path.Join(dir, name)
			if dir == "" || dir == "." {
				candidate = "./" + name
			}
			paths = append(paths, candidate)
		}
	}
	for _, candidate := range paths {
		info, err := s.FS.Stat(fsys.Resolve(s.Cwd, candidate))
		if err != nil {
			continue
		}
		if !info.Mode().IsRegular() || info.Mode()&0111 == 0 {
			denied = true
			continue
		}
		names = append(names, candidate)
		if !all {
			break
		}
	}
	return names, denied
}

func shellKeyword(name string) bool {
	switch name {
	case "if", "then", "else", "elif", "fi", "case", "esac", "for", "while", "until", "do", "done", "in", "function", "!", "{", "}", "[[", "]]":
		return true
	}
	return false
}

type description struct{ kind, value string }

func (s *Shell) descriptions(name string, all, pathsOnly, noFunctions, defaultPath bool) []description {
	var matches []description
	if !pathsOnly {
		if shellKeyword(name) {
			matches = append(matches, description{"keyword", name})
		}
		if !noFunctions && s.funcs[name] != nil {
			matches = append(matches, description{"function", name})
		}
		if IsBuiltin(name) {
			matches = append(matches, description{"builtin", name})
		}
		if len(matches) > 0 && !all {
			return matches[:1]
		}
	}
	paths, _ := s.lookup(name, all, defaultPath)
	for _, p := range paths {
		matches = append(matches, description{"file", p})
	}
	return matches
}

func (s *Shell) printDescription(name string, d description, mode string, streams IO) error {
	var text string
	switch mode {
	case "kind":
		text = d.kind
	case "path":
		if d.kind != "file" {
			return nil
		}
		text = d.value
	case "short":
		text = d.value
	default:
		switch d.kind {
		case "file":
			text = name + " is " + d.value
		case "builtin":
			text = name + " is a shell builtin"
		case "keyword":
			text = name + " is a shell keyword"
		case "function":
			var body strings.Builder
			if err := syntax.NewPrinter().Print(&body, s.funcs[name]); err != nil {
				return err
			}
			text = name + " is a function\n" + name + " () \n" + body.String()
		}
	}
	_, err := fmt.Fprintln(streams.Out, text)
	return err
}

func (s *Shell) describe(builtin string, args []string, streams IO) (int, error) {
	all, pathsOnly, noFunctions := false, builtin == "which", false
	mode := "verbose"
	if pathsOnly {
		mode = "path"
	}
	for len(args) > 0 && strings.HasPrefix(args[0], "-") {
		arg := args[0]
		args = args[1:]
		if arg == "--" {
			break
		}
		if arg == "-" {
			return 2, nil
		}
		for _, flag := range arg[1:] {
			switch {
			case flag == 'a':
				all = true
			case builtin == "type" && flag == 't':
				mode = "kind"
			case builtin == "type" && flag == 'p':
				mode = "path"
			case builtin == "type" && flag == 'P':
				mode, pathsOnly = "path", true
			case builtin == "type" && flag == 'f':
				noFunctions = true
			default:
				fmt.Fprintf(streams.Err, "%s: unsupported option: -%c\n", builtin, flag)
				return 2, nil
			}
		}
	}
	code := 0
	for _, name := range args {
		matches := s.descriptions(name, all, pathsOnly || all && mode == "path", noFunctions, false)
		if len(matches) == 0 {
			if mode == "verbose" {
				fmt.Fprintf(streams.Err, "%s: %s: not found\n", builtin, name)
			}
			code = 1
		}
		for _, match := range matches {
			if err := s.printDescription(name, match, mode, streams); err != nil {
				return 1, err
			}
		}
	}
	return code, nil
}

func (s *Shell) commandBuiltin(r *run, args []string, streams IO) (int, error) {
	mode, defaultPath := "", false
	for len(args) > 0 && strings.HasPrefix(args[0], "-") {
		arg := args[0]
		args = args[1:]
		if arg == "--" {
			break
		}
		if arg == "-" {
			return 2, nil
		}
		for _, flag := range arg[1:] {
			switch flag {
			case 'v':
				mode = "short"
			case 'V':
				mode = "verbose"
			case 'p':
				defaultPath = true
			default:
				fmt.Fprintf(streams.Err, "command: unsupported option: -%c\n", flag)
				return 2, nil
			}
		}
	}
	if len(args) == 0 {
		return 0, nil
	}
	if mode != "" {
		code := 1
		for _, name := range args {
			matches := s.descriptions(name, false, false, false, defaultPath)
			if len(matches) == 0 {
				if mode == "verbose" {
					fmt.Fprintf(streams.Err, "command: %s: not found\n", name)
				}
				continue
			}
			code = 0
			if err := s.printDescription(name, matches[0], mode, streams); err != nil {
				return 1, err
			}
		}
		return code, nil
	}
	if s.depth >= 64 {
		return 1, ErrLimit
	}
	s.depth++
	defer func() { s.depth-- }()
	if defaultPath && !IsBuiltin(args[0]) {
		paths, _ := s.lookup(args[0], false, true)
		if len(paths) == 0 {
			fmt.Fprintln(streams.Err, args[0]+": command not found")
			return 127, nil
		}
		args = append([]string{paths[0]}, args[1:]...)
	}
	return s.invokeBuiltin(r, args, streams)
}
