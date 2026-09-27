package shell

import (
	"errors"
	"fmt"
	"maps"
	"slices"
	"strings"

	"mvdan.cc/sh/v3/expand"
	"mvdan.cc/sh/v3/syntax"
)

type readonlyError string

func (e readonlyError) Error() string { return string(e) + ": readonly variable" }

// Declaration and read failures are ordinary command failures; an assignment
// to a readonly variable outside a builtin terminates the current shell.
func variableFailure(builtin string, err error, streams IO) (int, error) {
	var readonly readonlyError
	if errors.As(err, &readonly) {
		fmt.Fprintf(streams.Err, "%s: %s\n", builtin, err)
		return 1, nil
	}
	return 1, err
}

type declaration struct {
	name, value      string
	assigned, append bool
}

func declarationWord(word string) declaration {
	name, value, assigned := strings.Cut(word, "=")
	appendValue := assigned && strings.HasSuffix(name, "+")
	if appendValue {
		name = strings.TrimSuffix(name, "+")
	}
	return declaration{name: name, value: value, assigned: assigned, append: appendValue}
}

func (s *Shell) declare(r *run, c *syntax.DeclClause, streams IO) (int, error) {
	var operands []declaration
	var trace []string
	trace = append(trace, c.Variant.Value)
	// Expand the whole command before introducing locals, so `local x=$x`
	// reads the caller's x and adjacent operands see the same outer scope.
	for _, a := range c.Args {
		if a.Name == nil {
			fields, err := s.fields(r, []*syntax.Word{a.Value}, streams)
			if err != nil {
				return 1, err
			}
			for _, field := range fields {
				operands = append(operands, declarationWord(field))
				trace = append(trace, field)
			}
			continue
		}
		operand := declaration{name: a.Name.Value, assigned: !a.Naked, append: a.Append}
		word := operand.name
		if operand.assigned {
			value, err := s.literal(r, a.Value, streams)
			if err != nil {
				return 1, err
			}
			operand.value = value
			if operand.append {
				word += "+"
			}
			word += "=" + value
		}
		operands = append(operands, operand)
		trace = append(trace, word)
	}
	if err := s.trace(streams, trace...); err != nil {
		return 1, err
	}
	return s.declareValues(c.Variant.Value, operands, streams)
}

// Quoted builtin names and `command declare ...` arrive as ordinary words.
// Their values are already expanded and must never be reparsed as shell code.
func (s *Shell) declareWords(_ *run, builtin string, args []string, streams IO) (int, error) {
	operands := make([]declaration, len(args))
	for i, arg := range args {
		operands[i] = declarationWord(arg)
	}
	return s.declareValues(builtin, operands, streams)
}

func (s *Shell) declareValues(builtin string, operands []declaration, streams IO) (int, error) {
	local := builtin == "local" || ((builtin == "declare" || builtin == "typeset") && len(s.locals) > 0)
	if builtin == "local" && len(s.locals) == 0 {
		fmt.Fprintln(streams.Err, "local: can only be used in a function")
		return 1, nil
	}
	global, printOnly := false, false
	readonly, export := builtin == "readonly", 0
	if builtin == "export" {
		export = 1
	}
	for len(operands) > 0 {
		arg := operands[0]
		if arg.assigned || len(arg.name) < 2 || (arg.name[0] != '-' && arg.name[0] != '+') {
			break
		}
		operands = operands[1:]
		if arg.name == "--" {
			break
		}
		for _, flag := range arg.name[1:] {
			enable := arg.name[0] == '-'
			switch {
			case flag == 'p' && enable:
				printOnly = true
			case flag == 'r' && enable && builtin != "export":
				readonly = true
			case flag == 'x' && builtin != "readonly":
				export = -1
				if enable {
					export = 1
				}
			case flag == 'n' && enable && builtin == "export":
				export = -1
			case flag == 'g' && enable && (builtin == "declare" || builtin == "typeset"):
				global, local = true, false
			default:
				fmt.Fprintf(streams.Err, "%s: unsupported option: %c%c\n", builtin, arg.name[0], flag)
				return 2, nil
			}
		}
	}
	if printOnly || len(operands) == 0 {
		return s.printDeclarations(builtin, operands, streams)
	}
	code := 0
	for _, operand := range operands {
		name := operand.name
		if !syntax.ValidName(name) {
			fmt.Fprintf(streams.Err, "%s: %s: invalid variable name\n", builtin, name)
			code = 1
			continue
		}
		target := s.vars
		if global {
			// The outermost saved binding is the global when locals shadow it.
			for _, scope := range s.locals {
				if _, ok := scope[name]; ok {
					target = scope
					break
				}
			}
		}
		value := target[name]
		if value.ReadOnly && (operand.assigned || local) {
			fmt.Fprintf(streams.Err, "%s: %s\n", builtin, readonlyError(name))
			code = 1
			continue
		}
		if local {
			scope := s.locals[len(s.locals)-1]
			if _, exists := scope[name]; !exists {
				scope[name] = value
				value = expand.Variable{Local: true, Exported: value.Exported}
			}
		}
		if operand.assigned {
			if operand.append {
				value.Str += operand.value
			} else {
				value.Str = operand.value
			}
			value.Set = true
		}
		value.Kind = expand.String
		value.ReadOnly = value.ReadOnly || readonly
		if export != 0 {
			value.Exported = export > 0
		}
		if len(value.Str) > maxExpansion || len(target) >= 10000 && !target[name].Declared() {
			return 1, ErrLimit
		}
		// Attribute-only declarations may mark an existing readonly binding as
		// exported; they do not assign its value and bypass Set intentionally.
		target[name] = value
		if name == "OPTIND" && operand.assigned && !global {
			s.optOffset = 0
		}
	}
	return code, nil
}

func (s *Shell) printDeclarations(builtin string, operands []declaration, streams IO) (int, error) {
	names := make([]string, 0, len(operands))
	for _, arg := range operands {
		names = append(names, arg.name)
	}
	if len(operands) == 0 {
		for _, name := range slices.Sorted(maps.Keys(s.vars)) {
			v := s.vars[name]
			if !syntax.ValidName(name) || !v.Declared() {
				continue
			}
			if builtin == "export" && !v.Exported || builtin == "readonly" && !v.ReadOnly || builtin == "local" && !v.Local {
				continue
			}
			names = append(names, name)
		}
	}
	code := 0
	for _, name := range names {
		v := s.vars[name]
		if !v.Declared() {
			fmt.Fprintf(streams.Err, "%s: %s: not found\n", builtin, name)
			code = 1
			continue
		}
		flags := v.Flags()
		if flags == "" {
			flags = "-"
		}
		line := fmt.Sprintf("declare -%s %s", flags, name)
		if v.IsSet() {
			// Bash's declaration output always double-quotes string values.
			value := strings.NewReplacer("\\", "\\\\", "\"", "\\\"", "$", "\\$", "`", "\\`").Replace(v.Str)
			line += "=\"" + value + "\""
		}
		if _, err := fmt.Fprintln(streams.Out, line); err != nil {
			return 1, err
		}
	}
	return code, nil
}

func (s *Shell) unset(args []string, streams IO) (int, error) {
	functions, variablesOnly := false, false
	for len(args) > 0 && strings.HasPrefix(args[0], "-") {
		arg := args[0]
		args = args[1:]
		if arg == "--" {
			break
		}
		switch arg {
		case "-f":
			functions = true
		case "-v":
			variablesOnly = true
		default:
			fmt.Fprintln(streams.Err, "unset: unsupported option:", arg)
			return 2, nil
		}
	}
	if functions && variablesOnly {
		return 2, nil
	}
	code := 0
	for _, name := range args {
		if functions {
			delete(s.funcs, name)
			continue
		}
		value := s.vars[name]
		if value.ReadOnly {
			fmt.Fprintf(streams.Err, "unset: %s\n", readonlyError(name))
			code = 1
			continue
		}
		if !value.Declared() && !variablesOnly {
			delete(s.funcs, name)
		}
		if value.Local {
			s.vars[name] = expand.Variable{Local: true}
		} else {
			delete(s.vars, name)
		}
	}
	return code, nil
}
