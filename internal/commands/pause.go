package commands

import "github.com/AaronCQL/ape/internal/task"

// Pause runs `ape pause` with its arguments (after the subcommand name). See SPEC.md.
func Pause(env Env, args []string) error {
	n, err := oneNum("pause", args)
	if err != nil {
		return err
	}
	return withLock(env, func() error { return pause(env, n) })
}

func pause(env Env, n int) error {
	if _, err := findTask(env, n); err != nil {
		return err
	}
	w, err := taskWork(env, n)
	if err != nil {
		return err
	}
	if len(w.Worktrees) == 0 {
		return fail(ExitUsage, "%s has no worktree; nothing to pause", task.ID(n))
	}
	code := codeRepo(env)
	branch := w.Branch()
	keep, err := tip(code, branch)
	if err != nil {
		return err
	}
	keepName := branch
	if keepName == "" {
		keepName = "a branch"
	}
	for _, wt := range w.Worktrees {
		if err := checkRemovable(env, wt, false, keep, keepName, "pause"); err != nil {
			return err
		}
	}
	for _, wt := range w.Worktrees {
		if err := removeWorktree(code, wt, false); err != nil {
			return err
		}
	}
	if branch == "" {
		printf(env, "paused %s\n", task.ID(n))
	} else {
		printf(env, "paused %s; branch %s kept\n", task.ID(n), branch)
	}
	return nil
}
