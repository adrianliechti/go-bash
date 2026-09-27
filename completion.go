package bash

import (
	"context"
	"io/fs"

	"github.com/adrianliechti/go-bash/internal/shell"
)

// CompletionKind selects which names Complete looks up.
type CompletionKind uint8

const (
	CompleteFiles CompletionKind = iota
	CompleteDirectories
	CompleteCommands
	CompleteVariables
)

// Complete returns sorted, unique names matching a literal prefix, using the
// current virtual cwd, PATH, functions, and variables. Directories end in '/'.
// It never evaluates shell text or changes session state. The caller handles
// tokenization, quoting, and inserting candidates in its editor. Missing or
// unreadable directories produce no matches; cancellation and a closed session
// return errors. Variable prefixes and results omit '$'.
func (s *Shell) Complete(ctx context.Context, prefix string, kind CompletionKind) ([]string, error) {
	if err := s.lock(ctx); err != nil {
		return nil, err
	}
	defer s.unlock()
	if s.closed {
		return nil, fs.ErrClosed
	}
	return s.shell.Complete(ctx, prefix, shell.CompletionKind(kind))
}
