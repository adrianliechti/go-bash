package bash_test

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"strings"
	"sync"
	"testing"

	bash "github.com/adrianliechti/go-bash"
	"github.com/adrianliechti/go-bash/vfs"
)

func TestGitShellBuiltins(t *testing.T) {
	testBashDifferential(t, []bashTestCase{
		{"getopts positional clusters", `set -- -ac -b value tail; while getopts 'ab:c' opt; do printf '%s:%s:%s;' "$opt" "${OPTARG-unset}" "$OPTIND"; done; shift $((OPTIND-1)); printf '<%s>' "$@"`, "a:unset:1;c:unset:2;b:value:4;<tail>", 0},
		{"getopts explicit arguments", `set -- original; while getopts ':ab:' opt -abvalue -- -a; do printf '%s:%s:%s;' "$opt" "${OPTARG-unset}" "$OPTIND"; done; printf '%s:%s:%s' "$opt" "$OPTIND" "$1"`, "a:unset:1;b:value:2;?:3:original", 0},
		{"getopts silent errors", `getopts ':ab:' opt -z; printf '%s:%s;' "$opt" "$OPTARG"; OPTIND=1; getopts ':ab:' opt -b; printf '%s:%s;' "$opt" "$OPTARG"; getopts ':ab:' opt -b; printf '%s:%s:%s' "$?" "$opt" "${OPTARG-unset}"`, "?:z;::b;1:?:unset", 0},
		{"getopts reset cluster", `getopts ab opt -ab; printf '%s' "$opt"; OPTIND=1; getopts ab opt -ab; printf '%s' "$opt"; getopts ab opt -ab; printf '%s:%s' "$opt" "$OPTIND"`, "aab:2", 0},
		{"getopts quiet errors", `OPTERR=0; getopts ab: opt -z; printf '%s:%s;' "$opt" "${OPTARG-unset}"; OPTIND=1; getopts ab: opt -b; printf '%s:%s' "$opt" "${OPTARG-unset}"`, "?:unset;?:unset", 0},
		{"getopts local cursor", `f() { local OPTIND=1 opt; while getopts abcxy opt; do printf '%s' "$opt"; if [[ $opt == x ]]; then f -abc; fi; done; }; f -xy`, "xabcy", 0},
		{"getopts lone dash", `getopts a opt - tail; printf '%s:%s:%s' "$?" "$opt" "$OPTIND"`, "1:?:1", 0},
		{"saved descriptor", `exec 3>&1; exec >fd_saved; printf file; printf visible >&3; exec >&3 3>&-; cat fd_saved`, "visiblefile", 0},
		{"descriptor offset", `printf 'one\ntwo\nthree\n' >fd_input; exec 3<fd_input 4<&3; read -ru3 a; read -r -u 4 b; read c <&3; printf '%s:%s:%s' "$a" "$b" "$c"; exec 3<&- 4<&-`, "one:two:three", 0},
		{"descriptor scopes", `exec 3>fd_scope; { exec 4>&3; printf inner >&4; } >fd_other; (exec 3>fd_child; printf child >&3); printf outer >&3; exec 3>&-; printf more >&4; exec 4>&-; cat fd_scope fd_child`, "inneroutermorechild", 0},
		{"descriptor child inheritance", `exec 9>fd_child_script; bash -c 'printf child >&9'; exec 9>&-; cat fd_child_script`, "child", 0},
		{"descriptor command substitution", `exec 3>&1; x=$(printf visible >&3; printf captured); printf '<%s>' "$x"`, "visible<captured>", 0},
		{"descriptor pipeline", `exec 3>fd_pipe; { printf saved >&3; printf piped; } | cat; exec 3>&-; cat fd_pipe`, "pipedsaved", 0},
		{"descriptor move", `exec 3>&1; printf moved 4>&3- >&4; printf saved >&3`, "moved", 1},
		{"descriptor self move", `exec 3>&1; printf hi 3>&3-; printf after >&3`, "hiafter", 0},
		{"read closed descriptor", `exec 0<&-; read value; printf '%s' "$?"`, "1", 0},
		{"pipefail broken pipe", `set -o pipefail; yes | head -n 1; printf '%s' "$?"`, "y\n141", 0},
		{"descriptor read write", `exec 5<>fd_rw; printf abc >&5; exec 5>&-; cat fd_rw`, "abc", 0},
		{"descriptor failed redirection restores", `exec 3>&1; exec 3>fd_partial 4>absent_dir/file; printf restored >&3; cat fd_partial`, "restored", 0},
		{"exec command replaces shell", `trap 'printf wrong' EXIT; exec printf replaced; printf wrong`, "replaced", 0},
		{"exec child shell", `trap 'printf wrong' EXIT; exec bash -c 'printf child; exit 7'`, "child", 7},
		{"trap normal exit", `trap 'printf "done:%s" "$?"' EXIT; printf body; false`, "bodydone:1", 1},
		{"trap explicit exit", `trap 'printf "done:%s" "$?"; false' 0; exit 7`, "done:7", 7},
		{"trap exit overrides", `trap 'printf done; exit 9' EXIT; exit 7`, "done", 9},
		{"trap errexit", `trap 'printf "done:%s" "$?"' EXIT; set -e; false; printf wrong`, "done:1", 1},
		{"trap child shell", `bash -c 'trap '\''printf "child:%s" "$?"'\'' EXIT; exit 4'; printf ':parent:%s' "$?"`, "child:4:parent:4", 0},
		{"trap subshell reset", `trap 'printf parent' EXIT; (printf sub); x=$(printf value); printf '%s' "$x"`, "subvalueparent", 0},
		{"trap substitution own exit", `x=$(trap 'printf done' EXIT; printf value); printf '<%s>' "$x"`, "<valuedone>", 0},
		{"trap inherited listing", `trap 'printf done' EXIT; x=$(trap); printf '<%s>' "$x"`, "<trap -- 'printf done' EXIT>done", 0},
		{"trap function exit locals", `x=global; f() { local x=local; trap 'printf '%s' "$x"' EXIT; exit 4; }; f`, "local", 4},
		{"trap function errexit scope", `x=global; trap 'printf '%s' "$x"' EXIT; f() { local x=local; false; }; set -e; f`, "local", 1},
		{"trap reset and listing", `trap 'printf first' EXIT; trap -p EXIT; trap - EXIT; trap -p; trap '' 0; trap`, "trap -- 'printf first' EXIT\ntrap -- '' EXIT\n", 0},
		{"trap source and eval", `printf '%s\n' "trap 'printf cleanup' EXIT" >trap_source; . ./trap_source; eval 'printf body'; exit`, "bodycleanup", 0},
		{"source empty file", `: >empty_source; . ./empty_source; printf '%s' "$?"`, "0", 0},
		{"umask print and symbolic", `umask 027; umask; umask -S; umask -p; umask u=rwx,g=rx,o=; umask; umask g+w,o+r; umask`, "0027\nu=rwx,g=rx,o=\numask 0027\n0027\n0003\n", 0},
		{"umask inheritance and isolation", `umask 077; (umask 022; umask); bash -c umask; umask`, "0022\n0077\n0077\n", 0},
	}, 5)
}

func TestConcurrentDescriptorReads(t *testing.T) {
	b := newShell(t, bash.Options{})
	input := strings.Repeat("x", 16384)
	r, err := b.Run(t.Context(), bash.Request{Script: `exec 3<&0 4>&1; cat <&3 >&4 | cat <&3`, Stdin: input})
	if err != nil || r.Stdout != input || r.Stderr != "" || r.ExitCode != 0 {
		t.Fatalf("read %d of %d bytes, stderr %q, code %d, error %v", len(r.Stdout), len(input), r.Stderr, r.ExitCode, err)
	}
}

// A close-sensitive mount makes leaked descriptors and premature closes
// observable, including duplicates retained by a child after a parent closes.
type descriptorMount struct {
	*vfs.Memory
	mu             sync.Mutex
	active, closed int
}

func (m *descriptorMount) OpenFile(name string, flag int, perm fs.FileMode) (vfs.File, error) {
	f, err := m.Memory.OpenFile(name, flag, perm)
	if err != nil {
		return nil, err
	}
	m.mu.Lock()
	m.active++
	m.mu.Unlock()
	return &descriptorFile{File: f, mount: m}, nil
}

type descriptorFile struct {
	vfs.File
	mount  *descriptorMount
	closed bool
}

func (f *descriptorFile) Close() error {
	f.mount.mu.Lock()
	defer f.mount.mu.Unlock()
	if f.closed {
		return errors.New("descriptor closed twice")
	}
	f.closed = true
	f.mount.active--
	f.mount.closed++
	return f.File.Close()
}

func TestDescriptorLifetime(t *testing.T) {
	mount := &descriptorMount{Memory: vfs.NewMemory(1 << 20)}
	b := newShell(t, bash.Options{Mounts: []bash.Mount{{Path: "/state", FS: mount}}})
	r, err := b.Exec(t.Context(), `exec 3>/state/log 4>&3; (exec 3>&-; printf child >&4); exec 3>&-; printf parent >&4; exec 4>&-; exec 5>/state/pending`)
	if err != nil || r.ExitCode != 0 || r.Stderr != "" {
		t.Fatalf("%#v, %v", r, err)
	}
	if mount.active != 1 || mount.closed != 1 {
		t.Fatalf("before Close: %d active, %d closed", mount.active, mount.closed)
	}
	if err := b.Close(t.Context()); err != nil {
		t.Fatal(err)
	}
	if mount.active != 0 || mount.closed != 2 {
		t.Fatalf("after Close: %d active, %d closed", mount.active, mount.closed)
	}
	data, err := fs.ReadFile(mount.Memory, "log")
	if err != nil || string(data) != "childparent" {
		t.Fatalf("%q, %v", data, err)
	}
}

func TestExitTrapRunsOnce(t *testing.T) {
	b := newShell(t, bash.Options{})
	r, err := b.Exec(t.Context(), `trap 'printf first; trap "printf second" EXIT' EXIT; exit`)
	if err != nil || r.Stdout != "first" || !r.Exited {
		t.Fatalf("%#v, %v", r, err)
	}
	r, err = b.Finish(t.Context())
	if err != nil || r.Stdout != "" {
		t.Fatalf("%#v, %v", r, err)
	}
}

func TestPersistentDescriptorsAndExitTrap(t *testing.T) {
	b := newShell(t, bash.Options{})
	for _, tc := range []struct{ script, want string }{
		{`exec 3>&1; exec >session_log; trap 'printf "cleanup:%s" "$?"; printf done >&3' EXIT; printf first`, ""},
		{`printf second; printf current >&3`, "current"},
	} {
		r, err := b.Exec(t.Context(), tc.script)
		if err != nil || r.Stdout != tc.want || r.Stderr != "" || r.ExitCode != 0 || r.Exited {
			t.Fatalf("%#v, %v", r, err)
		}
	}
	r, err := b.Finish(t.Context())
	if err != nil || r.Stdout != "done" || r.ExitCode != 0 || !r.Exited {
		t.Fatalf("finish: %#v, %v", r, err)
	}
	data, err := b.ReadFile(t.Context(), "session_log")
	if err != nil || string(data) != "firstsecondcleanup:0" {
		t.Fatalf("log: %q, %v", data, err)
	}
	r, err = b.Finish(t.Context())
	if err != nil || r.Stdout != "" {
		t.Fatalf("second finish: %#v, %v", r, err)
	}
}

func TestCloseRunsExitCleanup(t *testing.T) {
	mem := vfs.NewMemory(1 << 20)
	b := newShell(t, bash.Options{Mounts: []bash.Mount{{Path: "/state", FS: mem}}})
	r, err := b.Exec(t.Context(), `trap 'printf cleanup >/state/result' EXIT`)
	if err != nil || r.ExitCode != 0 {
		t.Fatalf("%#v, %v", r, err)
	}
	if err := b.Close(t.Context()); err != nil {
		t.Fatal(err)
	}
	data, err := fs.ReadFile(mem, "result")
	if err != nil || string(data) != "cleanup" {
		t.Fatalf("%q, %v", data, err)
	}
}

func TestVirtualUmask(t *testing.T) {
	mem := vfs.NewMemory(1 << 20)
	b := newShell(t, bash.Options{Mounts: []bash.Mount{{Path: "/state", FS: mem}}, Commands: map[string]bash.CommandFunc{
		"create": func(ctx context.Context, cmd *bash.Command) (int, error) {
			f, err := cmd.FS.OpenFile("state/handler", os.O_CREATE|os.O_WRONLY, 0666)
			if err != nil {
				return 1, err
			}
			if err := f.Close(); err != nil {
				return 1, err
			}
			return 0, cmd.FS.Mkdir("state/handler_dir", 0777)
		},
	}})
	r, err := b.Exec(t.Context(), `umask 077; : >/state/redirect; touch /state/touch; mkdir /state/dir; create; (umask 022; : >/state/child); : >/state/parent; umask 000; : >/state/redirect`)
	if err != nil || r.ExitCode != 0 || r.Stderr != "" {
		t.Fatalf("%#v, %v", r, err)
	}
	for name, want := range map[string]fs.FileMode{"redirect": 0600, "touch": 0600, "dir": 0700, "handler": 0600, "handler_dir": 0700, "child": 0644, "parent": 0600} {
		info, err := fs.Stat(mem, name)
		if err != nil {
			t.Fatal(err)
		}
		if info.Mode().Perm() != want {
			t.Errorf("%s: %04o, want %04o", name, info.Mode().Perm(), want)
		}
	}
}

func TestTrapAndDescriptorLimits(t *testing.T) {
	for _, script := range []string{`trap 'while true; do :; done' EXIT; exit`, `exec 3>&1; trap 'while true; do printf x >&3; done' EXIT; exit`} {
		b := newShell(t, bash.Options{MaxSteps: 40})
		_, err := b.Exec(t.Context(), script)
		if !errors.Is(err, bash.ErrExecutionLimit) {
			t.Fatalf("%s: %v", script, err)
		}
	}
	b := newShell(t, bash.Options{MaxOutputBytes: 8})
	if _, err := b.Exec(t.Context(), `exec 3>&1`); err != nil {
		t.Fatal(err)
	}
	_, err := b.Exec(t.Context(), `printf '123456789' >&3`)
	if !errors.Is(err, bash.ErrOutputLimit) {
		t.Fatal(fmt.Errorf("saved stdout lost output limit: %w", err))
	}
}
