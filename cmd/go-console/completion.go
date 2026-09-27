package main

import (
	"context"
	"strings"
	"time"
	"unicode"

	bash "github.com/adrianliechti/go-bash"
	"mvdan.cc/sh/v3/syntax"
)

type completer struct {
	shell interface {
		Complete(context.Context, string, bash.CompletionKind) ([]string, error)
	}
}

func (c completer) Do(line []rune, pos int) ([][]rune, int) {
	// Complete at word ends, leaving text after the cursor intact. Expansion
	// expressions are deliberately left to the parser, never evaluated by Tab.
	if pos < 0 || pos > len(line) || pos < len(line) && !strings.ContainsRune(" \t\n;|&)", line[pos]) {
		return nil, 0
	}
	token, ok := completionToken(line[:pos])
	if !ok {
		return nil, 0
	}
	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()
	names, err := c.shell.Complete(ctx, token.prefix, token.kind)
	if err != nil {
		return nil, 0
	}
	var candidates [][]rune
	for _, name := range names {
		if strings.IndexFunc(name, unicode.IsControl) >= 0 {
			continue
		}
		suffix := strings.TrimPrefix(name, token.prefix)
		if token.kind == bash.CompleteVariables {
			if token.braced {
				suffix += "}"
			}
		} else {
			suffix = quoteSuffix(suffix, token.quote)
			if !strings.HasSuffix(name, "/") {
				if token.quote != 0 {
					suffix += string(token.quote)
				}
				suffix += " "
			}
		}
		candidates = append(candidates, []rune(suffix))
	}
	return candidates, token.length
}

type tokenCompletion struct {
	prefix string
	kind   bash.CompletionKind
	quote  rune
	braced bool
	length int
}

// This is a conservative lexer for literal command/path prefixes, not a second
// shell parser. Complex expansions, comments, and heredocs have no completion.
func completionToken(line []rune) (tokenCompletion, bool) {
	var word []rune
	var quote rune
	escaped, complex, redirect := false, false, false
	command := ""
	start := 0
	finishWord := func() {
		if len(word) > 0 {
			if redirect {
				redirect = false
			} else if command == "" {
				name := string(word)
				key, _, assignment := strings.Cut(name, "=")
				if !assignment || !syntax.ValidName(key) {
					command = name
				}
			}
		}
		word, complex = nil, false
	}
	for i, r := range line {
		if escaped {
			if r != '\n' {
				word = append(word, r)
			}
			escaped = false
			continue
		}
		if r == '\\' && quote != '\'' {
			// Inside double quotes, backslash only escapes these characters.
			if quote == '"' && i+1 < len(line) && !strings.ContainsRune("$`\"\\\n", line[i+1]) {
				word = append(word, r)
				continue
			}
			escaped = true
			continue
		}
		if quote != 0 {
			if r == quote {
				quote = 0
			} else {
				word = append(word, r)
				complex = complex || quote == '"' && (r == '$' || r == '`')
			}
			continue
		}
		switch r {
		case '\'', '"':
			quote = r
		case ' ', '\t':
			finishWord()
			start = i + 1
		case ';', '|', '&', '(', ')', '\n':
			// Do not guess context within substitutions or heredocs.
			if complex {
				return tokenCompletion{}, false
			}
			finishWord()
			command, redirect, start = "", false, i+1
		case '<', '>':
			if r == '<' && i+1 < len(line) && line[i+1] == '<' {
				return tokenCompletion{}, false
			}
			// A decimal prefix is a descriptor number, not a command word.
			if len(word) > 0 && strings.Trim(string(word), "0123456789") == "" {
				word = nil
			}
			finishWord()
			redirect, start = true, i+1
		case '#':
			if len(word) == 0 {
				return tokenCompletion{}, false
			}
			word = append(word, r)
		default:
			complex = complex || strings.ContainsRune("$`*?[]{}~", r)
			word = append(word, r)
		}
	}
	if escaped {
		return tokenCompletion{}, false
	}
	prefix := string(word)
	token := tokenCompletion{prefix: prefix, quote: quote, length: len(line) - start, kind: bash.CompleteFiles}
	// Only a leading, unescaped '$' outside single quotes is variable completion.
	raw := strings.TrimPrefix(string(line[start:]), "\"")
	if quote != '\'' && strings.HasPrefix(raw, "$") {
		name := strings.TrimPrefix(raw, "$")
		token.braced = strings.HasPrefix(name, "{")
		name = strings.TrimPrefix(name, "{")
		if name == "" || syntax.ValidName(name) {
			token.kind, token.prefix = bash.CompleteVariables, name
			return token, true
		}
	}
	if complex {
		return tokenCompletion{}, false
	}
	if !redirect {
		if command == "" {
			token.kind = bash.CompleteCommands
		}
		if command == "cd" {
			token.kind = bash.CompleteDirectories
		}
	}
	return token, true
}

func quoteSuffix(value string, quote rune) string {
	var out strings.Builder
	for _, r := range value {
		switch quote {
		case '\'':
			if r == '\'' {
				out.WriteString("'\\''")
				continue
			}
		case '"':
			if strings.ContainsRune("\\\"$`", r) {
				out.WriteByte('\\')
			}
		default:
			if !unicode.IsLetter(r) && !unicode.IsDigit(r) && !strings.ContainsRune("/_-.:", r) {
				out.WriteByte('\\')
			}
		}
		out.WriteRune(r)
	}
	return out.String()
}
