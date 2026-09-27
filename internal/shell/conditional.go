package shell

import (
	"errors"
	"fmt"
	"io/fs"
	"regexp"
	"strings"

	"github.com/adrianliechti/go-bash/internal/fsys"
	"mvdan.cc/sh/v3/expand"
	"mvdan.cc/sh/v3/pattern"
	"mvdan.cc/sh/v3/syntax"
)

func (s *Shell) forLoop(r *run, c *syntax.ForClause, streams IO) (int, error) {
	var items []string
	var arithmetic *syntax.CStyleLoop
	switch loop := c.Loop.(type) {
	case *syntax.WordIter:
		items = s.args
		if loop.InPos.IsValid() {
			var err error
			items, err = s.fields(r, loop.Items, streams)
			if err != nil {
				return 1, err
			}
		}
	case *syntax.CStyleLoop:
		arithmetic = loop
		if loop.Init != nil {
			if _, err := expand.Arithm(s.config(r, streams), loop.Init); err != nil {
				return 1, err
			}
		}
	}
	code := 0
	for i := 0; arithmetic != nil || i < len(items); i++ {
		if err := r.step(); err != nil {
			return 1, err
		}
		if arithmetic != nil {
			if arithmetic.Cond != nil {
				n, err := expand.Arithm(s.config(r, streams), arithmetic.Cond)
				if err != nil {
					return 1, err
				}
				if n == 0 {
					break
				}
			}
		} else if err := s.vars.Set(c.Loop.(*syntax.WordIter).Name.Value, variable(items[i], false)); err != nil {
			return 1, err
		}
		var err error
		code, err = s.list(r, c.Do, streams)
		var control flow
		if errors.As(err, &control) {
			if control.kind == "break" {
				return 0, nil
			}
			if control.kind == "continue" {
				err = nil // arithmetic loops still execute their post expression
			}
		}
		if err != nil {
			return code, err
		}
		if arithmetic != nil && arithmetic.Post != nil {
			if _, err := expand.Arithm(s.config(r, streams), arithmetic.Post); err != nil {
				return 1, err
			}
		}
	}
	return code, nil
}

func (s *Shell) literal(r *run, word *syntax.Word, streams IO) (string, error) {
	v, err := expand.Literal(s.config(r, streams), quoteLiteralEscapes(word))
	if len(v) > maxExpansion {
		return "", ErrLimit
	}
	return v, err
}

// Literal retains backslashes in unquoted source text in the pinned expansion
// library. Quote those escaped characters explicitly, leaving expansions and
// already quoted text alone. Splitting the literal also preserves whether a
// leading tilde is quoted. Never modify an AST shared by function invocations.
func quoteLiteralEscapes(word *syntax.Word) *syntax.Word {
	if word == nil {
		return nil
	}
	copyWord := *word
	copyWord.Parts = nil
	for _, part := range word.Parts {
		lit, ok := part.(*syntax.Lit)
		if !ok || !strings.Contains(lit.Value, "\\") {
			copyWord.Parts = append(copyWord.Parts, part)
			continue
		}
		value := lit.Value
		for {
			i := strings.IndexByte(value, '\\')
			if i < 0 || i+1 == len(value) {
				break
			}
			prefix := *lit
			prefix.Value = value[:i]
			copyWord.Parts = append(copyWord.Parts, &prefix, &syntax.SglQuoted{Value: value[i+1 : i+2]})
			value = value[i+2:]
		}
		suffix := *lit
		suffix.Value = value
		copyWord.Parts = append(copyWord.Parts, &suffix)
	}
	return &copyWord
}

func (s *Shell) match(r *run, word *syntax.Word, value string, streams IO) (bool, error) {
	pat, err := expand.Pattern(s.config(r, streams), word)
	if err != nil {
		return false, err
	}
	if len(pat) > maxExpansion {
		return false, ErrLimit
	}
	expr, err := pattern.Regexp(pat, pattern.EntireString|pattern.ExtendedOperators)
	if err != nil {
		var malformed *pattern.SyntaxError
		if errors.As(err, &malformed) {
			return false, nil
		}
		return false, err
	}
	re, err := regexp.Compile(expr)
	if err != nil {
		return false, err
	}
	return re.MatchString(value), nil
}

func (s *Shell) caseClause(r *run, c *syntax.CaseClause, streams IO) (int, error) {
	value, err := s.literal(r, c.Word, streams)
	if err != nil {
		return 1, err
	}
	code, fall := 0, false
	for _, item := range c.Items {
		if err := r.step(); err != nil {
			return 1, err
		}
		matched := fall
		if !matched {
			for _, pat := range item.Patterns {
				matched, err = s.match(r, pat, value, streams)
				if err != nil {
					return 1, err
				}
				if matched {
					break
				}
			}
		}
		if !matched {
			continue
		}
		code, err = s.list(r, item.Stmts, streams)
		if err != nil || item.Op == syntax.Break {
			return code, err
		}
		fall = item.Op == syntax.Fallthrough
	}
	return code, nil
}

// Reject predicates outside the virtual filesystem model before executing any
// statements. In particular, regex matching and ownership tests are not yet
// implemented; accepting their syntax must not silently change their meaning.
func supportedUnaryTest(op syntax.UnTestOperator) bool {
	switch op {
	case syntax.TsNot, syntax.TsEmpStr, syntax.TsNempStr, syntax.TsVarSet, syntax.TsOptSet,
		syntax.TsExists, syntax.TsRegFile, syntax.TsDirect, syntax.TsNoEmpty, syntax.TsSmbLink:
		return true
	}
	return false
}

func supportedBinaryTest(op syntax.BinTestOperator) bool {
	switch op {
	case syntax.AndTest, syntax.OrTest, syntax.TsMatchShort, syntax.TsMatch, syntax.TsNoMatch,
		syntax.TsBefore, syntax.TsAfter, syntax.TsEql, syntax.TsNeq, syntax.TsLeq, syntax.TsGeq,
		syntax.TsLss, syntax.TsGtr, syntax.TsNewer, syntax.TsOlder:
		return true
	}
	return false
}

func conditionStatus(ok bool) int {
	if ok {
		return 0
	}
	return 1
}

func (s *Shell) testExpr(r *run, expr syntax.TestExpr, streams IO) (int, error) {
	if err := r.step(); err != nil {
		return 1, err
	}
	switch x := expr.(type) {
	case *syntax.Word:
		v, err := s.literal(r, x, streams)
		return conditionStatus(v != ""), err
	case *syntax.ParenTest:
		return s.testExpr(r, x.X, streams)
	case *syntax.UnaryTest:
		if x.Op == syntax.TsNot {
			code, err := s.testExpr(r, x.X, streams)
			if err != nil || code > 1 {
				return code, err
			}
			return conditionStatus(code != 0), nil
		}
		v, err := s.literal(r, x.X.(*syntax.Word), streams)
		if err != nil {
			return 1, err
		}
		return conditionStatus(s.unaryTest(x.Op, v)), nil
	case *syntax.BinaryTest:
		if x.Op == syntax.AndTest || x.Op == syntax.OrTest {
			code, err := s.testExpr(r, x.X, streams)
			if err != nil || code > 1 || (x.Op == syntax.AndTest && code != 0) || (x.Op == syntax.OrTest && code == 0) {
				return code, err
			}
			return s.testExpr(r, x.Y, streams)
		}
		left, err := s.literal(r, x.X.(*syntax.Word), streams)
		if err != nil {
			return 1, err
		}
		if x.Op == syntax.TsMatch || x.Op == syntax.TsMatchShort || x.Op == syntax.TsNoMatch {
			matched, err := s.match(r, x.Y.(*syntax.Word), left, streams)
			return conditionStatus(matched != (x.Op == syntax.TsNoMatch)), err
		}
		right, err := s.literal(r, x.Y.(*syntax.Word), streams)
		if err != nil {
			return 1, err
		}
		if x.Op >= syntax.TsEql && x.Op <= syntax.TsGtr {
			a, err := s.testArithmetic(r, left, streams)
			if err != nil {
				return 2, err
			}
			b, err := s.testArithmetic(r, right, streams)
			if err != nil {
				return 2, err
			}
			var ok bool
			switch x.Op {
			case syntax.TsEql:
				ok = a == b
			case syntax.TsNeq:
				ok = a != b
			case syntax.TsLeq:
				ok = a <= b
			case syntax.TsGeq:
				ok = a >= b
			case syntax.TsLss:
				ok = a < b
			case syntax.TsGtr:
				ok = a > b
			}
			return conditionStatus(ok), nil
		}
		switch x.Op {
		case syntax.TsBefore:
			return conditionStatus(left < right), nil
		case syntax.TsAfter:
			return conditionStatus(left > right), nil
		case syntax.TsNewer, syntax.TsOlder:
			if x.Op == syntax.TsOlder {
				left, right = right, left
			}
			a, ea := s.statTest(left, false)
			b, eb := s.statTest(right, false)
			return conditionStatus(ea == nil && (eb != nil || a.ModTime().After(b.ModTime()))), nil
		}
	}
	return 2, fmt.Errorf("unsupported conditional expression %T", expr)
}

func (s *Shell) testArithmetic(r *run, value string, streams IO) (int, error) {
	if strings.TrimSpace(value) == "" {
		return 0, nil
	}
	// Validate expanded arithmetic with the same syntax restrictions and depth
	// limits as a script, and ensure it cannot inject additional statements.
	f, err := Parse("((" + value + "))")
	if err != nil {
		return 0, err
	}
	if len(f.Stmts) == 1 && len(f.Stmts[0].Redirs) == 0 {
		if cmd, ok := f.Stmts[0].Cmd.(*syntax.ArithmCmd); ok {
			return expand.Arithm(s.config(r, streams), cmd.X)
		}
	}
	return 0, errors.New("invalid conditional arithmetic expression")
}

func (s *Shell) statTest(name string, link bool) (fs.FileInfo, error) {
	if name == "" {
		return nil, fs.ErrNotExist
	}
	name = fsys.Resolve(s.Cwd, name)
	if link {
		return s.FS.Lstat(name)
	}
	return s.FS.Stat(name)
}

func (s *Shell) unaryTest(op syntax.UnTestOperator, value string) bool {
	switch op {
	case syntax.TsEmpStr:
		return value == ""
	case syntax.TsNempStr:
		return value != ""
	case syntax.TsVarSet:
		return (&environment{s: s}).Get(value).IsSet()
	case syntax.TsOptSet:
		option := s.option(value)
		return option != nil && *option
	}
	info, err := s.statTest(value, op == syntax.TsSmbLink)
	if err != nil {
		return false
	}
	switch op {
	case syntax.TsExists:
		return true
	case syntax.TsRegFile:
		return info.Mode().IsRegular()
	case syntax.TsDirect:
		return info.IsDir()
	case syntax.TsNoEmpty:
		return info.Size() > 0
	case syntax.TsSmbLink:
		return info.Mode()&fs.ModeSymlink != 0
	}
	return false
}
