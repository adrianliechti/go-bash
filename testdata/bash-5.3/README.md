# GNU Bash 5.3 regression fixtures

These 42 fixture groups port selected tests from 31 primary files in the GNU
Bash 5.3.0 release, plus seven unchanged helper scripts in `support/`. They cover
`case`, arithmetic loops, `[[ ]]`, `read`, heredocs, pipeline status, shell options,
function declarations and scope, command discovery, source/eval, empty positional
arguments, `getopts`, `umask`, EXIT traps, and persistent descriptor redirections.
All 42 groups pass; the seven gaps recorded by the initial port are fixed.

The source is the official [bash-5.3.tar.gz release archive](https://ftp.gnu.org/gnu/bash/bash-5.3.tar.gz):

```text
SHA-256: 0d5cd86965f869a26cf64f4b71be7b96f90a3ba8b3d74e27e8e9d9d5550f31ba
```

[manifest.json](manifest.json) records each upstream filename and the original,
one-based source line ranges. Entries marked “complete file” are unchanged.
Excerpts retain any upstream license header and add a provenance comment. The
excerpts were selected on 2026-09-27; commands within the selected ranges are
unchanged. `support_files` lists complete upstream scripts used by a fixture.
The harness supplies `THIS_SH` as the selected native Bash binary or the virtual
`bash` builtin, allowing upstream child-shell invocations to run unmodified.

The scripts retain GNU Bash's GPL-3.0-or-later licensing. Upstream's
[COPYRIGHT](COPYRIGHT) notice and [COPYING](COPYING) license are included here.

## Running the fixtures

From the repository root:

```sh
go test -run '^TestBash53Upstream$' -v .
```

Each fixture checks exact stdout, stderr, and exit status against the checked-in
expectations. The `.stdout` files were recorded with GNU Bash
`5.3.0(1)-release` and reviewed; `invert.stdout` also matches upstream's
`invert.right` byte for byte. Expected stderr and status are in the manifest.
Tests never regenerate expectations from go-bash.

If Bash 5.3 is on `PATH`, the test also verifies every expectation against that
binary. Select a particular build with:

```sh
BASH_TEST_BINARY=/absolute/path/to/bash-5.3 go test -run '^TestBash53Upstream$' -v .
```

An explicitly selected binary must be Bash 5.3.x. With an older system Bash or no
Bash installed, the go-bash checks still run against the pinned expectations.

The Go harness shares compiled WASM modules across fixtures. Each script runs in
a subshell with its own working directory, so variables, functions, shell
options, and relative files are isolated. Native Bash runs in a fresh temporary
directory with startup files disabled and a fixed environment: `LC_ALL=C`,
`TZ=UTC`, `HOME=/work`, `TMPDIR=.`, and `PATH=/usr/bin:/bin`. go-bash uses the same
locale, timezone, home, and temporary-directory settings with its virtual PATH.
Only the reviewed, checked-in scripts are executed on the host.

## Coverage and boundaries

The original seven failing groups now pass without `known_failure` annotations:
quoted arithmetic loop expressions, escaped case words, conditional negation,
two `read` whitespace cases, early-closing pipeline consumers, and continued
heredoc delimiters. Neighboring cases also have differential Go tests.

The added groups exercise `set -e` contexts and child scripts, `set -x` assignments,
`set -u` with empty arguments, function scope restoration, scalar readonly and
`declare -g`, `command -v`/`-V`/`-p`, source arguments/return, eval expansion,
getopts cursors and nested functions, numeric/symbolic umask, EXIT trap status,
and descriptor duplication/closing with persistent exec redirections.

This is a selected suite. Arrays, terminal/job control, signals, regex matching,
full POSIX mode, descriptors above 9, and exact error-message formatting remain
outside these excerpts. The source fixture omits `cp /dev/null`, which the current
WASI coreutils build rejects; an independent Go regression covers sourcing an
empty file. Complex mixtures of empty expansions and quoted `$@` also remain
outside the selected direct-empty-argument cases. Shebang dispatch, virtual PATH,
combined/named shell options, readonly errors, and resource limits have separate
Go regression tests because their setup or diagnostics depend on this runtime.

To add coverage, copy a reviewed upstream file or self-contained range, record
its provenance, and obtain expected results from Bash 5.3 in the same isolated
environment. Tests never regenerate expectations from go-bash. The harness still
supports explicit `known_failure` annotations for future ports; none are used
now. `BASH_TEST_STRICT=1` prevents any annotated mismatch from becoming a skip.
