package bash_test

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	bash "github.com/adrianliechti/go-bash"
)

func TestBashConditionals(t *testing.T) {
	testBashDifferential(t, []bashTestCase{
		{"case alternatives", `value=hello.txt; case "$value" in *.go|*.txt) printf match;; *) printf wrong;; esac`, "match", 0},
		{"case quoted pattern", `pattern='*.txt'; case hello.txt in "$pattern") printf wrong;; $pattern) printf match;; esac`, "match", 0},
		{"case no splitting", `case 'a b' in a*) printf match;; esac`, "match", 0},
		{"case no match status", `false; case x in y) false;; esac; printf '%s' "$?"`, "0", 0},
		{"case empty match status", `false; case x in x) ;; esac; printf '%s' "$?"`, "0", 0},
		{"case branch status", `case x in x) false;; esac`, "", 1},
		{"case lazy patterns", `case x in x) printf match;; $(touch case-side-effect)) printf wrong;; esac; [[ ! -e case-side-effect ]]`, "match", 0},
		{"case loop control", `for n in a b c; do case $n in a) continue;; c) break;; esac; printf '%s' "$n"; done`, "b", 0},
		{"test empty and negation", `[[ '' ]]; printf '%s' "$?"; [[ ! '' ]]; printf '%s' "$?"; [[ abc ]]; printf '%s' "$?"`, "100", 0},
		{"test word stays intact", `value='a b *'; [[ $value == 'a b *' ]] && printf yes`, "yes", 0},
		{"test quoted glob", `pattern='a*'; [[ apple == $pattern ]]; printf '%s' "$?"; [[ apple == "$pattern" ]]; printf '%s' "$?"; [[ 'a*' == a\* ]]; printf '%s' "$?"`, "010", 0},
		{"test glob slash and newline", `[[ a/b == a?b ]] && [[ $'a\nb' == a*b ]] && printf yes`, "yes", 0},
		{"test unequal pattern", `[[ apple != b* ]] && [[ apple != 'a*' ]] && printf yes`, "yes", 0},
		{"test and short circuit", `unset x; [[ '' && ${x:=wrong} ]]; printf '%s:%s' "$?" "${x-unset}"`, "1:unset", 0},
		{"test or short circuit", `unset x; [[ yes || ${x:=wrong} ]]; printf '%s:%s' "$?" "${x-unset}"`, "0:unset", 0},
		{"test grouping", `[[ ( -n yes && -z '' ) && ! ( apple == b* || z < a ) ]] && printf yes`, "yes", 0},
		{"test numeric expressions", `n=3; [[ n -eq 3 && '1 + 2' -ge 3 && 2 -lt 3 && 5 -ne 4 && 4 -le 4 && 9 -gt 8 ]] && printf yes`, "yes", 0},
		{"test arithmetic side effects", `n=1; [[ n++ -eq 1 ]]; printf '%s:%s' "$?" "$n"`, "0:2", 0},
		{"test empty arithmetic", `[[ '' -eq 0 ]] && printf yes`, "yes", 0},
		{"test virtual files", `printf content > conditional-file; mkdir conditional-dir; [[ -f conditional-file && -s conditional-file && -d conditional-dir && -e conditional-file && ! -e absent && ! -e '' && ! -L conditional-file ]] && printf yes`, "yes", 0},
		{"test missing file times", `: > conditional-time; [[ conditional-time -nt absent && absent -ot conditional-time ]] && printf yes; [[ absent -nt absent ]]`, "yes", 1},
		{"test option state", `[[ -o pipefail ]]; printf '%s' "$?"; set -o pipefail; [[ -o pipefail ]]; printf '%s' "$?"`, "10", 0},
		{"arithmetic for", `for ((i=0; i<4; i++)); do printf '%s' "$i"; done; printf ':%s' "$i"`, "0123:4", 0},
		{"arithmetic continue post", `for ((i=0; i<4; i++)); do if [[ i -eq 1 ]]; then continue; fi; printf '%s' "$i"; done`, "023", 0},
		{"arithmetic break", `for ((i=0; i<5; i++)); do if [[ i -eq 2 ]]; then break; fi; printf '%s' "$i"; done; printf ':%s' "$i"`, "01:2", 0},
		{"arithmetic omitted expressions", `i=0; for ((;;)); do ((i++)); if [[ i -ge 3 ]]; then break; fi; done; printf '%s' "$i"`, "3", 0},
		{"arithmetic last status", `for ((i=0; i<2; i++)); do false; done`, "", 1},
		{"arithmetic zero iterations", `false; for ((i=0; i<0; i++)); do false; done`, "", 0},
	}, 3)
}

func TestBash4Conditionals(t *testing.T) {
	testBashDifferential(t, []bashTestCase{
		{"case fallthrough", `case x in x) printf first ;& y) printf second ;; *) printf wrong ;; esac`, "firstsecond", 0},
		{"case resume", `case x in x) printf first ;;& y) printf wrong ;; x|z) printf second ;; esac`, "firstsecond", 0},
		{"case mixed terminators", `case x in x) printf a ;& y) printf b ;;& x) printf c ;; esac`, "abc", 0},
		{"case fallthrough skips expansion", `case x in x) printf first ;& $(touch fallthrough-side-effect)) printf second ;; esac; [[ ! -e fallthrough-side-effect ]]`, "firstsecond", 0},
		{"test variable set", `unset v; [[ -v v ]]; printf '%s' "$?"; v=''; [[ -v v ]]; printf '%s' "$?"`, "10", 0},
	}, 4)
}

func TestBashArgumentsAndPipefail(t *testing.T) {
	testBashDifferential(t, []bashTestCase{
		{"set and shift", `set -- one 'two three' ''; printf '%s:' "$#"; shift; printf '<%s>' "$@"; shift 2; printf ':%s' "$#"`, "3:<two three><>:0", 0},
		{"set arguments without separator", `set first second; printf '%s:%s:%s' "$#" "$1" "$2"`, "2:first:second", 0},
		{"set clears arguments", `set -- a b; set --; printf '%s:%s' "$#" "${1-unset}"`, "0:unset", 0},
		{"set shift function isolation", `set -- outer; f() { set -- inner extra; shift; printf '%s:' "$1"; }; f original; printf '%s' "$1"`, "extra:outer", 0},
		{"shift too far preserves args", `set -- one; shift 2 2>/dev/null; printf '%s:%s:%s' "$?" "$#" "$1"`, "1:1:one", 0},
		{"shift zero", `set -- one; shift 0; printf '%s:%s' "$?" "$1"`, "0:one", 0},
		{"for uses positional snapshot", `set -- one two three; for arg; do printf '<%s>' "$arg"; shift; done; printf '%s' "$#"`, "<one><two><three>0", 0},
		{"pipefail toggles", `false | true; printf '%s' "$?"; set -o pipefail; false | true; printf '%s' "$?"; set +o pipefail; false | true; printf '%s' "$?"`, "010", 0},
		{"pipefail rightmost failure", `set -o pipefail; (exit 7) | (exit 3) | true; printf '%s:' "$?"; (exit 7) | true | (exit 4); printf '%s' "$?"`, "3:4", 0},
		{"pipefail subshell inheritance", `set -o pipefail; (false | true); printf '%s:' "$?"; (set +o pipefail); false | true; printf '%s' "$?"`, "1:1", 0},
		{"pipefail nested shell reset", `set -o pipefail; bash -c 'false | true'; printf '%s' "$?"`, "0", 0},
		{"pipefail negation", `set -o pipefail; ! false | true; printf '%s:' "$?"; ! true | true; printf '%s' "$?"`, "0:1", 0},
		{"pipefail command substitution", `set -o pipefail; value=$(false | true); printf '%s:%s' "$?" "$value"`, "1:", 0},
	}, 3)
}

func TestBashRead(t *testing.T) {
	testBashDifferential(t, []bashTestCase{
		{"read fields and remainder", `read first rest <<< '  one  two   three  '; printf '<%s><%s>' "$first" "$rest"`, "<one><two   three>", 0},
		{"read empty IFS", `IFS= read -r line <<< '  a\b  '; printf '<%s>' "$line"`, "<  a\\b  >", 0},
		{"read implicit reply", `read -r <<< '  a\b  '; printf '<%s>' "$REPLY"`, "<  a\\b  >", 0},
		{"read escaped separators", `read a b <<< 'one\ two three'; printf '<%s><%s>' "$a" "$b"`, "<one two><three>", 0},
		{"read escaped trailing space", `read a <<< 'value\ '; printf '<%s>' "$a"`, "<value >", 0},
		{"read raw backslashes", `read -r a b <<< 'one\ two'; printf '<%s><%s>' "$a" "$b"`, "<one\\><two>", 0},
		{"read continuation", `printf 'one\\\ntwo\n' | { read line; printf '<%s>' "$line"; }`, "<onetwo>", 0},
		{"read consecutive lines", `printf 'first\nsecond\n' | { read -r a; read -r b; printf '<%s><%s>' "$a" "$b"; }`, "<first><second>", 0},
		{"read leaves stdin for cat", `printf 'first\nsecond\n' | { read -r a; printf '<%s>' "$a"; cat; }`, "<first>second\n", 0},
		{"read EOF keeps partial line", `printf tail | { read -r line; printf '%s:<%s>' "$?" "$line"; }`, "1:<tail>", 0},
		{"read empty EOF clears variables", `a=old; read a < /dev/null; printf '%s:<%s>' "$?" "$a"`, "1:<>", 0},
		{"read missing fields", `a=old; b=old; c=old; read a b c <<< one; printf '<%s><%s><%s>' "$a" "$b" "$c"`, "<one><><>", 0},
		{"read nonwhite separators", `IFS=: read -r a b c <<< ':one::'; printf '<%s><%s><%s>' "$a" "$b" "$c"`, "<><one><>", 0},
		{"read final delimiter", `IFS=: read -r a b <<< 'one:two:'; printf '<%s><%s>' "$a" "$b"`, "<one><two>", 0},
		{"read remainder delimiter", `IFS=: read -r a b <<< 'one:two:three:'; printf '<%s><%s>' "$a" "$b"`, "<one><two:three:>", 0},
		{"read single remainder", `IFS=: read -r a <<< ':one:'; printf '<%s>' "$a"`, "<:one:>", 0},
		{"read only delimiter", `IFS=: read -r a <<< ':'; printf '<%s>' "$a"`, "<>", 0},
		{"read custom delimiter", `printf 'one:two:' | { read -rd : a; read -r -d : b; printf '<%s><%s>' "$a" "$b"; }`, "<one><two>", 0},
		{"read NUL delimiter", `printf 'one\000two\000' | { read -r -d '' a; read -r -d '' b; printf '<%s><%s>' "$a" "$b"; }`, "<one><two>", 0},
		{"read loop", `while IFS= read -r line; do printf '<%s>' "$line"; done <<'EOF'
one
two
EOF`, "<one><two>", 0},
	}, 3)
}

func TestBashSessionArgumentsAndOptions(t *testing.T) {
	b := newShell(t, bash.Options{})
	for _, tc := range []struct{ script, want string }{
		{`set -- first second; set -o pipefail`, ""},
		{`false | true; printf '%s:%s:%s' "$?" "$#" "$1"; shift`, "1:2:first"},
		{`printf '%s:%s' "$#" "$1"`, "1:second"},
	} {
		r, err := b.Exec(t.Context(), tc.script)
		if err != nil || r.ExitCode != 0 || r.Stdout != tc.want {
			t.Fatalf("%s: %#v, %v", tc.script, r, err)
		}
	}
}

func TestBashCompatibilityLimits(t *testing.T) {
	b := newShell(t, bash.Options{Timeout: 5 * time.Second, MaxSteps: 100})
	if _, err := b.Exec(t.Context(), `for ((;;)); do :; done`); !errors.Is(err, bash.ErrExecutionLimit) {
		t.Fatalf("arithmetic loop limit: %v", err)
	}
	if _, err := b.Exec(t.Context(), `yes | read -r -d '' line`); !errors.Is(err, bash.ErrExecutionLimit) {
		t.Fatalf("read limit: %v", err)
	}
	ctx, cancel := context.WithTimeout(t.Context(), 100*time.Millisecond)
	defer cancel()
	if _, err := b.Exec(ctx, `sleep 3600 | read -r line`); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("read cancellation: %v", err)
	}
	r, err := b.Run(t.Context(), bash.Request{Script: `IFS= read -r value; printf '%s' "$value"`, Stdin: strings.Repeat("x", 4096) + "\n"})
	if err != nil || r.ExitCode != 0 || r.Stdout != strings.Repeat("x", 4096) {
		t.Fatalf("read after cancellation: %#v, %v", r, err)
	}
}
