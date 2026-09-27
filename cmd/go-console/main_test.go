package main

import (
	"bytes"
	"context"
	"io"
	"slices"
	"testing"

	bash "github.com/adrianliechti/go-bash"
	"github.com/chzyer/readline"
)

type lineResult struct {
	line string
	err  error
}
type fakeEditor struct {
	input            []lineResult
	prompts, history []string
}

func (e *fakeEditor) Readline() (string, error) {
	if len(e.input) == 0 {
		return "", io.EOF
	}
	r := e.input[0]
	e.input = e.input[1:]
	return r.line, r.err
}
func (e *fakeEditor) SetPrompt(prompt string)       { e.prompts = append(e.prompts, prompt) }
func (e *fakeEditor) SaveHistory(line string) error { e.history = append(e.history, line); return nil }

func TestConsoleLoopMultilineAndInterrupt(t *testing.T) {
	ed := &fakeEditor{input: []lineResult{{"if true; then", nil}, {"echo ok", nil}, {"fi", nil}, {"if true; then", nil}, {"", readline.ErrInterrupt}, {"echo next", nil}}}
	var scripts []string
	var stderr bytes.Buffer
	code := consoleLoop(ed, func(script string, finish bool) (bash.Result, error) {
		if finish {
			scripts = append(scripts, "EOF")
			return bash.Result{ExitCode: 7}, nil
		}
		scripts = append(scripts, script)
		return bash.Result{}, nil
	}, &stderr)
	if code != 7 || stderr.Len() != 0 || !slices.Equal(scripts, []string{"if true; then\necho ok\nfi\n", "echo next\n", "EOF"}) {
		t.Fatalf("%d %q %q", code, scripts, &stderr)
	}
	if !slices.Equal(ed.prompts, []string{"$ ", "> ", "> ", "$ ", "> ", "$ ", "$ "}) {
		t.Fatalf("prompts: %q", ed.prompts)
	}
	if !slices.Equal(ed.history, []string{"if true; then\necho ok\nfi", "echo next"}) {
		t.Fatalf("history: %q", ed.history)
	}
}

func TestConsoleLoopExitAndErrors(t *testing.T) {
	for _, tc := range []struct {
		name        string
		lines       []lineResult
		result      bash.Result
		err         error
		want, calls int
	}{
		{"exit", []lineResult{{"exit 9", nil}}, bash.Result{ExitCode: 9, Exited: true}, nil, 9, 1},
		{"execution canceled", []lineResult{{"sleep 10", nil}}, bash.Result{}, context.Canceled, 130, 2},
		{"interrupt then EOF", []lineResult{{"", readline.ErrInterrupt}}, bash.Result{}, nil, 130, 1},
		{"incomplete EOF", []lineResult{{"if true; then", nil}}, bash.Result{}, nil, 2, 1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			calls := 0
			ed := &fakeEditor{input: tc.lines}
			code := consoleLoop(ed, func(_ string, finish bool) (bash.Result, error) {
				calls++
				if finish {
					return bash.Result{}, nil
				}
				return tc.result, tc.err
			}, io.Discard)
			if code != tc.want || calls != tc.calls {
				t.Fatalf("code %d / calls %d", code, calls)
			}
		})
	}
}
