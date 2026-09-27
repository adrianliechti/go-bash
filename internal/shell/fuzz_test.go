package shell

import (
	"context"
	"io"
	"testing"
	"time"

	"github.com/adrianliechti/go-bash/internal/fsys"
)

func FuzzParse(f *testing.F) {
	for _, src := range []string{"", "echo 'unterminated", "x=$(echo hi)", "cat <<-EOF\n\thi\n\tEOF", "((1/0))", "cat >f 8>&1", "f() { f; }; f", "${x@P}", "x=(a b)", "\x00\xff", "( ( ( : ) ) )", "case x in x) : ;& y) : ;; esac", "[[ ! ( x == y || 1 -eq 0 ) ]]", "for ((;;)); do break; done"} {
		f.Add(src)
	}
	f.Add("cat <<EOF\nEO\\\nF\nprintf after")
	f.Add("cat <<ONE <<TWO\nON\\\nE\nx\nTW\\\nO")
	f.Fuzz(func(t *testing.T, src string) {
		if len(src) > 8192 {
			t.Skip()
		}
		_, _ = Parse(src)
	})
}

// Fuzzed text only reaches our interpreter and an empty in-memory namespace.
// It never reaches a host shell, host filesystem, or native process.
func FuzzExecution(f *testing.F) {
	for _, src := range []string{":", "x=1; ((x+=2))", "x=${x:-ok}", "for x in a b; do :; done", "f() { f; }; f", "exit 2 | true", "cat >file", "x=$(true)", "while true; do continue; done", "a=b; b=a; echo $((a))", "set -- a b; shift; for x; do :; done", "set -o pipefail; false | true", "IFS=: read -r a b <<<'x::y'", "case x in x) : ;;& *) false ;; esac", "[[ x == x && ! -e absent ]]", "[[ '1+2' -eq 3 ]]", "for ((i=0; i<3; i++)); do continue; done", "exec 3>file; read -u 3 x; exec 4>&3 3>&-", "getopts :abc: x -abc; set --; getopts :abc: x", "trap 'exit 3' EXIT; exit 1", "umask u=r+w,g=u,o-rwx"} {
		f.Add(src)
	}
	f.Fuzz(func(t *testing.T, src string) {
		if len(src) > 2048 {
			t.Skip()
		}
		n, err := fsys.New(nil, 4096)
		if err != nil {
			t.Fatal(err)
		}
		s := New(n, "/work", nil, func(context.Context, string, []string, map[string]string, IO) (int, error) { return 127, nil })
		defer s.Close()
		ctx, cancel := context.WithTimeout(t.Context(), 50*time.Millisecond)
		defer cancel()
		out := &boundedWriter{w: io.Discard, left: 4096}
		_, _ = s.Run(ctx, src, IO{In: &emptyReader{}, Out: out, Err: out}, 100)
	})
}

type emptyReader struct{}

func (*emptyReader) Read([]byte) (int, error) { return 0, io.EOF }
