package main

import (
	"context"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	bash "github.com/adrianliechti/go-bash"
)

// These scenarios run once in go-bash with the virtual git and once in the
// host's /bin/sh with real git. Each line is echoed with its merged output
// and exit status; the transcripts must match, including commit hashes.
var scenarios = []struct{ name, script string }{
	{"commit workflow", `
git init -q -b main
git status
printf 'pear\napple\n' > fruit.txt
mkdir -p notes/daily
echo todo > notes/daily/today.md
git status -s
git add fruit.txt
git commit -m "add fruit"
echo plum >> fruit.txt
git diff
git commit -am "add plum"
git log
git log --oneline --stat
git cat-file -p HEAD
`},
	{"branches", `
git init -q -b main
echo base > base.txt
git add .
git commit -q -m base
git checkout -b feature
echo feature > feature.txt
git add feature.txt
git commit -q -m feature
git switch main
git merge feature
git branch
git branch -d feature
git log --format="%h %an %s"
`},
	{"pipelines with coreutils", `
git init -q -b main
seq 1 20 > numbers.txt
git add numbers.txt
git commit -q -m numbers
sed 's/^7$/seven/' numbers.txt > tmp && mv tmp numbers.txt
git diff --stat
git diff | grep '^[-+]' | sort
git status --porcelain | cut -c4-
git ls-files | grep -c txt
git rev-parse HEAD | head -c 7
`},
	{"subdirectory and errors", `
git log
git init -q -b main
mkdir src
echo a > src/a
git add src
git commit -q -m src
cd src && echo b > b && git status
cd src && git rev-parse --show-prefix
git show nope
git commit -m nothing
`},
}

var env = []string{
	"GIT_CONFIG_NOSYSTEM=1", "GIT_CONFIG_GLOBAL=/dev/null", "TZ=UTC", "LC_ALL=C",
	"GIT_AUTHOR_NAME=Ada Author", "GIT_AUTHOR_EMAIL=ada@example.com",
	"GIT_COMMITTER_NAME=Cody Committer", "GIT_COMMITTER_EMAIL=cody@example.com",
	"GIT_AUTHOR_DATE=@1700000000 +0100", "GIT_COMMITTER_DATE=@1700000000 +0000",
}

func TestGitCompatibility(t *testing.T) {
	realGit, err := exec.LookPath("git")
	if err != nil {
		t.Skip("real git not found on PATH")
	}
	for _, sc := range scenarios {
		t.Run(sc.name, func(t *testing.T) {
			want := hostTranscript(t, filepath.Dir(realGit), sc.script)
			got := shellTranscript(t, sc.script)
			if got != want {
				t.Errorf("transcripts differ\n--- real git\n%s\n--- go-bash\n%s", want, got)
			}
		})
	}
}

func wrap(script string) string {
	var sh strings.Builder
	for _, line := range strings.Split(strings.TrimSpace(script), "\n") {
		quoted := "'" + strings.ReplaceAll("$ "+line, "'", `'\''`) + "'"
		sh.WriteString("printf '%s\\n' " + quoted + "\n(" + line + ") 2>&1; echo \"[exit $?]\"\n")
	}
	return sh.String()
}

func hostTranscript(t *testing.T, gitDir, script string) string {
	dir, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command("/bin/sh", "-c", wrap(script))
	cmd.Dir = dir
	cmd.Env = append([]string{"PATH=" + gitDir + ":/usr/bin:/bin", "HOME=" + dir}, env...)
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("sh: %v\n%s", err, out)
	}
	return strings.ReplaceAll(string(out), dir, "$ROOT")
}

func shellTranscript(t *testing.T, script string) string {
	ctx := context.Background()
	sh, err := bash.New(ctx, bash.Options{
		Timeout:  time.Minute,
		Commands: map[string]bash.CommandFunc{"git": gitCommand},
		Cwd:      "/work",
	})
	if err != nil {
		t.Fatal(err)
	}
	defer sh.Close(ctx)
	var exports strings.Builder
	for _, kv := range append([]string{"HOME=/work"}, env...) {
		k, v, _ := strings.Cut(kv, "=")
		exports.WriteString("export " + k + "='" + v + "'\n")
	}
	r, err := sh.Exec(ctx, exports.String()+wrap(script))
	if err != nil || r.Stderr != "" {
		t.Fatalf("go-bash: %v\n%s%s", err, r.Stdout, r.Stderr)
	}
	return strings.ReplaceAll(r.Stdout, "/work", "$ROOT")
}
