package commands

import "flag"

const dropUsage = `ape drop <id> "<reason>" [--force]`

// Drop runs `ape drop` with its arguments (after the subcommand name). See SPEC.md.
func Drop(env Env, args []string) error {
	fs := flag.NewFlagSet("drop", flag.ContinueOnError)
	force := fs.Bool("force", false, "")
	n, reason, err := numAndText(fs, args, dropUsage, "reason")
	if err != nil {
		return err
	}
	return withLock(env, func() error { return finish(env, "drop", n, reason, *force, "") })
}
