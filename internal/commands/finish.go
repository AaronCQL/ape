package commands

import (
	"os"
	"slices"
	"strings"

	"github.com/AaronCQL/ape/internal/gitx"
	"github.com/AaronCQL/ape/internal/task"
)

// finish is done and drop: task n's branch (if any) and any detached worktree of it must be merged into the main
// root's HEAD (gitx.Merged: squash merges count), unless force, and its worktrees clean; only then do its worktrees,
// its branch and the task go, in one ape commit "<verb>(T12): <text>[ (code <sha>)]", after one "edit(T12): hand
// edits" of the task's pending hand edits, if any. code (done --code) is the commit that merged the task: it must be
// in the main root's HEAD, stands for the branch's merge check, and is the sha recorded (else HEAD's, with a branch).
// done also releases the tasks that need n, in that same commit (release); drop leaves them blocked and names them.
// A refusal leaves everything as it was.
func finish(env Env, verb string, n int, text string, force bool, code string) error {
	c, err := findTask(env, n)
	if err != nil {
		return err
	}
	w, err := taskWork(env, n)
	if err != nil {
		return err
	}
	repo := codeRepo(env)
	branch := w.Branch()
	var head string
	if branch != "" || len(w.Worktrees) > 0 || code != "" {
		if head, err = repo.Head(); err != nil {
			return err
		}
	}
	into := mainBranch(repo)
	if code != "" {
		given := code
		if code, err = repo.Run("rev-parse", "--verify", "-q", code+"^{commit}"); err != nil {
			return fail(ExitUsage, "--code %s isn't a commit in the code repo", given)
		}
		in, err := repo.IsAncestor(code, head)
		if err != nil {
			return err
		}
		if !in {
			return fail(ExitUsage, "--code %s isn't in %s; name the commit that merged %s", given, into, task.ID(n))
		}
	}
	sha, err := tip(repo, branch)
	if err != nil {
		return err
	}
	if branch != "" && !force && code == "" {
		merged, err := repo.Merged(sha, head)
		if err != nil {
			return err
		}
		if !merged && verb == "drop" {
			return fail(ExitConflict, "%s has commits not in %s; merge them, or drop --force to discard them",
				branch, into)
		}
		if !merged {
			return fail(ExitConflict, "%s not merged into %s; merge first", branch, into)
		}
	}
	then := "done"
	if verb == "drop" {
		then = "drop (or drop --force to discard them)"
	}
	for _, wt := range w.Worktrees {
		if err := checkRemovable(env, wt, force, head, into, then); err != nil {
			return err
		}
	}

	var gone []string // what went besides the task, for the output
	for _, wt := range w.Worktrees {
		if err := removeWorktree(repo, wt, force); err != nil {
			return err
		}
		gone = append(gone, "worktree "+wt.Path)
	}
	if branch != "" {
		// At the commit checked above: a commit made to it since fails this instead of being lost.
		if err := repo.DeleteBranchAt(branch, sha); err != nil {
			return err
		}
		gone = append(gone, "branch "+branch)
		if verb == "done" && code == "" {
			code = head
		}
	}
	suffix := ""
	if code != "" {
		short, err := repo.Run("rev-parse", "--short", code)
		if err != nil {
			return err
		}
		suffix = " (code " + short + ")"
	}
	// Last, so that a refusal commits nothing and a hand edit has the least time to land after it and be lost.
	if err := commitEdits(env, c); err != nil {
		return err
	}
	needers, err := neededBy(env, n)
	if err != nil {
		return err
	}
	paths := []string{relPath(env, c.Path)}
	var released []int
	if verb == "done" {
		if released, err = release(env, n, needers); err != nil {
			return err
		}
		for _, d := range needers {
			paths = append(paths, relPath(env, d.Path))
		}
	}
	if err := os.Remove(c.Path); err != nil {
		return err
	}
	if err := apeRepo(env).Commit(subject(verb, n, text+suffix), paths...); err != nil {
		return err
	}
	printf(env, "%s deleted\n", c.Path)
	if len(gone) > 0 {
		printf(env, "removed %s\n", strings.Join(gone, ", "))
	}
	if len(released) > 0 {
		printf(env, "released: %s\n", task.JoinIDs(released))
	}
	if verb == "drop" && len(needers) > 0 {
		nums := make([]int, len(needers))
		for i, d := range needers {
			nums[i] = d.Num
		}
		printf(env, "still needed by %s: ape set their needs to what replaces it, or drop them\n", task.JoinIDs(nums))
	}
	return nil
}

// neededBy returns the readable tasks whose needs name task n.
func neededBy(env Env, n int) ([]*task.Task, error) {
	tasks, _, err := task.Load(tasksDir(env))
	if err != nil {
		return nil, err
	}
	var needers []*task.Task
	for _, d := range tasks {
		if nums, ok := task.NeedsNums(d.Get("needs")); ok && d.Err == nil && d.Num != n && slices.Contains(nums, n) {
			needers = append(needers, d)
		}
	}
	return needers, nil
}

// release takes n out of each needer's needs, after committing its pending hand edits; one left with no needs and
// status blocked becomes ready. It writes the files (the caller commits them) and returns the tasks made ready.
func release(env Env, n int, needers []*task.Task) ([]int, error) {
	var released []int
	for _, d := range needers {
		if err := commitEdits(env, d); err != nil {
			return nil, err
		}
		nums, _ := task.NeedsNums(d.Get("needs"))
		d.Set("needs", task.JoinIDs(slices.DeleteFunc(nums, func(m int) bool { return m == n })))
		if d.Get("needs") == "" && d.Get("status") == "blocked" {
			d.Set("status", "ready")
			released = append(released, d.Num)
		}
		if err := writeTask(d); err != nil {
			return nil, err
		}
	}
	return released, nil
}

// mainBranch names the main root's branch for messages ("HEAD" if detached).
func mainBranch(code gitx.Repo) string {
	if b, err := code.CurrentBranch(); err == nil && b != "" {
		return b
	}
	return "HEAD"
}
