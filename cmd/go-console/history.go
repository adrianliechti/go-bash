package main

import (
	"context"
	"fmt"
	"slices"
	"strconv"
	"strings"
	"sync"

	bash "github.com/adrianliechti/go-bash"
)

const (
	historyLimit = 500
	historyBytes = 4 << 20
)

type historyEntry struct {
	number int
	text   string
}

// sessionHistory is shared by the editor and virtual history command. Command
// handlers may run concurrently in pipelines, but never touch the live editor.
type sessionHistory struct {
	mu         sync.Mutex
	entries    []historyEntry
	next       int
	bytes      int
	generation uint64
}

func (h *sessionHistory) add(line string) {
	if strings.TrimSpace(line) == "" {
		return
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	// Match readline's treatment of consecutive identical submissions.
	if n := len(h.entries); n != 0 && h.entries[n-1].text == line {
		return
	}
	if h.next == 0 {
		h.next = 1
	}
	h.entries = append(h.entries, historyEntry{h.next, line})
	h.next++
	h.bytes += len(line)
	for len(h.entries) > historyLimit || h.bytes > historyBytes {
		h.bytes -= len(h.entries[0].text)
		h.entries[0] = historyEntry{}
		h.entries = h.entries[1:]
	}
	h.generation++
}

func (h *sessionHistory) snapshot() ([]historyEntry, uint64) {
	h.mu.Lock()
	defer h.mu.Unlock()
	return slices.Clone(h.entries), h.generation
}

func (h *sessionHistory) clear() {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.entries, h.next, h.bytes = nil, 1, 0
	h.generation++
}

func (h *sessionHistory) command(ctx context.Context, cmd *bash.Command) (int, error) {
	if err := ctx.Err(); err != nil {
		return 1, err
	}
	args := cmd.Args[1:]
	if len(args) == 1 && args[0] == "-c" {
		h.clear()
		return 0, nil
	}
	count := historyLimit
	valid := len(args) <= 1
	if len(args) == 1 {
		var err error
		count, err = strconv.Atoi(args[0])
		valid = err == nil && count >= 0
	}
	if !valid {
		_, err := fmt.Fprintln(cmd.Stderr, "usage: history [N | -c]")
		return 2, err
	}
	entries, _ := h.snapshot()
	for _, entry := range entries[max(0, len(entries)-count):] {
		if err := ctx.Err(); err != nil {
			return 1, err
		}
		if _, err := fmt.Fprintf(cmd.Stdout, "%5d  %s\n", entry.number, entry.text); err != nil {
			return 1, err
		}
	}
	return 0, nil
}

type historyBackend interface {
	lineEditor
	ResetHistory()
}

// Rebuild recall history between Readline calls. This makes history -c visible
// to arrow-key and Ctrl-R recall without mutating readline from a command's
// pipeline goroutine. Multiline submissions stay a single history entry.
type historyEditor struct {
	historyBackend
	history    *sessionHistory
	generation uint64
}

func (e *historyEditor) Readline() (string, error) {
	entries, generation := e.history.snapshot()
	if generation != e.generation {
		e.ResetHistory()
		for _, entry := range entries {
			if err := e.historyBackend.SaveHistory(entry.text); err != nil {
				return "", err
			}
		}
		e.generation = generation
	}
	return e.historyBackend.Readline()
}

func (e *historyEditor) SaveHistory(line string) error {
	e.history.add(line)
	return nil
}
