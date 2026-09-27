package shell

import (
	"fmt"
	"strconv"
	"strings"

	"mvdan.cc/sh/v3/syntax"
)

func (s *Shell) getopts(args []string, streams IO) (int, error) {
	if len(args) < 2 || !syntax.ValidName(args[1]) {
		fmt.Fprintln(streams.Err, "getopts: expected an option string and variable name")
		return 2, nil
	}
	spec, name := args[0], args[1]
	input := s.args
	if len(args) > 2 {
		input = args[2:]
	}
	index, err := strconv.Atoi(s.vars.Get("OPTIND").String())
	if err != nil || index < 1 {
		index, s.optOffset = 1, 0
	}
	if index != s.optIndex {
		s.optOffset = 0
	}
	silent := strings.HasPrefix(spec, ":")
	spec = strings.TrimPrefix(spec, ":")
	option, value, haveValue, code := "?", "", false, 0
	if s.optOffset == 0 && (index > len(input) || !strings.HasPrefix(input[index-1], "-") || input[index-1] == "-" || input[index-1] == "--") {
		if index <= len(input) && input[index-1] == "--" {
			index++
		}
		code = 1
	} else {
		if s.optOffset == 0 {
			s.optWord = input[index-1]
			s.optOffset = 1
		}
		word := s.optWord
		flag := word[s.optOffset]
		s.optOffset++
		at := strings.IndexByte(spec, flag)
		if flag == ':' || flag == '?' || at < 0 {
			if silent {
				value, haveValue = string(flag), true
			} else if s.vars.Get("OPTERR").String() != "0" {
				fmt.Fprintf(streams.Err, "%s: illegal option -- %c\n", (&environment{s}).Get("0").String(), flag)
			}
		} else {
			option = string(flag)
			if at+1 < len(spec) && spec[at+1] == ':' {
				switch {
				case s.optOffset < len(word):
					value, haveValue = word[s.optOffset:], true
				case index < len(input):
					value, haveValue = input[index], true
					index++
				default:
					option = "?"
					if silent {
						option, value, haveValue = ":", string(flag), true
					} else if s.vars.Get("OPTERR").String() != "0" {
						fmt.Fprintf(streams.Err, "%s: option requires an argument -- %c\n", (&environment{s}).Get("0").String(), flag)
					}
				}
				s.optOffset = len(word)
			}
		}
		if s.optOffset == len(word) {
			index++
			s.optOffset = 0
		}
	}
	s.optIndex = index
	for _, item := range [][2]string{{"OPTIND", strconv.Itoa(index)}, {name, option}} {
		if err := s.vars.Set(item[0], variable(item[1], false)); err != nil {
			return variableFailure("getopts", err, streams)
		}
	}
	if haveValue {
		if err := s.vars.Set("OPTARG", variable(value, false)); err != nil {
			return variableFailure("getopts", err, streams)
		}
	} else if _, err := s.unset([]string{"OPTARG"}, streams); err != nil {
		return 1, err
	}
	return code, nil
}

func (s *Shell) mask(args []string, streams IO) (int, error) {
	symbolic, reusable := false, false
	for len(args) > 0 && strings.HasPrefix(args[0], "-") {
		arg := args[0]
		args = args[1:]
		if arg == "--" {
			break
		}
		for _, flag := range strings.TrimPrefix(arg, "-") {
			switch flag {
			case 'S':
				symbolic = true
			case 'p':
				reusable = true
			default:
				fmt.Fprintln(streams.Err, "umask: unsupported option:", arg)
				return 2, nil
			}
		}
	}
	if len(args) == 0 {
		text := fmt.Sprintf("%04o", s.umask)
		if symbolic {
			var parts []string
			for i, who := range "ugo" {
				allowed := (^s.umask >> (6 - 3*i)) & 7
				part := string(who) + "="
				for j, permission := range "rwx" {
					if allowed&(4>>j) != 0 {
						part += string(permission)
					}
				}
				parts = append(parts, part)
			}
			text = strings.Join(parts, ",")
		}
		if reusable {
			if symbolic {
				text = "-S " + text
			}
			text = "umask " + text
		}
		_, err := fmt.Fprintln(streams.Out, text)
		return 0, err
	}
	if len(args) != 1 {
		fmt.Fprintln(streams.Err, "umask: too many arguments")
		return 1, nil
	}
	if mask, err := strconv.ParseUint(args[0], 8, 32); err == nil {
		s.umask = uint32(mask) & 0777
		return 0, nil
	}
	allowed := ^s.umask & 0777
	for _, clause := range strings.Split(args[0], ",") {
		who, i := uint32(0), 0
		for i < len(clause) && strings.ContainsRune("ugoa", rune(clause[i])) {
			switch clause[i] {
			case 'u':
				who |= 0700
			case 'g':
				who |= 0070
			case 'o':
				who |= 0007
			case 'a':
				who |= 0777
			}
			i++
		}
		if who == 0 {
			who = 0777
		}
		if i == len(clause) || !strings.ContainsRune("=+-", rune(clause[i])) {
			fmt.Fprintln(streams.Err, "umask: invalid mask:", args[0])
			return 1, nil
		}
		for i < len(clause) {
			op, bits := clause[i], uint32(0)
			if !strings.ContainsRune("=+-", rune(op)) {
				fmt.Fprintln(streams.Err, "umask: invalid mask:", args[0])
				return 1, nil
			}
			i++
			for i < len(clause) && !strings.ContainsRune("=+-", rune(clause[i])) {
				switch clause[i] {
				case 'r':
					bits |= 0444
				case 'w':
					bits |= 0222
				case 'x':
					bits |= 0111
				case 'X':
					if allowed&0111 != 0 {
						bits |= 0111
					}
				case 'u', 'g', 'o':
					shift := uint(0)
					if clause[i] == 'u' {
						shift = 6
					} else if clause[i] == 'g' {
						shift = 3
					}
					perm := (allowed >> shift) & 7
					bits |= perm | perm<<3 | perm<<6
				default:
					fmt.Fprintln(streams.Err, "umask: invalid permission:", string(clause[i]))
					return 1, nil
				}
				i++
			}
			switch op {
			case '=':
				allowed = allowed&^who | bits&who
			case '+':
				allowed |= bits & who
			case '-':
				allowed &^= bits & who
			}
		}
	}
	s.umask = ^allowed & 0777
	return 0, nil
}
