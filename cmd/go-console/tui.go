package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"strconv"
	"strings"
	"time"

	bash "github.com/adrianliechti/go-bash"
	"github.com/muesli/cancelreader"
	"golang.org/x/term"
)

// tuiCommand demonstrates a Go command taking over the console's terminal.
// It reads directory entries from cmd.FS, so even the full-screen UI sees the
// virtual filesystem. A larger application could use its preferred TUI toolkit.
func tuiCommand(ctx context.Context, cmd *bash.Command) (code int, err error) {
	if len(cmd.Args) != 1 {
		fmt.Fprintln(cmd.Stderr, "usage: tui")
		return 2, nil
	}
	in, inOK := cmd.Stdin.(interface{ Fd() uintptr })
	out, outOK := cmd.Stdout.(interface{ Fd() uintptr })
	if !inOK || !outOK || in.Fd() != os.Stdin.Fd() || out.Fd() != os.Stdout.Fd() ||
		!term.IsTerminal(int(in.Fd())) || !term.IsTerminal(int(out.Fd())) {
		fmt.Fprintln(cmd.Stderr, "tui needs the console terminal on stdin and stdout (no pipe or redirection)")
		return 2, nil
	}
	dir := strings.TrimPrefix(cmd.Cwd, "/")
	if dir == "" {
		dir = "."
	}
	entries, err := fs.ReadDir(cmd.FS, dir)
	if err != nil {
		return 1, err
	}
	state, err := term.MakeRaw(int(in.Fd()))
	if err != nil {
		return 1, err
	}
	defer func() {
		// Restore through the console itself: hitting the script output limit
		// must not prevent leaving the alternate screen or showing the cursor.
		_, screenErr := fmt.Fprint(os.Stdout, "\x1b[0m\x1b[?25h\x1b[?1049l")
		err = errors.Join(err, screenErr, term.Restore(int(in.Fd()), state))
	}()
	if _, err = fmt.Fprint(cmd.Stdout, "\x1b[?1049h\x1b[?25l"); err != nil {
		return 1, err
	}

	// The descriptor check above grants this demo exclusive use of the console
	// input. Its own cancellable reader ensures no read goroutine outlives the
	// TUI and steals keys from the next prompt, including on q, timeout, or error.
	input, err := cancelreader.NewReader(os.Stdin)
	if err != nil {
		return 1, err
	}
	tuiCtx, cancel := context.WithCancel(ctx)
	type keyEvent struct {
		key byte
		err error
	}
	keys, done := make(chan keyEvent), make(chan struct{})
	readNext := make(chan struct{})
	go func() {
		defer close(done)
		for {
			select {
			case <-readNext:
			case <-tuiCtx.Done():
				return
			}
			var b [1]byte
			_, readErr := io.ReadFull(input, b[:])
			select {
			case keys <- keyEvent{b[0], readErr}:
			case <-tuiCtx.Done():
				return
			}
			if readErr != nil {
				return
			}
		}
	}()
	defer func() { cancel(); input.Cancel(); <-done; input.Close() }()
	resize := time.NewTicker(100 * time.Millisecond)
	defer resize.Stop()
	selected, lastWidth, lastHeight, escape := 0, 0, 0, 0
	reading := false
	for {
		width, height, sizeErr := term.GetSize(int(out.Fd()))
		if sizeErr != nil {
			return 1, sizeErr
		}
		if width != lastWidth || height != lastHeight {
			if err := drawTUI(cmd.Stdout, cmd.Cwd, entries, selected, width, height); err != nil {
				return 1, err
			}
			lastWidth, lastHeight = width, height
		}
		if !reading {
			select {
			case readNext <- struct{}{}:
				reading = true
			case <-ctx.Done():
				return 1, ctx.Err()
			}
		}
		select {
		case <-ctx.Done():
			return 1, ctx.Err()
		case <-resize.C:
			continue
		case event := <-keys:
			reading = false
			if errors.Is(event.err, io.EOF) {
				return 0, nil
			}
			if event.err != nil {
				return 1, event.err
			}
			switch event.key {
			case 3:
				return 130, nil
			case 4, 'q':
				return 0, nil
			case 27:
				escape = 1
				continue
			case '[', 'O':
				if escape == 1 {
					escape = 2
					continue
				}
			case 'A':
				if escape == 2 {
					selected--
				}
			case 'B':
				if escape == 2 {
					selected++
				}
			case 'k':
				selected--
			case 'j':
				selected++
			}
			escape = 0
			selected = max(0, min(selected, len(entries)-1))
			lastWidth = 0 // redraw selection on the next iteration
		}
	}
}

func drawTUI(out io.Writer, cwd string, entries []fs.DirEntry, selected, width, height int) error {
	var screen strings.Builder
	screen.WriteString("\x1b[H\x1b[2J")
	rows := []string{"go-console TUI", "Directory: " + strconv.QuoteToASCII(cwd), "Up/Down or j/k: move   q: return   Ctrl-C: cancel", ""}
	count := max(0, height-len(rows)-1)
	start := max(0, selected-count+1)
	for i := start; i < min(len(entries), start+count); i++ {
		mark, name := "  ", strconv.QuoteToASCII(entries[i].Name())
		if entries[i].IsDir() {
			name += "/"
		}
		if i == selected {
			mark = "> "
		}
		rows = append(rows, mark+name)
	}
	for _, row := range rows[:min(len(rows), max(0, height-1))] {
		if len(row) > max(0, width-1) {
			row = row[:max(0, width-1)]
		}
		screen.WriteString(row)
		screen.WriteString("\r\n")
	}
	_, err := io.WriteString(out, screen.String())
	return err
}
