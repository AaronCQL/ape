// Package commands implements ape's subcommands (SPEC.md "Commands").
package commands

import (
	"errors"
	"fmt"
	"io"
	"strings"
)

// Env is what every command runs against.
type Env struct {
	Cwd      string
	ApeDir   string // resolved; "" for init
	MainRoot string // "" for init
	Out      io.Writer
	In       io.Reader // stdin if it's a pipe or a file (new reads a task's body from it), else nil
}

// Exit codes (SPEC.md "Writes").
const (
	ExitUsage    = 1
	ExitConflict = 2
	ExitNotFound = 3
)

// Error is a failure with its exit code; Msg is printed as "ape: <Msg>".
type Error struct {
	Code int
	Msg  string
}

func (e *Error) Error() string { return e.Msg }

func fail(code int, format string, a ...any) error {
	return &Error{Code: code, Msg: fmt.Sprintf(format, a...)}
}

// Report returns the line ape prints for err (after "ape: ") and the exit code: an *Error's own code, else
// ExitUsage. The line is always one line: a multi-line message (git's stderr) has its lines trimmed and joined
// with "; ".
func Report(err error) (msg string, code int) {
	msg, code = err.Error(), ExitUsage
	var e *Error
	if errors.As(err, &e) {
		msg, code = e.Msg, e.Code
	}
	var lines []string
	for l := range strings.Lines(msg) {
		if l = strings.TrimSpace(l); l != "" {
			lines = append(lines, l)
		}
	}
	return strings.Join(lines, "; "), code
}
