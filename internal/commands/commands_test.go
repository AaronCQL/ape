package commands

import (
	"errors"
	"fmt"
	"testing"
)

func TestReport(t *testing.T) {
	for _, tc := range []struct {
		err  error
		msg  string
		code int
	}{
		{fail(ExitConflict, "taken: worktree %s", "/x"), "taken: worktree /x", ExitConflict},
		{fmt.Errorf("pause: %w", fail(ExitNotFound, "no T9")), "no T9", ExitNotFound},
		{errors.New("git worktree add: fatal: x\nhint: y\n  \nhint: z\n"), "git worktree add: fatal: x; hint: y; hint: z",
			ExitUsage},
		{fail(ExitConflict, "a\r\nb"), "a; b", ExitConflict},
	} {
		if msg, code := Report(tc.err); msg != tc.msg || code != tc.code {
			t.Errorf("Report(%q) = %q, %d; want %q, %d", tc.err, msg, code, tc.msg, tc.code)
		}
	}
}
