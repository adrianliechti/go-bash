package main

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"slices"
	"strings"
	"sync"
	"testing"

	bash "github.com/adrianliechti/go-bash"
)

func TestHistoryCommand(t *testing.T) {
	h := &sessionHistory{}
	h.add("echo first")
	h.add("if true; then\n  echo second\nfi")
	h.add("history")
	for _, tc := range []struct {
		args []string
		want string
		code int
	}{
		{nil, "    1  echo first\n    2  if true; then\n  echo second\nfi\n    3  history\n", 0},
		{[]string{"1"}, "    3  history\n", 0},
		{[]string{"0"}, "", 0},
		{[]string{"-1"}, "", 2},
		{[]string{"abc"}, "", 2},
		{[]string{"999999999999999999999999"}, "", 2},
		{[]string{"-c", "1"}, "", 2},
		{[]string{"1", "2"}, "", 2},
	} {
		var out, stderr bytes.Buffer
		code, err := h.command(t.Context(), &bash.Command{Args: append([]string{"history"}, tc.args...), Stdout: &out, Stderr: &stderr})
		if err != nil || code != tc.code || out.String() != tc.want || (code == 2) != strings.Contains(stderr.String(), "usage: history") {
			t.Errorf("history %q: code %d, %v, output %q / %q", tc.args, code, err, &out, &stderr)
		}
	}
	if entries, _ := h.snapshot(); len(entries) != 3 {
		t.Fatal("listing or invalid arguments changed history")
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if _, err := h.command(ctx, &bash.Command{Args: []string{"history", "-c"}}); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancelled clear: %v", err)
	}
	if entries, _ := h.snapshot(); len(entries) != 3 {
		t.Fatal("cancelled clear changed history")
	}
	if code, err := h.command(t.Context(), &bash.Command{Args: []string{"history", "-c"}}); code != 0 || err != nil {
		t.Fatalf("clear: %d, %v", code, err)
	}
	if entries, _ := h.snapshot(); len(entries) != 0 {
		t.Fatalf("clear retained %v", entries)
	}
	h.add("echo fresh")
	if entries, _ := h.snapshot(); len(entries) != 1 || entries[0].number != 1 {
		t.Fatalf("clear did not reset numbering: %v", entries)
	}
}

func TestHistoryBoundsAndSnapshots(t *testing.T) {
	h := &sessionHistory{}
	h.add(" \t")
	for i := range historyLimit + 3 {
		h.add(fmt.Sprintf("echo %d", i))
	}
	h.add(fmt.Sprintf("echo %d", historyLimit+2))
	entries, generation := h.snapshot()
	if len(entries) != historyLimit || entries[0].number != 4 || entries[len(entries)-1].number != historyLimit+3 {
		t.Fatalf("retention and duplicate handling: %v", entries)
	}
	entries[0].text = "changed snapshot"
	if again, _ := h.snapshot(); again[0].text != "echo 3" {
		t.Fatal("snapshot aliases history")
	}
	h.clear()
	large := strings.Repeat("x", historyBytes/2)
	for i := range 3 {
		h.add(large + fmt.Sprint(i))
	}
	entries, nextGeneration := h.snapshot()
	if len(entries) != 1 || entries[0].number != 3 || h.bytes > historyBytes || nextGeneration <= generation {
		t.Fatalf("byte limit: %d entries, %d bytes, generation %d", len(entries), h.bytes, nextGeneration)
	}
}

type historyTestEditor struct {
	fakeEditor
	resets int
}

func (e *historyTestEditor) ResetHistory() { e.history = nil; e.resets++ }

func TestHistoryEditorSynchronization(t *testing.T) {
	h := &sessionHistory{}
	backend := &historyTestEditor{}
	ed := &historyEditor{historyBackend: backend, history: h}
	line := "if true; then\n  echo ok\nfi"
	if err := ed.SaveHistory(line); err != nil {
		t.Fatal(err)
	}
	if len(backend.history) != 0 {
		t.Fatal("changed editor history during command execution")
	}
	_, _ = ed.Readline()
	if !slices.Equal(backend.history, []string{line}) {
		t.Fatalf("multiline recall: %q", backend.history)
	}
	_, _ = ed.Readline()
	if backend.resets != 1 {
		t.Fatal("rebuilt unchanged history")
	}
	h.clear()
	_, _ = ed.Readline()
	if len(backend.history) != 0 || backend.resets != 2 {
		t.Fatal("clear did not clear recall history")
	}
}

func TestHistoryRecordsCurrentSubmission(t *testing.T) {
	h := &sessionHistory{}
	backend := &historyTestEditor{fakeEditor: fakeEditor{input: []lineResult{{line: "echo hello"}, {line: "history"}}}}
	ed := &historyEditor{historyBackend: backend, history: h}
	var out bytes.Buffer
	code := consoleLoop(ed, func(script string, finish bool) (bash.Result, error) {
		if script == "history\n" {
			code, err := h.command(t.Context(), &bash.Command{Args: []string{"history"}, Stdout: &out})
			return bash.Result{ExitCode: code}, err
		}
		return bash.Result{}, nil
	}, io.Discard)
	if code != 0 || out.String() != "    1  echo hello\n    2  history\n" {
		t.Fatalf("%d %q", code, &out)
	}
}

func TestHistoryConcurrentCommands(t *testing.T) {
	h := &sessionHistory{}
	var wg sync.WaitGroup
	for worker := range 4 {
		wg.Go(func() {
			for i := range 100 {
				h.add(fmt.Sprintf("echo %d %d", worker, i))
				args := []string{"history", "10"}
				if i%13 == 0 {
					args[1] = "-c"
				}
				if code, err := h.command(t.Context(), &bash.Command{Args: args, Stdout: io.Discard, Stderr: io.Discard}); code != 0 || err != nil {
					t.Errorf("concurrent history: %d %v", code, err)
				}
			}
		})
	}
	wg.Wait()
}
