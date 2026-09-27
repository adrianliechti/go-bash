package main

import (
	"context"
	"fmt"
	"io"
	"os"

	bash "github.com/adrianliechti/go-bash"
	"golang.org/x/term"
)

// clearCommand clears the visible ANSI terminal screen, preserving scrollback.
// Use the command's output so redirection, pipes, and output limits still work.
func clearCommand(_ context.Context, cmd *bash.Command) (int, error) {
	if len(cmd.Args) != 1 {
		_, err := fmt.Fprintln(cmd.Stderr, "usage: clear")
		return 2, err
	}
	_, err := io.WriteString(cmd.Stdout, "\x1b[H\x1b[2J")
	if err != nil {
		return 1, err
	}
	return 0, nil
}

// Leave alternate-screen and application input modes, restore normal display
// attributes, cursor visibility, scrolling margins and character set, and clear
// the visible screen. These are the ANSI modes used by this console and its TUIs.
const resetScreen = "\x18\x1b[?1049l\x1b[?2004l\x1b[?1000l\x1b[?1002l\x1b[?1003l\x1b[?1006l\x1b[?1l\x1b>\x1b[0m\x1b[?25h\x1b[?7h\x1b[r\x1b(B\x1b[H\x1b[2J"

// reset restores the terminal settings captured before the first prompt. It
// deliberately uses the command's streams and requires the console terminal,
// so a redirected invocation cannot silently modify the user's terminal.
func newResetCommand(input, output *os.File, initial *term.State) bash.CommandFunc {
	return func(ctx context.Context, cmd *bash.Command) (int, error) {
		if len(cmd.Args) != 1 {
			_, err := fmt.Fprintln(cmd.Stderr, "usage: reset")
			return 2, err
		}
		if err := ctx.Err(); err != nil {
			return 1, err
		}
		in, inOK := cmd.Stdin.(interface{ Fd() uintptr })
		out, outOK := cmd.Stdout.(interface{ Fd() uintptr })
		if !inOK || !outOK || in.Fd() != input.Fd() || out.Fd() != output.Fd() ||
			!term.IsTerminal(int(in.Fd())) || !term.IsTerminal(int(out.Fd())) {
			_, err := fmt.Fprintln(cmd.Stderr, "reset needs the console terminal on stdin and stdout (no pipe or redirection)")
			return 2, err
		}
		if err := term.Restore(int(input.Fd()), initial); err != nil {
			return 1, err
		}
		if _, err := io.WriteString(cmd.Stdout, resetScreen); err != nil {
			return 1, err
		}
		return 0, nil
	}
}
