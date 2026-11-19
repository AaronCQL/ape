package commands

import "flag"

const doneUsage = `ape done <id> "<summary>"`

// Done runs `ape done` with its arguments (after the subcommand name). See SPEC.md.
func Done(env Env, args []string) error {
	n, summary, err := numAndText(flag.NewFlagSet("done", flag.ContinueOnError), args, doneUsage, "summary")
	if err != nil {
		return err
	}
	return withLock(env, func() error { return finish(env, "done", n, summary, false) })
}

// numAndText parses `<id> "<text>"` plus fs's flags; text is made one line and must not be empty.
func numAndText(fs *flag.FlagSet, args []string, usage, what string) (int, string, error) {
	pos, err := parseArgs(fs, args, usage)
	if err != nil {
		return 0, "", err
	}
	if len(pos) != 2 {
		return 0, "", usageErr(usage, "want a task ID and a quoted %s, got %d arguments", what, len(pos))
	}
	n, err := parseNum(pos[0])
	if err != nil {
		return 0, "", err
	}
	text := oneLine(pos[1])
	if text == "" {
		return 0, "", usageErr(usage, "empty %s", what)
	}
	return n, text, nil
}
