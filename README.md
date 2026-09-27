# go-bash

A virtual Bash-style shell for Go with **uutils coreutils, grep, find, diff, sed, and ripgrep
running in WebAssembly**. Package: `bash`. CLI: `cmd/go-bash`.
Interactive console example: [`cmd/go-console`](cmd/go-console).

The shell creates its own filesystem layout. Mount ordinary Go `fs.FS` values
read-only, or use `vfs.WriteFS` for read-write access. Mounts do not need to be at
`/`; the CLI mounts the current host directory at `/workspace`.

```text
/
├── bin/          read-only registered commands
├── usr/bin/      the same commands
├── tmp/          writable, in-memory scratch space
├── work/         default working directory for the Go API
├── dev/null
└── workspace/    CLI's host-directory mount; writes persist
```

`cat`, `/bin/cat`, and `/usr/bin/cat` invoke the embedded WASM command.
`PATH` defaults to `/usr/bin:/bin`. The command files expose the actual embedded
WASM bytes, shared in memory, with mode `0555`. Custom Go commands expose empty
files with the same mode. These directories are reserved; commands cannot be
replaced. Executable guest files with a sh/bash shebang run through the virtual
shell; arbitrary native and WASM executables are not run.

## Try it

Requires Go 1.26 or newer. The checked-in WASM artifact means ordinary Go builds
need neither Rust nor a system-installed shell or coreutils.

```sh
go run ./cmd/go-bash
go run ./cmd/go-bash -c 'pwd; ls /bin'
go run ./cmd/go-bash -c 'printf "pear\napple\npear\n" | sort | uniq > result.txt; cat result.txt'
go run ./cmd/go-bash -root ./some-directory -readonly -c 'grep -rn TODO . | head -n 5'
go run ./cmd/go-bash -root ./some-directory -readonly -c 'rg -n TODO -g "*.go"'
go run ./cmd/go-bash -c 'find . -name "*.txt" | xargs sed -i "s/pear/plum/"; diff -u result.txt /dev/null'
go build ./cmd/go-bash
```

The third example **writes `result.txt` in your current host directory**. The
interactive prompt is intentionally basic; output is captured until each script
finishes. Piped input without `-c` is treated as a script; with `-c` it becomes
the command's stdin. Use `-timeout 30s` to change the per-script deadline.

For an editable prompt, history, Tab completion, and live command I/O:

```sh
go run ./cmd/go-console
go run ./cmd/go-console -root ./some-directory -readonly
```

The console uses `$ ` as its prompt. Arrow keys edit and browse session history;
Ctrl-R searches history. Tab completes commands, functions, variables, and paths
in the virtual filesystem. `clear` clears the visible screen while preserving
scrollback, and `reset` restores the console's startup terminal modes and ANSI
display state. `history`, `history N`, and `history -c` list or clear session
history, including editor recall. Enter `tui` for a full-screen Go directory browser;
use Up/Down and `q` to return to the prompt. Ctrl-C cancels the current input or
command, and Ctrl-D at an empty prompt exits and runs EXIT cleanup. The console
requires a terminal and defaults to a five-minute execution deadline, adjustable
with `-timeout`. See the [console example](cmd/go-console/README.md) for details.

## Go API

```go
package main

import (
    "context"
    "fmt"

    bash "github.com/adrianliechti/go-bash"
    "github.com/adrianliechti/go-bash/vfs"
)

func main() {
    ctx := context.Background()
    work := vfs.NewMemory(16 << 20)
    sh, err := bash.New(ctx, bash.Options{
        Mounts: []bash.Mount{{Path: "/workspace", FS: work}},
        Cwd: "/workspace",
    })
    if err != nil { panic(err) }
    defer sh.Close(ctx)

    result, err := sh.Exec(ctx, `echo hello > greeting.txt; cat greeting.txt`)
    if err != nil { panic(err) }
    fmt.Print(result.Stdout)
    // Check result.ExitCode for command failure (distinct from a Go error).
}
```

For write-through access to a host directory, replace `work` with
`vfs.OpenDirectory("./project")` and defer its `Close`. It uses `os.Root` to
confine filesystem operations to that directory. The caller owns mounts; close
the shell before closing its backends.

Plain `fs.FS` values, including `embed.FS`, can be mounted at any non-overlapping
absolute directory such as `/data`. They are read-only. `Mount.ReadOnly` also
turns a writable backend into a read-only mount. Root and overlapping mounts
are currently rejected.

`fs.FS` has no write operations. The extension is deliberately small:

```go
type File interface {
    fs.File
    io.Writer
}

type WriteFS interface {
    fs.FS
    OpenFile(name string, flag int, perm fs.FileMode) (File, error)
    Mkdir(name string, perm fs.FileMode) error
    Remove(name string) error
    Rename(oldName, newName string) error
}
```

Names passed to backends use `io/fs` conventions: relative paths and `.` for the
mount root. Seek, positioned I/O, truncate, timestamps, and symlink inspection
are optional capabilities. `vfs.Memory` and `vfs.Directory` implement the common
operations. There is no automatic copy-on-write overlay.

Sessions retain files, variables, functions, options, descriptors, and cwd between calls. Calls on one
shell are serialized. `Run(ctx, Request{Script: ..., Stdin: ...})` supplies stdin;
`ReadFile` retrieves a guest file. `Result.Exited` tells interactive callers that
the script terminated the current shell through `exit`, `errexit`, or a fatal
expansion/assignment error; the API session remains reusable. `Finish(ctx)` supplies
EOF and returns any pending EXIT-trap output and final status. `Close(ctx)` also
runs pending cleanup, discarding its output, before releasing resources. The CLI
calls `Finish` at EOF and uses `$ ` as its default primary prompt (`PS1`).

`RunIO(ctx, script, IO{Stdin: reader, Stdout: writer, Stderr: writer})` connects
live streams instead of capturing output in `Result`. `FinishIO` does the same
for EOF cleanup. Nil input supplies EOF; nil output discards writes. The caller
owns these streams and must unblock pending I/O when the execution context ends;
the optional `IO.Cancel` callback handles cancellation, timeouts, and output-limit
failures. The console shows cancellable terminal input. Execution and output limits still
apply. Stream wrappers forward `Fd()` when available (otherwise `^uintptr(0)`),
letting trusted Go commands detect a terminal and manage raw mode. Pipelines and
virtual files supply their redirected streams as usual. This does not provide
a PTY, job control, or terminal device support inside WASM.

`Complete(ctx, prefix, kind)` provides a read-only completion source for editors.
Kinds are `CompleteCommands`, `CompleteFiles`, `CompleteDirectories`, and
`CompleteVariables`. Prefixes and results are literal, unquoted names; directory
results end in `/`, and variable names omit `$`. Completion follows current
virtual cwd/PATH and includes shell functions and custom commands. It does not
evaluate shell text, run commands, or change `$?`.

### Custom Go commands

Register virtual commands with `Options.Commands`. The shell resolves them
through `PATH`, `/bin`, or `/usr/bin` and supplies expanded arguments, exported
environment variables, cwd, and redirected streams:

```go
sh, err := bash.New(ctx, bash.Options{
    Commands: map[string]bash.CommandFunc{
        "hello": func(ctx context.Context, cmd *bash.Command) (int, error) {
            _, err := fmt.Fprintln(cmd.Stdout, "hello from Go")
            return 0, err
        },
    },
})
// After checking err and arranging sh.Close(ctx):
result, err := sh.Exec(ctx, "hello | tr a-z A-Z")
```

[`Command`](command.go) also provides a live `vfs.WriteFS` view rooted at the
shell's `/`. Names use `io/fs` conventions, for example `work/file.txt`.
Handlers can read with `fs.ReadFile` and mutate files through `OpenFile`,
`Mkdir`, `Rename`, and `Remove`. `OpenFile` accepts the usual `os.O_*` flags.
Writes follow the shell's mount permissions and filesystem limits; plain
`fs.FS` mounts and mounts marked `ReadOnly` remain read-only.

Return a nonzero status with a nil error for a command failure; return an error
to abort execution. Handlers must support concurrent pipeline invocations,
respect context cancellation, and finish their I/O before returning. They must
not call methods on the executing shell, which holds its session lock. The
caller owns handler resources and closes them after closing the shell.

Custom commands cannot replace embedded commands or builtins. `New` copies the
registration map; `ls /bin` includes the session's custom commands, while
`bash.Commands()` lists only the embedded inventory.

[`examples/python`](examples/python) registers virtual `python` and `python3`
commands using [go-pyodide](https://github.com/adrianliechti/go-pyodide). It shows
pipelines, script files, arguments, exit codes, and access to shell files. Its
separate Go module uses the sibling `../go-pyodide` checkout, keeping CPython
out of ordinary go-bash builds. Python reads shell files through its read-only
mount API; shell redirection saves its output to writable mounts.

[`examples/git`](examples/git) registers a virtual `git` backed by
[go-git](https://github.com/adrianliechti/go-git), which implements common git
commands in Go against the shell filesystem, with no host files, network, or
child processes. Its test runs the same scripts in go-bash and with the host's
real git, and requires identical output, including commit hashes. It uses the
sibling `../go-git` checkout.

## Compatibility

The compatibility target is **noninteractive Bash 5.3 scripting with GNU-style
utilities**, within an explicitly mounted virtual filesystem. This is a supported
subset, not full Bash or a complete POSIX/GNU conformance claim. Shell syntax and
expansion come from `mvdan.cc/sh/v3`;
execution is our own Go implementation, not its host-executing interpreter.
The command implementations come from
[coreutils 0.12.0](https://github.com/uutils/coreutils/tree/0.12.0),
[grep 0.2.0](https://github.com/uutils/grep/tree/0.2.0),
[findutils 0.10.0](https://github.com/uutils/findutils/tree/0.10.0),
[diffutils v0.5.0](https://github.com/uutils/diffutils/tree/v0.5.0),
[sed 0.2.0](https://github.com/uutils/sed/tree/0.2.0), and
[ripgrep 15.2.0](https://github.com/BurntSushi/ripgrep/tree/15.2.0). grep and sed
are early upstream releases; see [the artifact notes](internal/wasm/README.md) for the
local patches applied to diffutils, sed, and ripgrep.

Supported shell features include quoting, variables and exports, parameter and
arithmetic expansion, command substitution, globs, pipelines, `&&`/`||`, ordinary
redirects, heredocs, here strings, blocks, subshells, functions, `if`, `case`,
word and arithmetic `for` loops, and `while`/`until`. `case` supports `;;`, `;&`,
and `;;&`, with quoted patterns treated literally.

`[[ ... ]]` supports short-circuit `&&`/`||`, negation and grouping, strings,
glob comparisons (`=`, `==`, `!=`), lexical comparisons (`<`, `>`), arithmetic
comparisons (`-eq`, `-ne`, `-lt`, `-le`, `-gt`, `-ge`), `-z`/`-n`, `-v`, and
`-o` checks for `errexit`, `nounset`, `xtrace`, and `pipefail`. File predicates
`-e`/`-a`, `-f`, `-d`, `-s`, `-L`/`-h`, `-nt`,
and `-ot` operate on the virtual filesystem. Regex matching (`=~`), ownership,
permission, and other predicates remain unsupported and are rejected before
the script runs. Negative extended patterns (`!(...)`) are not supported.

Builtins include `cd`, `pwd`, `export`, `unset`, `exit`, `return`, `xargs`, `set`,
`shift`, `read`, `local`, `declare`, `typeset`, `readonly`, `command`, `type`,
`which`, `source`/`.`, `eval`, `trap`, `exec`, `getopts`, `umask`, and single-level
loop control.

`set -euo pipefail` and separate or combined `-e`, `-u`, and `-x` options work.
The corresponding named options are `errexit`, `nounset`, and `xtrace`; use `+`
to disable them. `-e` stops on an unhandled failure, with exceptions for tested
conditions, nonfinal `&&`/`||` commands, and negation. `-u` rejects unset parameter
expansions while allowing defaults such as `${name:-fallback}`. `-x` traces
expanded simple commands and assignments to stderr. `pipefail` makes a pipeline
return its rightmost nonzero status instead of the final command's status. `set -o` lists
option states; `set +o` prints commands to restore them. `set -- ...` and
`shift [N]` manage positional arguments. Options and arguments persist between
API calls and are copied into subshells. Command substitutions clear `-e`, as
Bash does by default.

`local` creates function-scoped variables with Bash-style dynamic scope: called
functions see the caller's locals, and values and attributes are restored on
return. `declare` and `typeset` create locals inside functions and globals
otherwise; `-g` explicitly selects the global binding. Scalar declarations
support `-r` (readonly), `-x`/`+x` (export/unexport), and `-p` (inspect).
`readonly` prevents reassignment and unsetting, and `export -n` removes export
status. Array, integer, and nameref declaration flags remain unsupported.

`command -v`/`-V` and `type` identify functions, builtins, keywords, and PATH
commands; `type -t`, `-p`, `-P`, `-a`, and `-f` are supported. `which [-a]`
searches PATH for executable files. All lookup uses the virtual filesystem.
`command NAME ...` skips functions, and `command -p` uses `/usr/bin:/bin`.

`source FILE [ARGS ...]` and `. FILE [ARGS ...]` parse and run a guest file in
the current shell, retaining variable, function, option, and cwd changes.
Sourcing searches PATH, then cwd, and does not require execute bits. Supplied
arguments temporarily replace positional arguments; an explicit `set --` in the
sourced file retains the new arguments. `return` ends the sourced file. `eval [ARG ...]` joins its arguments and parses them in the current shell.
Both share the caller's execution and recursion limits.

`./script.sh` and scripts found through PATH require executable file modes and a
sh/bash shebang, including `#!/usr/bin/env bash` and `#!/usr/bin/env -S bash -eu`.
They run in a fresh virtual child shell with exported variables, cwd, stdin,
and arguments. `sh FILE`/`bash FILE` also run readable scripts without execute
bits or a shebang; `sh -c SCRIPT [NAME [ARGS ...]]`, `bash -c`, and scripts on
stdin are supported. Child shells start with default options unless explicitly
passed options such as `-eu`; they use this same supported language subset.

`read [-r] [-d DELIM] [-u FD] [NAME ...]` reads stdin (or the selected descriptor)
without consuming subsequent lines,
supports IFS splitting and backslash continuations, and assigns `REPLY` when no
names are supplied. `-r` preserves backslashes and `-d ''` reads NUL-delimited
records. At EOF it assigns any partial line and returns status 1. For example:

```sh
set -o pipefail
for ((i=0; i<3; i++)); do printf '%s\n' "$i"; done |
  while IFS= read -r line; do
    case "$line" in
      0) printf 'zero\n' ;;
      *) [[ "$line" -gt 0 ]] && printf 'positive: %s\n' "$line" ;;
    esac
  done
```

Descriptors 0 through 9 support input, output, append, read/write (`<>`),
duplication (`>&`/`<&`), and closing (`>&-`/`<&-`). Bare `exec` makes its
redirections persistent, including across API calls. `exec COMMAND ...` runs a
virtual executable and ends the current shell; its EXIT trap is skipped after a
successful replacement. No host process is launched.

`trap 'COMMANDS' EXIT` (or `0`) installs cleanup for normal child-script completion,
`exit`, and shell errors such as `set -e` failures. The handler sees the original
`$?`; its ordinary commands preserve the exit status, while `exit N` overrides it.
`trap - EXIT` removes the handler; `trap` and `trap -p EXIT` print it. Subshells
reset execution of inherited handlers. Persistent API sessions keep their trap
until exit, `Finish`, or `Close`. Cleanup shares script limits; an embedding error
such as cancellation or an output limit is propagated rather than hidden.

`getopts OPTSTRING NAME [ARGS ...]` supports clustered options, required arguments,
`--`, `OPTIND`, `OPTARG`, `OPTERR`, and leading-colon silent errors. Set `OPTIND=1`
to restart parsing; function-local `OPTIND` supports nested parsers.

`umask` starts at `0022`, accepts octal and symbolic modes, and supports `-S`/`-p`
printing. It applies to new files and directories made by redirections, WASM
utilities, and custom command filesystem calls. Child shells inherit it; changes
inside a subshell stay there. The host process umask is never changed.

Not yet supported: job control/background jobs, process substitution, arrays,
descriptors above 9 or `{fd}` allocation, signal/ERR/DEBUG/RETURN traps, aliases,
shell options beyond the four above,
or arbitrary native or WASM executables. Bare variable names in arithmetic
still use the expansion library's zero-default behavior, including under `-u`.
Unsupported syntax returns an error; unsupported builtin options return a
nonzero status.

The pinned coreutils `feat_wasm` build contains 79 utilities; `coreutils --list`
prints them. The separate modules add `grep`, `find`, `diff`, `cmp`, `sed`, and `rg`.
`bash.Commands()` lists the embedded commands and `bash.Versions()` the pinned
releases. Not all native coreutils are available in the WASI build (for
example, `chmod` and `stat` are absent), and `awk` is absent because the uutils
implementation has no release yet.

`rg` supports recursive searches, ignore rules, globs, `--files`, `--json`, and
piped input. Its WASI build always runs on one thread, including when `-j` is
supplied. PCRE2 (`-P`) is not compiled in, and subprocess-based features such
as `--pre` and compressed-file searches (`-z`) are unavailable. go-bash supplies
stdin state so `rg pattern` searches cwd by default and reads stdin when piped
or redirected. Use `rg pattern -` to explicitly select even an empty stdin.

`xargs` is a shell builtin rather than the findutils binary, because WASI
cannot spawn processes: it supports `-0`, `-n N`, `-I R`, `-r`, `-t`, and `--`,
and runs the assembled command lines through the shell's own lookup with an
empty stdin. For the same reason `find`'s `-exec`, `-execdir`, and `-ok` print
an error for each match yet still exit 0; pipe into `xargs` instead. `grep -r`
and `sed -i` work on any writable mount.

Important filesystem/WASI limitations:

- The WASM `/usr/bin/pwd` currently fails to resolve the CLI's mounted
  host-directory cwd. Use the shell's built-in `pwd`, which works correctly.
- WASI does not expose POSIX permission bits or uid/gid. In upstream uutils,
  `test -x` always returns false; `test -w` is not a reliable mount-writability
  check. Actual writes are enforced by the Go mount layer. `ls /bin` and
  `test -f /bin/cat` work, as does `/bin/cat file`.
- Creating hard links and symlinks is not implemented in the bridge. Existing
  relative symlinks in directory mounts work within that mount; absolute or
  escaping links are rejected by `os.Root`. Guest paths are cleaned lexically,
  so `symlink/..` is not full POSIX physical-path traversal.
- Rename is limited to one mount. Cross-mount `mv` currently fails; use `cp`
  followed by `rm`. Wazero's experimental errno set cannot express `EXDEV` or
  `ENOSPC` precisely, so some diagnostics use a generic error.
- Directory mounts inherit `os.Root.Rename`'s refusal to replace an existing
  directory. Memory mounts support POSIX empty-directory replacement. File-handle
  timestamp updates use the handle's inode, not its original pathname; Unix
  directory backends currently use microsecond precision for these updates.
- Optional operations depend on the backend; command availability does not
  imply every flag works. Memory filesystem modes are metadata, not a multi-user
  permission system. The virtual umask filters creation modes, but no ownership
  model is implemented; host directory permissions can restrict them further.

## Boundaries and limits

The built-in runtime has no host process execution, host environment inheritance,
or network API. Only explicitly mounted backends are exposed. WASM commands get
fresh instances, a fixed environment, real time, and a cryptographic random source.
Custom command handlers are trusted Go code with the caller's host privileges;
their runtimes and resource limits are the caller's responsibility. Captured
output still uses the shell's output limit, and handlers receive its deadline.

Defaults: 10 seconds per execution, 1 MiB combined stdout/stderr, 1 MiB script
and stdin limits, 10,000 shell execution steps, and 128 MiB linear memory per
WASM command. The default memory filesystem has a 64 MiB content budget;
each `vfs.NewMemory` has its own budget and a 10,000-entry cap. Limits can be
configured in `Options` where exposed. File changes are **not rolled back** on
failure, output limits, or cancellation.

This is not an audited security boundary for hostile multitenant workloads.
Per-command memory limits are not a total Go-process memory limit; pipelines
can run several instances. Backends are trusted Go code and must be safe for
concurrent use. A blocking backend operation cannot necessarily be interrupted
by a context deadline. Directory mounts have no disk quota and may expose
pre-existing hard links, devices, FIFOs, or other special files: use a dedicated
regular-file directory or memory backend for untrusted workloads, and apply
process-level resource limits as needed. Read-only directory access may still
affect host access times. Do not mount more host data than the script needs.

## Development

```sh
go test ./...
go test -race ./...
go vet -stdmethods=false ./...
```

Tests exercise actual uutils WASM, persistent writes, filesystem conformance,
command discovery, pipelines, read-only mounts, host-symlink confinement,
same-file copy protection, quotas, and cancellation. The hard-test suite includes
fixed Bash differential cases, 3,600 randomized filesystem operations (with
documented Go/POSIX differences excluded), open-inode lifetime checks, writeback
failure injection, queued deadlines, concurrent session calls, and integer-limit
boundaries. Fixed fixtures may run against an installed Bash in temporary
directories; generated scripts never run on the host. The
[Bash 5.3 upstream fixtures](testdata/bash-5.3/README.md) port selected GNU Bash
tests with source attribution and pinned stdout, stderr, and exit status.
All 42 selected groups pass, including the seven formerly recorded gaps.
Set `BASH_TEST_BINARY=/path/to/bash-5.3` to check
expectations against a specific Bash 5.3 build. Pinned checks still run when the
system Bash is older (for example, macOS's Bash 3.2) or unavailable.

The fuzz targets cover parsing, virtual execution, and memory-filesystem state
transitions. Crashing inputs are retained in `testdata/fuzz` and replay during
ordinary tests. For longer local runs:

```sh
go test ./internal/shell -run '^$' -fuzz '^FuzzParse$' -fuzztime=30s
go test ./internal/shell -run '^$' -fuzz '^FuzzExecution$' -fuzztime=30s
go test ./vfs -run '^$' -fuzz '^FuzzMemoryTransitions$' -fuzztime=30s
```

The `stdmethods` vet check is disabled because wazero's required
`Seek(int64, int) (int64, sys.Errno)` signature intentionally differs from
`io.Seeker`. Other vet analyzers remain enabled.

See [the artifact build notes](internal/wasm/README.md) for pinned sources,
toolchain, checksums, the working-directory constructor, and local patches.
