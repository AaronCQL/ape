package commands

import (
	"os"
	"path/filepath"
	"strings"

	"github.com/AaronCQL/ape/internal/board"
	"github.com/AaronCQL/ape/internal/gitx"
	"github.com/AaronCQL/ape/internal/locate"
	"github.com/AaronCQL/ape/internal/task"
)

// apeLink is the name of the symlink to the ape dir inside a task's worktree.
const apeLink = "ape"

// taskWork finds task n's branch and worktrees as the board does (board.Resolve); several branches is a conflict.
func taskWork(env Env, n int) (board.Work, error) {
	code := codeRepo(env)
	wts, err := code.Worktrees()
	if err != nil {
		return board.Work{}, err
	}
	brs, err := code.Branches(board.BranchPrefix)
	if err != nil {
		return board.Work{}, err
	}
	w := board.Resolve(n, locate.WorktreePath(env.MainRoot, ""), wts, brs)
	if len(w.Branches) > 1 {
		return w, fail(ExitConflict, "%s has several branches: %s; delete the stale one", task.ID(n),
			strings.Join(w.Branches, ", "))
	}
	return w, nil
}

// tip is branch's commit; "" for no branch.
func tip(code gitx.Repo, branch string) (string, error) {
	if branch == "" {
		return "", nil
	}
	return code.Run("rev-parse", "--verify", "refs/heads/"+branch+"^{commit}")
}

// checkRemovable fails unless removing wt loses nothing: it isn't the main worktree nor locked, and unless force,
// a detached HEAD (mid-rebase, say) is merged into keep (gitx.Merged) (a commit; named keepName in messages) and nothing is
// uncommitted (a conflict whose message ends "commit or stash, then <then>"). A worktree deleted by hand can go.
// The ape link doesn't count as uncommitted.
func checkRemovable(env Env, wt gitx.Worktree, force bool, keep, keepName, then string) error {
	if wt.Prunable {
		return nil
	}
	if samePath(wt.Path, env.MainRoot) {
		return fail(ExitConflict, "%s is checked out in the main worktree %s; check out another branch there first",
			wt.Branch, wt.Path)
	}
	if wt.Locked {
		return fail(ExitConflict, "worktree %s is locked; git worktree unlock it", wt.Path)
	}
	if force {
		return nil
	}
	if wt.Detached && wt.Head != "" {
		kept := false
		if keep != "" {
			var err error
			if kept, err = codeRepo(env).Merged(wt.Head, keep); err != nil {
				return err
			}
		}
		if !kept {
			return fail(ExitConflict, "worktree %s is detached at commits not in %s; finish the rebase (or check out "+
				"its branch), then %s", wt.Path, keepName, then)
		}
	}
	dirt, err := gitx.Dirty(wt.Path)
	if err != nil {
		return err
	}
	for _, e := range dirt {
		if e != "?? "+apeLink {
			return fail(ExitConflict, "worktree %s has uncommitted changes; commit or stash, then %s", wt.Path, then)
		}
	}
	return nil
}

// removeWorktree removes wt, which checkRemovable passed (with the same force); one deleted by hand is pruned.
// The ape link goes first: git refuses to remove a worktree with an untracked file, which the link is when the code
// repo doesn't ignore it (start warned), and git mustn't follow it either way.
func removeWorktree(code gitx.Repo, wt gitx.Worktree, force bool) error {
	if wt.Prunable {
		_, err := code.Run("worktree", "prune")
		return err
	}
	link := filepath.Join(wt.Path, apeLink)
	target, linkErr := os.Readlink(link)
	if linkErr == nil {
		if err := os.Remove(link); err != nil {
			return err
		}
	}
	args := []string{"worktree", "remove", wt.Path}
	if force {
		args = []string{"worktree", "remove", "--force", wt.Path}
	}
	if _, err := code.Run(args...); err != nil {
		if linkErr == nil {
			os.Symlink(target, link) // put it back; the worktree stays usable
		}
		return err
	}
	return nil
}

// samePath compares two paths after resolving symlinks.
func samePath(a, b string) bool {
	ra, errA := filepath.EvalSymlinks(a)
	rb, errB := filepath.EvalSymlinks(b)
	if errA != nil || errB != nil {
		return filepath.Clean(a) == filepath.Clean(b)
	}
	return ra == rb
}

// oneNum parses args that must be exactly one task number.
func oneNum(cmd string, args []string) (int, error) {
	if len(args) != 1 {
		return 0, fail(ExitUsage, "usage: ape %s <id>", cmd)
	}
	return parseNum(args[0])
}
