package main

import (
	"context"
	"slices"
	"strings"
	"testing"

	bash "github.com/adrianliechti/go-bash"
)

type completionLookup struct {
	prefix string
	kind   bash.CompletionKind
	names  []string
}

func (l *completionLookup) Complete(_ context.Context, prefix string, kind bash.CompletionKind) ([]string, error) {
	l.prefix, l.kind = prefix, kind
	return l.names, nil
}

func TestConsoleCompletion(t *testing.T) {
	for _, tc := range []struct {
		line, prefix string
		kind         bash.CompletionKind
		name, want   string
	}{
		{"pw", "pw", bash.CompleteCommands, "pwd", "pwd "},
		{"X=y pw", "pw", bash.CompleteCommands, "pwd", "X=y pwd "},
		{"true && pw", "pw", bash.CompleteCommands, "pwd", "true && pwd "},
		{"cd di", "di", bash.CompleteDirectories, "dir/", "cd dir/"},
		{"cat > fi", "fi", bash.CompleteFiles, "file", "cat > file "},
		{"2>/dev/null pw", "pw", bash.CompleteCommands, "pwd", "2>/dev/null pwd "},
		{"cat two\\ w", "two w", bash.CompleteFiles, "two words", "cat two\\ words "},
		{"cat 'two w", "two w", bash.CompleteFiles, "two words", "cat 'two words' "},
		{`cat "two w`, "two w", bash.CompleteFiles, `two words$(bad)`, `cat "two words\$(bad)" `},
		{`cat "back\s`, `back\s`, bash.CompleteFiles, `back\slash`, `cat "back\slash" `},
		{"cat 'don", "don", bash.CompleteFiles, "don't", "cat 'don'\\''t' "},
		{"cat caf", "caf", bash.CompleteFiles, "café", "cat café "},
		{"cat café", "café", bash.CompleteFiles, "café.txt", "cat café.txt "},
		{"echo $PA", "PA", bash.CompleteVariables, "PATH", "echo $PATH"},
		{"echo ${PA", "PA", bash.CompleteVariables, "PATH", "echo ${PATH}"},
		{`cat a`, "a", bash.CompleteFiles, "a;$(bad)", `cat a\;\$\(bad\) `},
	} {
		t.Run(tc.line, func(t *testing.T) {
			lookup := &completionLookup{names: []string{tc.name}}
			comps, _ := (completer{lookup}).Do([]rune(tc.line), len([]rune(tc.line)))
			if lookup.prefix != tc.prefix || lookup.kind != tc.kind {
				t.Fatalf("lookup %q/%d, want %q/%d", lookup.prefix, lookup.kind, tc.prefix, tc.kind)
			}
			if len(comps) != 1 || tc.line+string(comps[0]) != tc.want {
				t.Fatalf("completion: %q, want %q", comps, tc.want)
			}
		})
	}
	for _, line := range []string{"echo $(touch marker)", "echo `pwd`", "cat << EOF", "# comment", "cat $PATH/fi", "cat *", "cat trailing\\"} {
		lookup := &completionLookup{names: []string{"unexpected"}}
		if comps, _ := (completer{lookup}).Do([]rune(line), len([]rune(line))); len(comps) != 0 {
			t.Errorf("complex input %q: %q", line, comps)
		}
	}
	lookup := &completionLookup{names: []string{"bad\x1b[2J", "bad\nname"}}
	if comps, _ := (completer{lookup}).Do([]rune("cat bad"), 7); len(comps) != 0 {
		t.Fatalf("unsafe display: %q", comps)
	}
}

func TestCompletionQuotingRoundTrip(t *testing.T) {
	b, err := bash.New(t.Context(), bash.Options{})
	if err != nil {
		t.Fatal(err)
	}
	defer b.Close(context.Background())
	for _, name := range []string{"two words", "it's fine", "café", "dollar$(echo unsafe)", "semi;colon", `back\slash`, "a*b?[c]", "!bang"} {
		for _, quote := range []rune{0, '\'', '"'} {
			word := quoteSuffix(name, quote)
			if quote != 0 {
				word = string(quote) + word + string(quote)
			}
			r, err := b.Exec(t.Context(), "printf '%s\\n' "+word)
			if err != nil || r.ExitCode != 0 || r.Stdout != name+"\n" {
				t.Errorf("%q: %+v %v; want %q", word, r, err, name)
			}
		}
	}
}

func TestCompletionRetainsFollowingText(t *testing.T) {
	l := &completionLookup{names: []string{"file"}}
	line := "cat fi | cat"
	got, offset := (completer{l}).Do([]rune(line), 6)
	if len(got) != 1 || !slices.Equal(got[0], []rune("le ")) || offset != len("fi") {
		t.Fatalf("%q %d", got, offset)
	}
	if !strings.HasSuffix(line[:6]+string(got[0])+line[6:], " | cat") {
		t.Fatal("lost following text")
	}
}
