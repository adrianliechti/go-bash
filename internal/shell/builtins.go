package shell

import (
	"errors"
	"fmt"
	"io"
	"maps"
	"slices"
	"strconv"
	"strings"
	"unicode/utf8"

	"mvdan.cc/sh/v3/syntax"
)

func (s *Shell) set(args []string, streams IO) (int, error) {
	if len(args) == 0 {
		for _, name := range slices.Sorted(maps.Keys(s.vars)) {
			if !syntax.ValidName(name) || !s.vars[name].IsSet() {
				continue
			}
			value, err := syntax.Quote(s.vars[name].String(), syntax.LangBash)
			if err != nil {
				return 1, err
			}
			if _, err := fmt.Fprintf(streams.Out, "%s=%s\n", name, value); err != nil {
				return 1, err
			}
		}
		return 0, nil
	}
	for len(args) > 0 {
		arg := args[0]
		if arg == "--" {
			s.args = append([]string(nil), args[1:]...)
			return 0, nil
		}
		if arg == "-" {
			s.xtrace = false
			args = args[1:]
			break
		}
		if len(arg) < 2 || (arg[0] != '-' && arg[0] != '+') {
			break
		}
		args = args[1:]
		for _, flag := range arg[1:] {
			name := ""
			switch flag {
			case 'e':
				name = "errexit"
			case 'u':
				name = "nounset"
			case 'x':
				name = "xtrace"
			case 'o':
				if len(args) == 0 {
					if err := s.printOptions(streams.Out, arg[0] == '+'); err != nil {
						return 1, err
					}
					continue
				}
				name, args = args[0], args[1:]
			default:
				fmt.Fprintf(streams.Err, "set: unsupported option: %c%c\n", arg[0], flag)
				return 2, nil
			}
			option := s.option(name)
			if option == nil {
				fmt.Fprintln(streams.Err, "set: unsupported option:", name)
				return 2, nil
			}
			*option = arg[0] == '-'
		}
	}
	if len(args) > 0 {
		s.args = append([]string(nil), args...)
	}
	return 0, nil
}

func (s *Shell) option(name string) *bool {
	switch name {
	case "errexit":
		return &s.errexit
	case "nounset":
		return &s.nounset
	case "xtrace":
		return &s.xtrace
	case "pipefail":
		return &s.pipefail
	}
	return nil
}

func (s *Shell) printOptions(w io.Writer, commands bool) error {
	for _, name := range []string{"errexit", "nounset", "pipefail", "xtrace"} {
		state, flag := "off", "+o"
		if *s.option(name) {
			state, flag = "on", "-o"
		}
		var err error
		if commands {
			_, err = fmt.Fprintf(w, "set %s %s\n", flag, name)
		} else {
			_, err = fmt.Fprintf(w, "%s\t%s\n", name, state)
		}
		if err != nil {
			return err
		}
	}
	return nil
}

func (s *Shell) trace(streams IO, args ...string) error {
	if !s.xtrace || len(args) == 0 {
		return nil
	}
	quoted := make([]string, len(args))
	for i, arg := range args {
		value, err := syntax.Quote(arg, syntax.LangBash)
		if err != nil {
			return err
		}
		quoted[i] = value
	}
	return s.writeTrace(streams, strings.Join(quoted, " "))
}

func (s *Shell) traceAssignment(streams IO, name, value string) error {
	if !s.xtrace {
		return nil
	}
	quoted, err := syntax.Quote(value, syntax.LangBash)
	if err != nil {
		return err
	}
	return s.writeTrace(streams, name+"="+quoted)
}

func (s *Shell) writeTrace(streams IO, text string) error {
	prefix := s.vars.Get("PS4").String()
	if prefix != "" {
		_, size := utf8.DecodeRuneInString(prefix)
		prefix = strings.Repeat(prefix[:size], s.traceDepth) + prefix
	}
	_, err := fmt.Fprintln(streams.Err, prefix+text)
	return err
}

func (s *Shell) shift(args []string, streams IO) (int, error) {
	if len(args) > 0 && args[0] == "--" {
		args = args[1:]
	}
	n := 1
	var err error
	if len(args) == 1 {
		n, err = strconv.Atoi(args[0])
	}
	if len(args) > 1 || err != nil || n < 0 || n > len(s.args) {
		fmt.Fprintln(streams.Err, "shift: invalid count")
		return 1, nil
	}
	s.args = s.args[n:]
	return 0, nil
}

// read consumes one byte at a time so it never buffers input belonging to the
// next read or to another command sharing stdin. Escaped bytes retain their
// quoting during IFS splitting, including escaped whitespace and delimiters.
func (s *Shell) read(r *run, args []string, streams IO) (int, error) {
	raw, delimiter := false, byte('\n')
	for len(args) > 0 && strings.HasPrefix(args[0], "-") {
		arg := args[0]
		args = args[1:]
		if arg == "--" {
			break
		}
		if arg == "-" {
			fmt.Fprintln(streams.Err, "read: invalid option: -")
			return 2, nil
		}
		for i := 1; i < len(arg); i++ {
			switch arg[i] {
			case 'r':
				raw = true
			case 'd':
				value := arg[i+1:]
				if value == "" {
					if len(args) == 0 {
						fmt.Fprintln(streams.Err, "read: -d requires a delimiter")
						return 2, nil
					}
					value, args = args[0], args[1:]
				}
				delimiter = 0
				if value != "" {
					delimiter = value[0]
				}
				i = len(arg)
			default:
				fmt.Fprintf(streams.Err, "read: unsupported option: -%c\n", arg[i])
				return 2, nil
			}
		}
	}
	for _, name := range args {
		if !syntax.ValidName(name) {
			fmt.Fprintln(streams.Err, "read: invalid variable name:", name)
			return 1, nil
		}
	}
	var line []byte
	var quoted []bool
	escaped, code := false, 1
	var one [1]byte
	for consumed := 0; ; consumed++ {
		if err := r.ctx.Err(); err != nil {
			return 1, err
		}
		if consumed > MaxScript {
			return 1, ErrLimit
		}
		_, err := io.ReadFull(streams.In, one[:])
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return 1, err
		}
		c := one[0]
		if c == 0 && delimiter != 0 {
			continue
		}
		if escaped {
			escaped = false
			if c != '\n' {
				line = append(line, c)
				quoted = append(quoted, true)
			}
			continue
		}
		if !raw && c == '\\' {
			escaped = true
			continue
		}
		if c == delimiter {
			code = 0
			break
		}
		line = append(line, c)
		quoted = append(quoted, false)
	}
	if len(args) == 0 {
		if err := s.vars.Set("REPLY", variable(string(line), false)); err != nil {
			return variableFailure("read", err, streams)
		}
		return code, nil
	}
	ifs := " \t\n"
	if v := s.vars.Get("IFS"); v.IsSet() {
		ifs = v.String()
	}
	values := readFields(string(line), quoted, ifs, len(args))
	for i, name := range args {
		value := ""
		if i < len(values) {
			value = values[i]
		}
		if err := s.vars.Set(name, variable(value, false)); err != nil {
			return variableFailure("read", err, streams)
		}
	}
	return code, nil
}

func readFields(line string, quoted []bool, ifs string, count int) []string {
	type span struct{ start, end int }
	var spans []span
	delimiter := func(i int) (separator, whitespace bool, size int) {
		ch, size := utf8.DecodeRuneInString(line[i:])
		separator = !quoted[i] && strings.ContainsRune(ifs, ch)
		return separator, separator && strings.ContainsRune(" \t\n\r\f\v", ch), size
	}
	skipSpace := func(i int) int {
		for i < len(line) {
			_, white, size := delimiter(i)
			if !white {
				break
			}
			i += size
		}
		return i
	}
	for i := skipSpace(0); i < len(line); {
		start := i
		for i < len(line) {
			sep, _, size := delimiter(i)
			if sep {
				break
			}
			i += size
		}
		spans = append(spans, span{start, i})
		if len(spans) > count {
			break
		}
		i = skipSpace(i)
		if i < len(line) {
			if sep, _, size := delimiter(i); sep {
				i = skipSpace(i + size)
			}
		}
	}
	if len(spans) > count {
		// Extra fields go into the last variable with their separators intact;
		// Bash trims trailing IFS whitespace from this remainder even if it
		// was escaped. A single field above retains its quoted trailing space.
		end := len(line)
		for end > 0 {
			ch, size := utf8.DecodeLastRuneInString(line[:end])
			if !strings.ContainsRune(ifs, ch) || !strings.ContainsRune(" \t\n\r\f\v", ch) {
				break
			}
			end -= size
		}
		spans = spans[:count]
		spans[count-1].end = end
	}
	values := make([]string, len(spans))
	for i, field := range spans {
		values[i] = line[field.start:field.end]
	}
	return values
}
