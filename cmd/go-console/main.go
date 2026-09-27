// go-console is an interactive frontend for the virtual shell. The editor and
// Go TUI commands take turns owning the host terminal; shell paths stay virtual.
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"os/signal"
	"strings"
	"time"

	bash "github.com/adrianliechti/go-bash"
	"github.com/adrianliechti/go-bash/internal/shell"
	"github.com/adrianliechti/go-bash/vfs"
	"github.com/chzyer/readline"
	"github.com/muesli/cancelreader"
	"golang.org/x/term"
	"mvdan.cc/sh/v3/syntax"
)

func main() { os.Exit(run()) }

func run() int {
	root := flag.String("root", ".", "host directory mounted at /workspace")
	readOnly := flag.Bool("readonly", false, "make /workspace read-only")
	timeout := flag.Duration("timeout", 5*time.Minute, "maximum duration per command, including TUIs")
	flag.Parse()
	if flag.NArg() != 0 || *timeout <= 0 {
		fmt.Fprintln(os.Stderr, "usage: go-console [-root DIR] [-readonly] [-timeout 5m]")
		return 2
	}
	if !term.IsTerminal(int(os.Stdin.Fd())) || !term.IsTerminal(int(os.Stdout.Fd())) {
		fmt.Fprintln(os.Stderr, "go-console needs a terminal; use go-bash for scripts and piped input")
		return 2
	}
	initial, err := term.GetState(int(os.Stdin.Fd()))
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	defer term.Restore(int(os.Stdin.Fd()), initial)
	dir, err := vfs.OpenDirectory(*root)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	defer dir.Close()
	history := &sessionHistory{}
	b, err := bash.New(context.Background(), bash.Options{
		Mounts: []bash.Mount{{Path: "/workspace", FS: dir, ReadOnly: *readOnly}},
		Cwd:    "/workspace", Timeout: *timeout,
		Commands: map[string]bash.CommandFunc{
			"clear": clearCommand, "history": history.command,
			"reset": newResetCommand(os.Stdin, os.Stdout, initial), "tui": tuiCommand,
		},
	})
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	defer b.Close(context.Background())

	// Read at most one byte so the editor cannot buffer input intended for the
	// command that takes over after Enter (including a pasted command + input).
	input, err := cancelreader.NewReader(os.Stdin)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	defer input.Close()
	editor, err := readline.NewEx(&readline.Config{
		Prompt: "$ ", Stdin: oneByteInput{input}, Stdout: os.Stdout, Stderr: os.Stderr,
		AutoComplete: completer{shell: b}, HistoryLimit: historyLimit,
		DisableAutoSaveHistory: true, EOFPrompt: "\n",
		// Job control is not implemented by the virtual shell.
		FuncFilterInputRune: func(r rune) (rune, bool) { return r, r != readline.CharCtrlZ },
	})
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	defer func() { input.Cancel(); editor.Close() }()
	fmt.Fprintln(os.Stderr, "go-console — /workspace is", *root, "(writes persist unless -readonly)")
	fmt.Fprintln(os.Stderr, "Tab completes; arrows edit/history; Ctrl-R searches; tui opens the Go TUI demo.")
	return consoleLoop(&historyEditor{historyBackend: editor, history: history}, func(script string, finish bool) (bash.Result, error) {
		return execute(b, script, finish)
	}, os.Stderr)
}

type oneByteInput struct{ io.ReadCloser }

func (r oneByteInput) Read(p []byte) (int, error) {
	return r.ReadCloser.Read(p[:min(1, len(p))])
}

type lineEditor interface {
	Readline() (string, error)
	SetPrompt(string)
	SaveHistory(string) error
}

func consoleLoop(editor lineEditor, run func(string, bool) (bash.Result, error), stderr io.Writer) int {
	var src strings.Builder
	code, failed := 0, false
	for {
		prompt := "$ "
		if src.Len() != 0 {
			prompt = "> "
		}
		editor.SetPrompt(prompt)
		line, err := editor.Readline()
		if errors.Is(err, readline.ErrInterrupt) {
			src.Reset()
			code, failed = 130, true
			continue
		}
		if errors.Is(err, io.EOF) {
			if src.Len() != 0 {
				fmt.Fprintln(stderr, "go-console: unexpected EOF in incomplete command")
				code, failed = 2, true
			}
			res, finishErr := run("", true)
			if finishErr != nil {
				fmt.Fprintln(stderr, "go-console:", finishErr)
				return 1
			}
			if failed {
				return code
			}
			return res.ExitCode
		}
		if err != nil {
			fmt.Fprintln(stderr, "go-console:", err)
			_, _ = run("", true)
			return 1
		}
		src.WriteString(line)
		src.WriteByte('\n')
		_, parseErr := shell.Parse(src.String())
		if syntax.IsIncomplete(parseErr) {
			continue
		}
		script := src.String()
		src.Reset()
		if strings.TrimSpace(script) == "" {
			continue
		}
		_ = editor.SaveHistory(strings.TrimSuffix(script, "\n"))
		res, execErr := run(script, false)
		code, failed = res.ExitCode, execErr != nil
		if execErr != nil {
			if errors.Is(execErr, context.Canceled) {
				fmt.Fprintln(stderr, "^C")
				code = 130
			} else {
				fmt.Fprintln(stderr, "go-console:", execErr)
				if code == 0 {
					code = 1
				}
			}
		}
		if res.Exited {
			return code
		}
	}
}

// Each execution gets a fresh cancellation context. Ctrl-C cancels that
// execution without poisoning the persistent shell or the next prompt.
func execute(b *bash.Shell, script string, finish bool) (bash.Result, error) {
	ctx, stopSignals := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stopSignals()
	input, err := cancelreader.NewReader(os.Stdin)
	if err != nil {
		return bash.Result{}, err
	}
	defer input.Close()
	streams := bash.IO{
		Stdin: terminalInput{input, os.Stdin.Fd()}, Stdout: os.Stdout, Stderr: os.Stderr,
		Cancel: func() { input.Cancel() },
	}
	if finish {
		return b.FinishIO(ctx, streams)
	}
	return b.RunIO(ctx, script, streams)
}

type terminalInput struct {
	io.Reader
	fd uintptr
}

func (r terminalInput) Fd() uintptr { return r.fd }
