package bash_test

import (
	"errors"
	"strings"
	"testing"
	"testing/fstest"

	bash "github.com/adrianliechti/go-bash"
)

func TestBashScriptOptions(t *testing.T) {
	testBashDifferential(t, []bashTestCase{
		{"strict preamble", `set -euo pipefail; printf '%s' "${missing:-ok}"`, "ok", 0},
		{"errexit", `set -e; false; printf wrong`, "", 1},
		{"errexit final and", `set -e; true && false; printf wrong`, "", 1},
		{"errexit final or", `set -e; false || false; printf wrong`, "", 1},
		{"errexit conditions", `set -e; false && printf wrong; false || printf or; if false; then :; fi; while false; do :; done; ! true; printf ok`, "orok", 0},
		{"errexit compound conditions", `set -e; f() { false; printf f; }; if f; then printf yes; fi; ! { false; printf block; }; printf ok`, "fyesblockok", 0},
		{"errexit ignored function", `set -e; f() { false; printf f; }; f && printf yes; printf ok`, "fyesok", 0},
		{"errexit function", `set -e; f() { false; printf wrong; }; f; printf wrong`, "", 1},
		{"errexit compound list", `set -e; { false && true; }; if true; then false && true; fi; printf ok`, "ok", 0},
		{"negated redirect failure", `set -e; ! : > absent_directory/file; printf '%s' "$?"`, "0", 0},
		{"errexit subshell", `set -e; (false && true); printf wrong`, "", 1},
		{"errexit pipeline default", `set -e; false | true; printf ok`, "ok", 0},
		{"errexit pipefail", `set -eo pipefail; false | true; printf wrong`, "", 1},
		{"errexit substitution", `set -e; x=$(false; printf ok); printf '%s' "$x"`, "ok", 0},
		{"errexit substitution status", `set -e; x=$(false); printf wrong`, "", 1},
		{"nounset default and arguments", `set -u; set --; printf '<%s>' "${missing-default}" "${1-fallback}" "$@"`, "<default><fallback>", 0},
		{"nounset assignment default", `set -u; printf '%s:%s' "${missing:=value}" "$missing"`, "value:value", 0},
		{"nounset substitution isolation", `set -u; x=$(printf '%s' "$missing"); printf '%s:after' "$?"`, "1:after", 0},
		{"named options", `set -o errexit -o nounset; [[ -o errexit && -o nounset ]]; set +eu; [[ ! -o errexit && ! -o nounset ]]; printf ok`, "ok", 0},
		{"option positional arguments", `set -eu -- a 'b c'; printf '%s:<%s>:<%s>' "$#" "$1" "$2"`, "2:<a>:<b c>", 0},
		{"nested shell flags", `bash -eu -c 'printf "%s:%s" "$0" "$1"' nested arg`, "nested:arg", 0},
	}, 3)
	// Bash 3.2 reports different nounset statuses and ignores some compound
	// redirection failures under -e. Keep modern expectations on older hosts.
	testBashDifferential(t, []bashTestCase{
		{"errexit compound redirect", `set -e; { :; } > absent_directory/file; printf wrong`, "", 1},
		{"nounset", `set -u; printf '%s' "$missing"; printf wrong`, "", 1},
		{"nounset positional", `set -u; set --; printf '%s' "$1"`, "", 1},
		{"nounset redirection", `set -u; : > "$missing"; printf wrong`, "", 1},
	}, 4)
}

func TestBashScriptDeclarations(t *testing.T) {
	testBashDifferential(t, []bashTestCase{
		{"local restoration", `x=global; f() { local x=local fresh=new; printf '%s:%s:' "$x" "$fresh"; }; f; printf '%s:%s' "$x" "${fresh-unset}"`, "local:new:global:unset", 0},
		{"dynamic scope", `x=global; g() { x=changed; }; f() { local x=local; g; printf '%s:' "$x"; }; f; printf '%s' "$x"`, "changed:global", 0},
		{"nested scopes", `x=global; g() { local x=inner; printf '%s:' "$x"; }; f() { local x=outer; g; printf '%s:' "$x"; }; f; printf '%s' "$x"`, "inner:outer:global", 0},
		{"local expansion before shadow", `x=global; f() { local x=local y=$x; printf '%s:%s' "$x" "$y"; }; f`, "local:global", 0},
		{"local bare and repeated", `x=global; f() { local x; printf '%s:' "${x-unset}"; x=value; local x; printf '%s' "$x"; }; f`, "unset:value", 0},
		{"declare and typeset", `declare x=global; f() { declare x=local; typeset y=typed; printf '%s:%s:' "$x" "$y"; }; f; printf '%s:%s' "$x" "${y-unset}"`, "local:typed:global:unset", 0},
		{"local return restoration", `x=global; f() { local x=local; return 7; }; f; printf '%s:%s' "$?" "$x"`, "7:global", 0},
		{"local outside function", `local x=wrong; printf '%s:%s' "$?" "${x-unset}"`, "1:unset", 0},
		{"local export", `export x=global; f() { local x=local; bash -c 'printf "%s:" "$x"'; }; f; bash -c 'printf "%s" "$x"'`, "local:global", 0},
		{"readonly assignment", `readonly x=fixed; x=changed; printf wrong`, "", 1},
		{"readonly unset", `readonly x=fixed; unset x; printf '%s:%s' "$?" "$x"`, "1:fixed", 0},
		{"readonly redeclaration", `readonly x=fixed; declare x=changed; printf '%s:%s' "$?" "$x"`, "1:fixed", 0},
		{"local readonly", `x=global; f() { local -r x=local; unset x; printf '%s:%s:' "$?" "$x"; }; f; x=changed; printf '%s' "$x"`, "1:local:changed", 0},
		{"declaration flags", `declare -rx x=value; declare -p x`, "declare -rx x=\"value\"\n", 0},
		{"dynamic declaration literal", `arg='x=$HOME'; declare "$arg"; printf '%s' "$x"`, "$HOME", 0},
		{"declaration substitution status", `set -e; f() { local x=$(false); printf ok; }; f`, "ok", 0},
		{"remove export", `export x=value; export -n x; bash -c 'printf "%s" "${x-unset}"'`, "unset", 0},
	}, 3)
	testBashDifferential(t, []bashTestCase{
		{"readonly read", `readonly x=fixed; read x <<< changed; printf '%s:%s' "$?" "$x"`, "1:fixed", 0},
		{"global declaration through locals", `x=global; g() { local x=inner; declare -g x=changed; printf '%s:' "$x"; }; f() { local x=outer; g; printf '%s:' "$x"; }; f; printf '%s' "$x"`, "inner:outer:changed", 0},
	}, 4)
}

func TestBashScriptEvaluation(t *testing.T) {
	testBashDifferential(t, []bashTestCase{
		{"eval state", `eval 'x=changed; f() { printf "%s" "$x"; }'; f`, "changed", 0},
		{"eval joined arguments", `eval printf "'%s'" "'two words'"`, "two words", 0},
		{"eval return", `f() { local x=value; eval 'printf "%s" "$x"; return 7'; printf wrong; }; f`, "value", 7},
		{"eval loop control", `for x in a b; do eval 'printf "%s" "$x"; break'; done`, "a", 0},
		{"eval condition", `set -e; eval 'false; printf ok' || printf wrong`, "ok", 0},
		{"eval strict failure", `set -e; eval 'false; printf wrong'; printf wrong`, "", 1},
		{"source current shell", `printf '%s\n' 'x=value; cd /; f() { printf "%s" "$x"; }' > sourced; source ./sourced; f; printf ':%s' "$PWD"`, "value:/", 0},
		{"source args restored", `printf '%s\n' 'printf "%s:%s:" "$1" "$2"; shift; return 3; printf wrong' > sourced_args; set -- outer; . ./sourced_args first second; printf '%s:%s' "$?" "$1"`, "first:second:3:outer", 0},
		{"source inherits args", `printf '%s\n' 'printf "%s:" "$1"; shift' > sourced_inherit; set -- first second; source ./sourced_inherit; printf '%s' "$1"`, "first:second", 0},
		{"source local declaration", `printf '%s\n' 'local x=inside' > sourced_local; x=global; f() { . ./sourced_local; printf '%s:' "$x"; }; f; printf '%s' "$x"`, "inside:global", 0},
		{"source cwd fallback", `printf 'x=found' > sourced_cwd; source sourced_cwd; printf '%s' "$x"`, "found", 0},
		{"source missing status", `. ./missing_source; printf '%s' "$?"`, "1", 0},
		{"bash script file", `printf '%s\n' 'printf "%s:%s" "$0" "$1"; exit 4' > child_script; bash child_script argument`, "child_script:argument", 4},
		{"bash stdin", `printf 'printf child' | bash`, "child", 0},
		{"command probe", `f() { :; }; command -v f cd; command -v nonexistent_script_tool; printf '%s' "$?"`, "f\ncd\n1", 0},
		{"type classification", `f() { :; }; type -t f cd cat if; type -p cd`, "function\nbuiltin\nfile\nkeyword\n", 0},
		{"command bypass function", `cat() { printf wrong; }; printf right | command cat`, "right", 0},
		{"command builtin", `command export x=exported; bash -c 'printf "%s" "$x"'`, "exported", 0},
	}, 3)
}

func TestGuestScriptExecutionAndLookup(t *testing.T) {
	files := fstest.MapFS{
		"script.sh":   {Mode: 0555, Data: []byte("#!/bin/bash\nprintf '%s:%s:%s:%s' \"$0\" \"$1\" \"$shared\" \"${private-unset}\"; shared=child; cd /; exit 7\n")},
		"env.sh":      {Mode: 0555, Data: []byte("#!/usr/bin/env bash\nprintf env\n")},
		"strict.sh":   {Mode: 0555, Data: []byte("#!/usr/bin/env -S bash -eu\nfalse; printf wrong\n")},
		"options.sh":  {Mode: 0555, Data: []byte("#!/bin/sh -e\nfalse; printf wrong\n")},
		"readable.sh": {Mode: 0444, Data: []byte("#!/bin/sh\nprintf readable\n")},
		"native":      {Mode: 0555, Data: []byte("\x7fELF")},
		"unknown":     {Mode: 0555, Data: []byte("#!/usr/bin/python\nprint('wrong')\n")},
		"config":      {Mode: 0444, Data: []byte("configured=yes")},
	}
	b := newShell(t, bash.Options{Mounts: []bash.Mount{{Path: "/scripts", FS: files}}})
	for _, tc := range []struct {
		script, want string
		code         int
	}{
		{`cd /scripts; export shared=parent; private=secret; ./script.sh argument; printf ':%s:%s:%s' "$?" "$shared" "$PWD"`, "./script.sh:argument:parent:unset:7:parent:/scripts", 0},
		{`PATH=/scripts:/usr/bin:/bin; script.sh via-path`, "/scripts/script.sh:via-path:parent:unset", 7},
		{`command -v script.sh; type -P script.sh; which script.sh`, "/scripts/script.sh\n/scripts/script.sh\n/scripts/script.sh\n", 0},
		{`./env.sh`, "env", 0},
		{`./strict.sh`, "", 1},
		{`./options.sh`, "", 1},
		{`./readable.sh`, "", 126},
		{`bash ./readable.sh`, "readable", 0},
		{`./native`, "", 126},
		{`./unknown`, "", 126},
		{`./absent`, "", 127},
		{`source config; printf '%s' "$configured"`, "yes", 0},
		{`PATH=/missing; command -v cat`, "", 1},
		{`command -p -v cat; command -p cat /scripts/config`, "/usr/bin/cat\nconfigured=yes", 0},
		{`PATH=/scripts; command -p script.sh`, "", 127},
	} {
		t.Run(tc.script, func(t *testing.T) {
			r, err := b.Exec(t.Context(), tc.script)
			if err != nil || r.Stdout != tc.want || r.ExitCode != tc.code || r.Exited {
				t.Fatalf("got %#v, %v; want %q/%d", r, err, tc.want, tc.code)
			}
		})
	}
}

func TestScriptOptionsPersistAndTrace(t *testing.T) {
	b := newShell(t, bash.Options{})
	for _, tc := range []struct {
		script, out, trace string
		code               int
		exited             bool
	}{
		{`set -eux; value=ok; printf '%s' "$value"`, "ok", "+ value=ok\n+ printf %s ok\n", 0, false},
		{`false; printf wrong`, "", "+ false\n", 1, true},
		{`set +ex; printf '%s' "$absent"`, "", "+ set +ex\ngo-bash: absent: unbound variable\n", 1, true},
		{`set +u; printf recovered`, "recovered", "", 0, false},
	} {
		r, err := b.Exec(t.Context(), tc.script)
		if err != nil || r.ExitCode != tc.code || r.Stdout != tc.out || r.Stderr != tc.trace || r.Exited != tc.exited {
			t.Fatalf("%s: %#v, %v", tc.script, r, err)
		}
	}
}

func TestDynamicScriptLimits(t *testing.T) {
	b := newShell(t, bash.Options{MaxSteps: 100})
	for _, script := range []string{
		`code='eval "$code"'; eval "$code"`,
		`printf 'source ./recursive' > recursive; source ./recursive`,
		`printf 'while true; do :; done' > loop_script; bash loop_script`,
	} {
		if _, err := b.Exec(t.Context(), script); !errors.Is(err, bash.ErrExecutionLimit) {
			t.Fatalf("%s: expected execution limit, got %v", script, err)
		}
	}
	files := fstest.MapFS{"large": {Data: []byte(strings.Repeat(" ", (1<<20)+1))}}
	large := newShell(t, bash.Options{Mounts: []bash.Mount{{Path: "/scripts", FS: files}}})
	if _, err := large.Exec(t.Context(), `. /scripts/large`); !errors.Is(err, bash.ErrExecutionLimit) {
		t.Fatalf("oversized source: %v", err)
	}
}
