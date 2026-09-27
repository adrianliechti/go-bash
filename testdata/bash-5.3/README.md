# GNU Bash 5.3 regression fixtures

These 20 fixture groups port selected tests from 11 files in the GNU Bash 5.3.0
release. They cover `case` matching and fallthrough, arithmetic `for` loops,
`[[ ]]` logic and arithmetic, `read` splitting and delimiters, here-strings,
here-documents, and pipeline status inversion.

The source is the official [bash-5.3.tar.gz release archive](https://ftp.gnu.org/gnu/bash/bash-5.3.tar.gz):

```text
SHA-256: 0d5cd86965f869a26cf64f4b71be7b96f90a3ba8b3d74e27e8e9d9d5550f31ba
```

[manifest.json](manifest.json) records each upstream filename and the original,
one-based source line ranges. `invert.bash`, `read-ifs-whitespace.bash`, and
`read-ifs-assignment.bash` are complete, unchanged upstream files. The other
scripts concatenate the listed excerpts, retaining the upstream license header
and adding a provenance comment. The excerpts were selected on 2026-09-27;
commands within the selected ranges are unchanged.

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

## Known compatibility gaps

Thirteen fixture groups currently pass. Seven expose existing differences,
recorded by `known_failure` in the manifest. They still execute: a mismatch is
reported as a skip in the default suite, and a matching result is reported as a
pass. The Bash oracle must match the expected results even for known failures.

Use strict mode to turn every remaining difference into a test failure:

```sh
BASH_TEST_STRICT=1 go test -run '^TestBash53Upstream$' -v .
# Run just one reproduction while implementing its fix:
BASH_TEST_STRICT=1 go test -run '^TestBash53Upstream/conditional-negation$' -v .
```

| Fixture | Difference observed in go-bash |
| --- | --- |
| `arithmetic-for-quoted` | A quoted arithmetic condition evaluates to zero instead of evaluating its contents. |
| `case-escaped-word` | A backslash in an unquoted `case` word survives quote removal. |
| `conditional-negation` | `[[ ! x \|\| x ]]` negates the full expression instead of its first term. |
| `read-escaped-trailing-space` | `read` retains the escaped trailing IFS space in the upstream multiword example. |
| `read-ifs-whitespace` | `read` leaves trailing delimiters when IFS contains tab, carriage return, form feed, and vertical tab. |
| `read-delimiter-short-consumer` | An early-closing `read -d` consumer can produce spurious `echo: I/O error` diagnostics. |
| `here-document-delimiter-continuation` | Backslash-newline folding in a delimiter consumes subsequent script text. |

After a fix, run its fixture in strict mode and remove its `known_failure`
annotation. Keep the Bash expectations unchanged. Executor fixes belong in the
separate shell implementation work; this port adds fixtures and a runner.

## Scope and further ports

The full upstream suite also covers arrays, regular expressions and
`BASH_REMATCH`, shell options, terminal and job control, extended file
descriptors, locale-specific behavior, and exact diagnostic formatting. Those
areas remain outside this initial selection. Driver invocations of `${THIS_SH}`
and external test helpers such as `recho` are omitted. Function-printing checks
(`type`/`typeset -f`) are omitted from otherwise selected scripts. The manifest
makes these excerpt boundaries explicit.

To add coverage, copy a reviewed upstream file or a self-contained range, record
its provenance, and obtain its expected result from Bash 5.3 in the same isolated
environment. Add a separate fixture for an unsupported case and explain any
`known_failure`; never adjust a Bash expectation to match go-bash behavior.
