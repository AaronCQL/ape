package commands

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	"github.com/AaronCQL/ape/internal/board"
	"github.com/AaronCQL/ape/internal/gitx"
	"github.com/AaronCQL/ape/internal/locate"
	"github.com/AaronCQL/ape/internal/task"
)

// Start runs `ape start` with its arguments (after the subcommand name). See SPEC.md.
func Start(env Env, args []string) error {
	n, err := oneNum("start", args)
	if err != nil {
		return err
	}
	return withLock(env, func() error { return start(env, n) })
}

func start(env Env, n int) error {
	c, err := findTask(env, n)
	if err != nil {
		return err
	}
	code := codeRepo(env)
	// A worktree deleted by hand would still hold its branch.
	if _, err := code.Run("worktree", "prune"); err != nil {
		return err
	}
	w, err := taskWork(env, n)
	if err != nil {
		return err
	}
	if wt, taken := w.Live(); taken {
		return fail(ExitConflict, "taken: worktree %s", wt.Path)
	}
	if needs := c.Get("needs"); needs != "" {
		return fail(ExitUsage, "%s needs %s; once that's resolved: ape set %s status=ready needs=", task.ID(n), needs, task.ID(n))
	}
	if status := c.Get("status"); status != "ready" {
		if status == "" {
			status = "without a status"
		}
		return fail(ExitUsage, "%s is %s; triage it", task.ID(n), status)
	}
	if c.Get("kind") == "owner" {
		return fail(ExitUsage, "%s is kind owner; only the owner can do it", task.ID(n))
	}

	// A paused branch is resumed under its own name, whatever the task's slug is now.
	branch := w.Branch()
	paused := branch != ""
	if !paused {
		branch = branchName(c)
	}
	path := locate.WorktreePath(env.MainRoot, strings.TrimPrefix(branch, board.BranchPrefix))
	if _, err := os.Lstat(path); err == nil {
		return fail(ExitConflict, "%s exists but isn't a worktree of %s; move it away, then start", path, branch)
	} else if !errors.Is(err, fs.ErrNotExist) {
		return err
	}
	var base string
	if !paused {
		if base, err = code.Head(); err != nil {
			if b, berr := code.CurrentBranch(); berr == nil && b != "" {
				if ok, berr := code.BranchExists(b); berr == nil && !ok {
					return fail(ExitUsage, "code repo has no commits; commit first")
				}
			}
			return err
		}
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	if paused {
		_, err = code.Run("worktree", "add", "-q", path, branch)
	} else {
		_, err = code.Run("worktree", "add", "-q", "-b", branch, path, base)
	}
	if err != nil {
		return err
	}
	if err := os.Symlink(env.ApeDir, filepath.Join(path, apeLink)); err != nil {
		return fail(ExitConflict, "worktree %s created, but linking ape/ into it failed: %v; link it by hand", path, err)
	}

	printf(env, "%s\nbranch %s\n", path, branch)
	// check-ignore prints the path when it's ignored; anything else (not ignored, or git failing) gets the warning.
	if out, _ := (gitx.Repo{Dir: path}).Run("check-ignore", apeLink); out != apeLink {
		printf(env, "warning: ape/ not ignored by the code repo; run ape init in the main worktree\n")
	}
	return nil
}
