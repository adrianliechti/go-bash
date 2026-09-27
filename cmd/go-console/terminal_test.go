//go:build darwin || linux

package main

import (
	"bytes"
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"os/exec"
	"reflect"
	"testing"
	"time"

	bash "github.com/adrianliechti/go-bash"
	"github.com/creack/pty"
	"golang.org/x/term"
)

func TestConsoleProcess(t *testing.T) {
	if os.Getenv("GO_CONSOLE_PTY_TEST") != "1" {
		return
	}
	os.Args = []string{"go-console", "-root", os.Getenv("GO_CONSOLE_TEST_ROOT"), "-readonly", "-timeout", "2s"}
	flag.CommandLine = flag.NewFlagSet("go-console", flag.ExitOnError)
	os.Exit(run())
}

func TestConsoleTerminal(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), 60*time.Second)
	defer cancel()
	root := t.TempDir()
	for _, name := range []string{"two words", "café", "a$(bad)"} {
		if err := os.WriteFile(root+"/"+name, []byte("file:"+name+"\n"), 0600); err != nil {
			t.Fatal(err)
		}
	}
	cmd := exec.CommandContext(ctx, os.Args[0], "-test.run=^TestConsoleProcess$")
	cmd.Env = append(os.Environ(), "GO_CONSOLE_PTY_TEST=1", "GO_CONSOLE_TEST_ROOT="+root, "TERM=xterm-256color")
	terminal, err := pty.StartWithSize(cmd, &pty.Winsize{Rows: 24, Cols: 100})
	if err != nil {
		t.Fatal(err)
	}
	defer terminal.Close()
	wait := make(chan error, 1)
	go func() { wait <- cmd.Wait() }()
	defer func() {
		cancel()
		select {
		case <-wait:
		case <-time.After(5 * time.Second):
		}
	}()
	chunks := make(chan []byte, 16)
	go func() {
		defer close(chunks)
		for {
			data := make([]byte, 4096)
			n, err := terminal.Read(data)
			if n > 0 {
				select {
				case chunks <- data[:n]:
				case <-ctx.Done():
					return
				}
			}
			if err != nil {
				return
			}
		}
	}()
	var pending []byte
	expect := func(text string) {
		t.Helper()
		// Compiling the embedded coreutils module under -race takes several
		// seconds before the first prompt, especially alongside other packages.
		deadline := time.NewTimer(20 * time.Second)
		defer deadline.Stop()
		for {
			if at := bytes.Index(pending, []byte(text)); at >= 0 {
				pending = pending[at+len(text):]
				return
			}
			select {
			case data, ok := <-chunks:
				if !ok {
					t.Fatalf("console closed before %q; output %q", text, pending)
				}
				pending = append(pending, data...)
			case <-deadline.C:
				t.Fatalf("waiting for %q; output %q", text, pending)
			}
		}
	}
	send := func(text string) {
		t.Helper()
		if _, err := io.WriteString(terminal, text); err != nil {
			t.Fatal(err)
		}
	}
	const prompt = "$  \b"
	expect(prompt)
	command := func(src, output string) {
		t.Helper()
		send(src + "\r")
		expect(output)
		expect(prompt)
	}
	command("pw\t", "\r\n/workspace\r\n")
	command("clea\t", "\x1b[H\x1b[2J")
	command("clear extra", "usage: clear")
	command("clear > /work/screen; printf 'clear:%s\\n' \"$(wc -c < /work/screen)\"", "\r\nclear:7\r\n")
	command("res\t", resetScreen)
	command("reset | cat", "reset needs the console terminal")
	command("reset < /dev/null", "reset needs the console terminal")
	command("reset > /work/reset; printf 'reset-bytes:%s\\n' \"$(wc -c < /work/reset)\"", "\r\nreset-bytes:0\r\n")
	for _, name := range []string{"two words", "café", "a$(bad)"} {
		prefix := string([]rune(name)[:1])
		command("cat "+prefix+"\t", "\r\nfile:"+name+"\r\n")
	}
	// Cursor movement and history run the edited/previous command.
	command("echo cursor-OKX\x1b[D\x1b[3~", "\r\ncursor-OK\r\n")
	command("\x1b[A", "\r\ncursor-OK\r\n")
	// Clearing history also clears the editor's Up/Down and search history.
	send("history -c\r")
	expect(prompt)
	command("\x1b[Aprintf 'hist:%s\\n' fresh", "\r\nhist:fresh\r\n")
	command("history", "    2  history\r\n")
	command("history 1", "    3  history 1\r\n")
	command("history 1 | cat", "    4  history 1 | cat\r\n")
	command("history 1 > /work/history; cat /work/history", "    5  history 1 > /work/history; cat /work/history\r\n")
	command("history -1", "usage: history")
	// Wait for a live readiness message before replying to the command.
	send("printf 'ready:%s\\n' read; read value; printf 'read:%s\\n' \"$value\"\r")
	expect("\r\nready:read\r\n")
	send("live-input\n")
	expect("\r\nread:live-input\r\n")
	expect(prompt)
	// Full-screen Go command, resize, and return to the editor.
	send("tui\r")
	expect("go-console TUI")
	if err := pty.Setsize(terminal, &pty.Winsize{Rows: 30, Cols: 90}); err != nil {
		t.Fatal(err)
	}
	expect("go-console TUI")
	send("\x1b[B")
	expect("> \"caf\\u00e9\"")
	send("q")
	expect("\x1b[?1049l")
	expect(prompt)
	command("printf 'after:%s\\n' tui", "\r\nafter:tui\r\n")
	command("tui | cat", "tui needs the console terminal")
	command("tui < /dev/null", "tui needs the console terminal")
	// Cancellation while blocked in stdin must not leave an input goroutine.
	send("printf 'ready:%s\\n' cancel; read value\r")
	expect("\r\nready:cancel\r\n")
	send("\x03")
	expect("^C")
	expect(prompt)
	command("printf 'after:%s\\n' cancel", "\r\nafter:cancel\r\n")
	// A timeout also restores the alternate screen and terminal modes.
	send("tui\r")
	expect("go-console TUI")
	expect("\x1b[?1049l")
	expect("context deadline exceeded")
	expect(prompt)
	command("printf 'after:%s\\n' timeout", "\r\nafter:timeout\r\n")
	send("trap 'printf \"cleanup:%s\\n\" exit' EXIT\r")
	expect(prompt)
	send("\x04")
	expect("cleanup:exit\r\n")
	select {
	case err := <-wait:
		if err != nil {
			t.Fatal(fmt.Errorf("console exit: %w", err))
		}
		// Allow the deferred cleanup to observe completion without another wait.
		wait <- nil
	case <-ctx.Done():
		t.Fatal(ctx.Err())
	}
}

func TestResetTerminalModes(t *testing.T) {
	master, terminal, err := pty.Open()
	if err != nil {
		t.Fatal(err)
	}
	defer master.Close()
	defer terminal.Close()
	initial, err := term.GetState(int(terminal.Fd()))
	if err != nil {
		t.Fatal(err)
	}
	defer term.Restore(int(terminal.Fd()), initial)
	reset := newResetCommand(terminal, terminal, initial)
	if _, err := term.MakeRaw(int(terminal.Fd())); err != nil {
		t.Fatal(err)
	}
	damaged, err := term.GetState(int(terminal.Fd()))
	if err != nil || reflect.DeepEqual(damaged, initial) {
		t.Fatalf("did not change terminal modes: %v", err)
	}
	var stderr bytes.Buffer
	cmd := &bash.Command{Args: []string{"reset"}, Stdin: terminal, Stdout: &bytes.Buffer{}, Stderr: &stderr}
	if code, err := reset(t.Context(), cmd); code != 2 || err != nil {
		t.Fatalf("redirected reset: %d %v", code, err)
	}
	unchanged, _ := term.GetState(int(terminal.Fd()))
	if !reflect.DeepEqual(unchanged, damaged) {
		t.Fatal("redirected reset changed terminal modes")
	}
	cmd.Stdout = terminal
	cmd.Args = []string{"reset", "extra"}
	if code, err := reset(t.Context(), cmd); code != 2 || err != nil {
		t.Fatalf("invalid args: %d %v", code, err)
	}
	cmd.Args = []string{"reset"}
	if code, err := reset(t.Context(), cmd); code != 0 || err != nil {
		t.Fatalf("reset: %d %v", code, err)
	}
	restored, err := term.GetState(int(terminal.Fd()))
	if err != nil || !reflect.DeepEqual(restored, initial) {
		t.Fatalf("did not restore terminal modes: %v", err)
	}
	// Avoid an unbounded read if a regression stops writing terminal recovery.
	received := make(chan string, 1)
	go func() {
		data := make([]byte, len(resetScreen))
		_, _ = io.ReadFull(master, data)
		received <- string(data)
	}()
	select {
	case got := <-received:
		if got != resetScreen {
			t.Fatalf("recovery sequences: %q", got)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("reset did not write terminal recovery sequences")
	}
	// A failed output stream must not prevent restoring terminal input modes.
	if _, err := term.MakeRaw(int(terminal.Fd())); err != nil {
		t.Fatal(err)
	}
	wantErr := errors.New("terminal write failed")
	cmd.Stdout = failedTerminalWriter{terminal, wantErr}
	if code, err := reset(t.Context(), cmd); code != 1 || !errors.Is(err, wantErr) {
		t.Fatalf("write error: %d %v", code, err)
	}
	restored, _ = term.GetState(int(terminal.Fd()))
	if !reflect.DeepEqual(restored, initial) {
		t.Fatal("output error prevented restoring terminal modes")
	}
}

type failedTerminalWriter struct {
	file *os.File
	err  error
}

func (w failedTerminalWriter) Fd() uintptr               { return w.file.Fd() }
func (w failedTerminalWriter) Write([]byte) (int, error) { return 0, w.err }
