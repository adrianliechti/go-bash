package shell

import (
	"context"
	"io/fs"
	"path"
	"slices"
	"strings"

	"github.com/adrianliechti/go-bash/internal/fsys"
	"mvdan.cc/sh/v3/syntax"
)

type CompletionKind uint8

const (
	CompleteFiles CompletionKind = iota
	CompleteDirectories
	CompleteCommands
	CompleteVariables
)

const builtinNames = ": true false cd pwd export local declare typeset readonly unset exit return break continue bash sh xargs set shift read command type which source . eval getopts umask exec trap"

func (s *Shell) Complete(ctx context.Context, prefix string, kind CompletionKind) ([]string, error) {
	if kind > CompleteVariables || strings.ContainsRune(prefix, 0) {
		return nil, fs.ErrInvalid
	}
	var matches []string
	add := func(name string) {
		if strings.HasPrefix(name, prefix) {
			matches = append(matches, name)
		}
	}
	if kind == CompleteVariables {
		for name, value := range s.vars {
			if value.IsSet() && syntax.ValidName(name) {
				add(name)
			}
		}
	} else if kind == CompleteCommands && !strings.Contains(prefix, "/") {
		for name := range strings.FieldsSeq(builtinNames) {
			add(name)
		}
		for name := range s.funcs {
			add(name)
		}
		seen := make(map[string]bool)
		for dir := range strings.SplitSeq(s.vars.Get("PATH").String(), ":") {
			if err := ctx.Err(); err != nil {
				return nil, err
			}
			dir = fsys.Resolve(s.Cwd, dir)
			if seen[dir] {
				continue
			}
			seen[dir] = true
			entries, _ := s.FS.ReadDir(dir)
			for _, entry := range entries {
				if err := ctx.Err(); err != nil {
					return nil, err
				}
				if !strings.HasPrefix(entry.Name(), prefix) {
					continue
				}
				info, err := s.FS.Stat(path.Join(dir, entry.Name()))
				if err == nil && info.Mode().IsRegular() && info.Mode()&0111 != 0 {
					add(entry.Name())
				}
			}
		}
	} else {
		dir, base := path.Split(prefix)
		entries, _ := s.FS.ReadDir(fsys.Resolve(s.Cwd, dir))
		for _, entry := range entries {
			if err := ctx.Err(); err != nil {
				return nil, err
			}
			name := entry.Name()
			if !strings.HasPrefix(name, base) || strings.HasPrefix(name, ".") && !strings.HasPrefix(base, ".") {
				continue
			}
			info, err := s.FS.Stat(fsys.Resolve(s.Cwd, dir+name))
			isDir := err == nil && info.IsDir()
			if kind == CompleteDirectories && !isDir {
				continue
			}
			if kind == CompleteCommands && !isDir && (err != nil || !info.Mode().IsRegular() || info.Mode()&0111 == 0) {
				continue
			}
			if isDir {
				name += "/"
			}
			add(dir + name)
		}
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	slices.Sort(matches)
	return slices.Compact(matches), nil
}
