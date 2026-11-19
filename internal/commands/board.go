package commands

import (
	"io"
	"time"

	"github.com/AaronCQL/ape/internal/board"
)

// Board runs bare `ape`, the board with its arguments (after the subcommand name). See SPEC.md. It only reads, so
// it takes no lock.
func Board(env Env, args []string) error {
	if len(args) > 0 {
		return fail(ExitUsage, "the board takes no arguments; run 'ape -h'")
	}
	s, err := board.Collect(env.ApeDir, env.MainRoot, time.Now())
	if err != nil {
		return err
	}
	_, err = io.WriteString(env.Out, board.Render(board.Build(s)))
	return err
}
