// This example registers a go-git based git as a virtual go-bash command.
package main

import (
	"context"
	"flag"
	"fmt"
	"io/fs"
	"os"
	"os/signal"
	"time"

	bash "github.com/adrianliechti/go-bash"
	"github.com/adrianliechti/go-bash/vfs"
	git "github.com/adrianliechti/go-git"
)

const demo = `
export GIT_AUTHOR_NAME="Ada" GIT_AUTHOR_EMAIL="ada@example.com"
export GIT_COMMITTER_NAME="Ada" GIT_COMMITTER_EMAIL="ada@example.com"
git init -q -b main
printf 'pear\napple\n' > fruit.txt
git add fruit.txt
git commit -q -m "add fruit"
echo plum >> fruit.txt
git diff
git commit -qam "add plum"
git log --oneline | wc -l
git show --stat --format=%s | grep changed
`

func main() {
	script := flag.String("c", demo, "shell script to execute")
	flag.Parse()
	os.Exit(run(*script))
}

func run(script string) int {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()
	sh, err := bash.New(ctx, bash.Options{
		Timeout:  30 * time.Second,
		Commands: map[string]bash.CommandFunc{"git": gitCommand},
	})
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	defer sh.Close(context.Background())

	result, err := sh.Exec(ctx, script)
	fmt.Print(result.Stdout)
	fmt.Fprint(os.Stderr, result.Stderr)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	return result.ExitCode
}

// gitCommand runs git in-process against the shell's filesystem. There is no
// network, editor, pager, or hook support, and the host's git config is never
// read: the global config is $HOME/.gitconfig inside the shell.
func gitCommand(ctx context.Context, cmd *bash.Command) (int, error) {
	return git.Run(ctx, git.Options{
		Args:   cmd.Args[1:],
		Dir:    cmd.Cwd,
		Env:    cmd.Env,
		Stdin:  cmd.Stdin,
		Stdout: cmd.Stdout,
		Stderr: cmd.Stderr,
		FS:     shellFS{cmd.FS},
	})
}

// shellFS adapts vfs.WriteFS to git.FS; only OpenFile's result type differs.
type shellFS struct{ vfs.WriteFS }

func (f shellFS) OpenFile(name string, flag int, perm fs.FileMode) (git.File, error) {
	file, err := f.WriteFS.OpenFile(name, flag, perm)
	if err != nil {
		return nil, err
	}
	return file, nil
}
