package commands

import (
	"errors"
	"fmt"
	"path/filepath"
	"strings"
	"time"

	"github.com/AaronCQL/ape/internal/board"
	"github.com/AaronCQL/ape/internal/fsx"
	"github.com/AaronCQL/ape/internal/gitx"
	"github.com/AaronCQL/ape/internal/lock"
	"github.com/AaronCQL/ape/internal/task"
)

// lockTimeout is how long a write command waits for another ape process (SPEC.md "Writes").
var lockTimeout = 30 * time.Second

// withLock runs fn holding the ape lock.
func withLock(env Env, fn func() error) error {
	release, err := lock.Acquire(env.ApeDir, lockTimeout)
	if errors.Is(err, lock.ErrTimeout) {
		return fail(ExitConflict, "lock held >%s: %s; another ape is stuck or slow", lockTimeout,
			filepath.Join(env.ApeDir, ".git", "ape.lock"))
	}
	if err != nil {
		return err
	}
	defer release()
	return fn()
}

// apeRepo is the ape dir's own git repo.
func apeRepo(env Env) gitx.Repo { return gitx.Repo{Dir: env.ApeDir} }

// codeRepo is the code repo's main worktree.
func codeRepo(env Env) gitx.Repo { return gitx.Repo{Dir: env.MainRoot} }

// tasksDir is <ape>/tasks.
func tasksDir(env Env) string { return filepath.Join(env.ApeDir, "tasks") }

// parseNum parses a task ID argument: "T12", "t12" or "12".
func parseNum(s string) (int, error) {
	n, ok := task.ParseID(s)
	if !ok {
		return 0, fail(ExitUsage, "%q isn't a task ID; IDs look like T12", s)
	}
	return n, nil
}

// findTask loads task n, or fails with ExitNotFound.
func findTask(env Env, n int) (*task.Task, error) {
	tasks, _, err := task.Load(tasksDir(env))
	if err != nil {
		return nil, err
	}
	for _, c := range tasks {
		if c.Num == n {
			return c, nil
		}
	}
	return nil, fail(ExitNotFound, "no %s in %s", task.ID(n), tasksDir(env))
}

// branchName is the task's branch, ape/T12-slug.
func branchName(c *task.Task) string {
	return board.BranchPrefix + strings.TrimSuffix(task.FileName(c.Num, c.Slug), ".md")
}

// relPath is p relative to the ape dir, for commits.
func relPath(env Env, p string) string {
	r, err := filepath.Rel(env.ApeDir, p)
	if err != nil {
		return p
	}
	return r
}

// subject is an ape commit's message: "<verb>(T12): <text>", Conventional Commits with the task's ID as scope.
func subject(verb string, n int, text string) string { return verb + "(" + task.ID(n) + "): " + text }

// commitEdits commits task c's own pending hand edits (tracked or not; that file only) on their own, as "edit(T12):
// hand edits", so that a command that then rewrites or deletes it neither folds them into its commit nor loses them.
// Nothing pending: no commit.
func commitEdits(env Env, c *task.Task) error {
	return apeRepo(env).Commit(subject("edit", c.Num, "hand edits"), relPath(env, c.Path))
}

// writeTask writes c to its path.
func writeTask(c *task.Task) error { return fsx.WriteAtomic(c.Path, c.Bytes(), 0o644) }

// printf writes to env.Out.
func printf(env Env, format string, a ...any) { fmt.Fprintf(env.Out, format, a...) }
