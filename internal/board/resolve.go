package board

import (
	"path/filepath"
	"slices"

	"github.com/AaronCQL/ape/internal/gitx"
)

// Work is what task n has in the code repo (SPEC.md "Task's branch and worktree"): the board and every command
// find it the same way, by number, whatever the task's current slug.
type Work struct {
	Branches []string // local ape/T12-* branches with n's number, sorted; more than one is a conflict
	// Worktrees on such a branch, or at <wtDir>/T12-* with n's number on no task's branch (detached mid-rebase,
	// say). Prunable ones (directory deleted by hand) are included.
	Worktrees []gitx.Worktree
}

// Resolve finds task n's Work among the code repo's worktrees and branches; wtDir is where `ape start` puts
// worktrees (locate.WorktreePath(mainRoot, "")).
func Resolve(n int, wtDir string, wts []gitx.Worktree, brs []gitx.Branch) Work {
	var w Work
	for _, b := range brs {
		if m, ok := BranchNum(b.Name); ok && m == n {
			w.Branches = append(w.Branches, b.Name)
		}
	}
	slices.Sort(w.Branches)
	for _, wt := range wts {
		m, ok := BranchNum(wt.Branch)
		if !ok && filepath.Dir(wt.Path) == wtDir {
			m, ok = BranchNum(BranchPrefix + filepath.Base(wt.Path))
		}
		if ok && m == n {
			w.Worktrees = append(w.Worktrees, wt)
		}
	}
	return w
}

// Branch is the task's branch: "" if it has none or several.
func (w Work) Branch() string {
	if len(w.Branches) == 1 {
		return w.Branches[0]
	}
	return ""
}

// Live is the first worktree whose directory exists: the one that makes the task DOING.
func (w Work) Live() (gitx.Worktree, bool) {
	for _, wt := range w.Worktrees {
		if !wt.Prunable {
			return wt, true
		}
	}
	return gitx.Worktree{}, false
}
