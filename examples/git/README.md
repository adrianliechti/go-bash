# A virtual git command

Registers `git` using go-bash's `Options.Commands` and
[`go-git`](https://github.com/adrianliechti/go-git), a sandboxed git CLI built
on [go-git/go-git](https://github.com/go-git/go-git). Git runs in-process
against the shell filesystem (`Command.FS`). It never reads host files or the
host environment, opens network connections, or starts processes. The global
config is `$HOME/.gitconfig` inside the shell.

This example has its own Go module so go-bash users do not need the git
dependency. It requires a sibling checkout at `../go-git` relative to the
go-bash repository; the `replace` directives in `go.mod` use the local sources.

```sh
cd examples/git
go run .
go run . -c 'git init -q -b main; echo hi > a; git add a; git status -s'
```

`gitCommand` forwards arguments, cwd, exported variables, and streams to
`git.Run`. A small `shellFS` wrapper adapts `vfs.WriteFS` to `git.FS`, which
differ only in `OpenFile`'s result type.

## Compatibility test

`main_test.go` runs each scenario twice: in go-bash with this command, and in
the host's `/bin/sh` with the real `git`. Each line is echoed with its merged
output and exit status, and the transcripts must be identical, including
commit hashes, diffs, and error messages. The scenarios mix git with the
shell's WASM coreutils (`seq`, `sed`, `grep`, `sort`, `cut`, `head`). The test
is skipped when no `git` is on `PATH`.

```sh
go test ./...
```

The go-git repository has a broader suite covering more commands. See its
README for supported commands and known differences, for example no remotes,
fast-forward-only `merge`, and no rename detection.
