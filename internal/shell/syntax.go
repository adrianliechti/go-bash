package shell

import (
	"errors"
	"reflect"
	"sort"
	"strings"

	"mvdan.cc/sh/v3/syntax"
)

// mvdan's lexer compares physical lines with a here-document delimiter before
// folding escaped newlines. Bash compares logical lines. Use the parser's own
// redirect nodes to locate unquoted documents and repair only a folded closing
// delimiter; quoted documents, body contents, and subsequent source stay intact.
// Remap positions afterwards to preserve physical line numbers without adding
// blank lines which would become data in another pending here-document.
func parseBashSource(src string) (*syntax.File, error) {
	original := src
	var offsets []uint
	finish := func(file *syntax.File, err error) (*syntax.File, error) {
		if offsets != nil {
			remapPositions(file, original, offsets)
			var parseErr syntax.ParseError
			if errors.As(err, &parseErr) {
				parseErr.Pos = originalPosition(parseErr.Pos, original, offsets)
				err = parseErr
			}
		}
		return file, err
	}
	for attempt := 0; attempt < 64; attempt++ {
		parser := syntax.NewParser(syntax.Variant(syntax.LangBash))
		file, err := parser.Parse(strings.NewReader(src), "script")
		probe := file
		if err != nil {
			// An unclosed document discards its statement from the returned AST.
			// Supply terminators only to obtain a diagnostic tree, never to run it.
			// The repaired original source is always parsed again without recovery.
			padded, probeErr := src, err
			for n := 0; probeErr != nil && n < 16; n++ {
				var parseErr syntax.ParseError
				if !errors.As(probeErr, &parseErr) || !strings.HasPrefix(parseErr.Text, "unclosed here-document") {
					return finish(file, err)
				}
				offset := int(parseErr.Pos.Offset()) + 2
				if offset >= len(padded) {
					return finish(file, err)
				}
				if padded[offset] == '-' {
					offset++
				}
				var word *syntax.Word
				_ = syntax.NewParser().Words(strings.NewReader(padded[offset:]), func(w *syntax.Word) bool {
					word = w
					return false
				})
				if word == nil || word.Lit() == "" || strings.ContainsAny(word.Lit(), "\\\n") {
					return finish(file, err)
				}
				padded += "\n" + word.Lit() + "\n"
				if len(padded) > 2*MaxScript {
					return nil, ErrLimit
				}
				probe, probeErr = syntax.NewParser(syntax.Variant(syntax.LangBash), syntax.RecoverErrors(128)).Parse(strings.NewReader(padded), "script")
			}
			if probeErr != nil {
				return finish(file, err)
			}
		}
		start, end, replacement := -1, 0, ""
		depth, count := 0, 0
		limited := false
		syntax.Walk(probe, func(node syntax.Node) bool {
			if node == nil {
				depth--
				return true
			}
			if start >= 0 || limited {
				return false
			}
			depth++
			count++
			if depth > 128 || count > 20000 {
				limited = true
				return false
			}
			if rd, ok := node.(*syntax.Redirect); ok && (rd.Op == syntax.Hdoc || rd.Op == syntax.DashHdoc) && rd.Hdoc != nil {
				stop := rd.Word.Lit()
				if stop != "" && !strings.Contains(stop, "\\") {
					offset := int(rd.Hdoc.Pos().Offset())
					if offset <= len(src) {
						for offset >= 2 && src[offset-2:offset] == "\\\n" {
							offset -= 2
						}
						start, end, replacement = joinedHeredocEnd(src, offset, stop, rd.Op == syntax.DashHdoc)
					}
				}
			}
			return true
		})
		if limited {
			return nil, ErrLimit
		}
		if start < 0 {
			return finish(file, err)
		}
		if offsets == nil {
			offsets = make([]uint, len(src)+1)
			for i := range offsets {
				offsets[i] = uint(i)
			}
		}
		updated := make([]uint, 0, len(src)-(end-start)+len(replacement)+1)
		updated = append(updated, offsets[:start]...)
		for i := range len(replacement) {
			updated = append(updated, offsets[min(start+i, end)])
		}
		offsets = append(updated, offsets[end:]...)
		src = src[:start] + replacement + src[end:]
	}
	return nil, ErrLimit
}

func originalPosition(pos syntax.Pos, source string, offsets []uint) syntax.Pos {
	if !pos.IsValid() || int(pos.Offset()) >= len(offsets) {
		return pos
	}
	offset := offsets[pos.Offset()]
	before := source[:offset]
	return syntax.NewPos(offset, uint(strings.Count(before, "\n")+1), offset-uint(strings.LastIndexByte(before, '\n')+1)+1)
}

func remapPositions(file *syntax.File, source string, offsets []uint) {
	lineStarts := []int{0}
	for i, ch := range source {
		if ch == '\n' {
			lineStarts = append(lineStarts, i+1)
		}
	}
	// AST nodes expose their position fields, but there is no position visitor.
	// Limit reflection to those fields; syntax.Walk owns traversal of the tree.
	syntax.Walk(file, func(node syntax.Node) bool {
		if node == nil {
			return true
		}
		value := reflect.ValueOf(node).Elem()
		for i := 0; i < value.NumField(); i++ {
			field := value.Field(i)
			if field.Type() != reflect.TypeFor[syntax.Pos]() || !field.CanSet() {
				continue
			}
			pos := field.Interface().(syntax.Pos)
			if !pos.IsValid() || int(pos.Offset()) >= len(offsets) {
				continue
			}
			offset := offsets[pos.Offset()]
			line := sort.Search(len(lineStarts), func(i int) bool { return lineStarts[i] > int(offset) })
			field.Set(reflect.ValueOf(syntax.NewPos(offset, uint(line), offset-uint(lineStarts[line-1])+1)))
		}
		return true
	})
}

func joinedHeredocEnd(src string, start int, stop string, stripTabs bool) (int, int, string) {
	var line strings.Builder
	joined := false
	for i := start; i <= len(src); i++ {
		if i < len(src) && src[i] == '\\' && i+1 < len(src) {
			if src[i+1] == '\n' {
				joined = true
			} else {
				line.WriteString(src[i : i+2])
			}
			i++
			continue
		}
		if i == len(src) || src[i] == '\n' {
			end := i
			if i < len(src) {
				end++
			}
			text := line.String()
			if stripTabs {
				text = strings.TrimLeft(text, "\t")
			}
			if text == stop {
				if joined {
					ending := ""
					if i < len(src) {
						ending = "\n"
					}
					return start, end, stop + ending
				}
				break
			}
			line.Reset()
			start, joined = end, false
		} else {
			line.WriteByte(src[i])
		}
	}
	return -1, 0, ""
}

// The pinned parser groups ! with the remainder of a [[ ]] expression and
// associates && and || without their Bash precedence. Normalize the validated
// tree once, before it can be shared by function invocations and pipelines.
func normalizeTest(expr syntax.TestExpr) syntax.TestExpr {
	switch x := expr.(type) {
	case *syntax.ParenTest:
		x.X = normalizeTest(x.X)
	case *syntax.UnaryTest:
		if x.Op == syntax.TsNot {
			x.X = normalizeTest(x.X)
			return bindTestNegation(x)
		}
	case *syntax.BinaryTest:
		if x.Op == syntax.AndTest || x.Op == syntax.OrTest {
			x.X, x.Y = normalizeTest(x.X), normalizeTest(x.Y)
			if right, ok := x.Y.(*syntax.BinaryTest); ok && x.Op == syntax.AndTest && right.Op == syntax.OrTest {
				x.Y, right.X = right.X, x
				return right
			}
		}
	}
	return expr
}

func bindTestNegation(not *syntax.UnaryTest) syntax.TestExpr {
	if binary, ok := not.X.(*syntax.BinaryTest); ok && (binary.Op == syntax.AndTest || binary.Op == syntax.OrTest) {
		not.X = binary.X
		binary.X = bindTestNegation(not)
		return binary
	}
	return not
}
