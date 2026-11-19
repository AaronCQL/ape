// Package board classifies tasks into columns and renders the board (SPEC.md "State" and "Board output").
package board

import (
	"bytes"
	"cmp"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/AaronCQL/ape/framework"
	"github.com/AaronCQL/ape/internal/gitx"
	"github.com/AaronCQL/ape/internal/locate"
	"github.com/AaronCQL/ape/internal/task"
)

// State is everything the board is computed from. Collect fills it; tests build it by hand.
type State struct {
	Tasks       []*task.Task
	Problems    []task.Problem // from task.Load and task.Validate
	Worktrees   []gitx.Worktree
	Branches    []gitx.Branch // local branches under w/
	WorktreeDir string        // where ape start puts worktrees (Resolve's wtDir)
	// LastOwn: a DOING task's worktree path → the time of its HEAD's newest commit not on the main root's HEAD
	// (zero: none).
	LastOwn map[string]time.Time
	// Started: worktree path → when it was created, for those whose LastOwn is zero (the quiet check's age when
	// there's no own commit). Absent if git or the file system can't tell.
	Started  map[string]time.Time
	Done     []gitx.LogEntry // ape repo commits from the last 24 h, newest first
	Outdated bool            // _FRAMEWORK.md differs from the embedded one
	Now      time.Time
	// Conflicts: {a READY task with a branch, a task with a worktree} → what merging their commits conflicts in;
	// only pairs that conflict.
	Conflicts map[[2]int]Conflict
	// MainConflicts: a task with a worktree and own commits → what merging it into the main root's HEAD
	// (MainBranch) conflicts in; only those that conflict.
	MainConflicts map[int]Conflict
	MainBranch    string
	// Dropped: tasks named in a blocked task's needs that are gone because `ape drop` closed them.
	Dropped map[int]bool
}

// Conflict is what a merge of two commits would conflict in (git merge-tree).
type Conflict struct {
	Files       []string // sorted
	Regenerated bool     // every one is linguist-generated (.gitattributes): regenerate, don't resolve
}

// Column is one of the board's sections.
type Column int

const (
	Owner Column = iota
	Doing
	New
	Blocked
	Ready
)

// Row is one task on the board.
type Row struct {
	Task  *task.Task
	Col   Column
	Marks []string // in display order, e.g. "clashes 4", "paused", "12m since commit"
}

// Board is the classified state.
type Board struct {
	Rows  []Row    // grouped by Col (Owner..Ready), each group sorted as SPEC.md says
	Done  []string // rendered DONE lines, without the leading two spaces
	Check []string // rendered CHECK lines, without the leading two spaces
}

// Collect reads the ape dir and both repos into a State. Git calls: a fixed few, plus one per task with a worktree
// (LastOwn: Build reads it only for those), and one more for each of those without an own commit (Started); merge
// checks (Conflicts, MainConflicts: one merge-tree per pair, run in parallel); and one log search when a blocked
// task needs a task that's gone (Dropped).
func Collect(apeDir, mainRoot string, now time.Time) (State, error) {
	s := State{Now: now, LastOwn: map[string]time.Time{}, Started: map[string]time.Time{},
		WorktreeDir: locate.WorktreePath(mainRoot, ""), Conflicts: map[[2]int]Conflict{},
		MainConflicts: map[int]Conflict{}, Dropped: map[int]bool{}}
	var err error
	if s.Tasks, s.Problems, err = task.Load(filepath.Join(apeDir, "tasks")); err != nil {
		return State{}, err
	}
	s.Problems = append(s.Problems, task.Validate(s.Tasks)...)

	code := gitx.Repo{Dir: mainRoot}
	if s.Worktrees, err = code.Worktrees(); err != nil {
		return State{}, err
	}
	if s.Branches, err = code.Branches(BranchPrefix); err != nil {
		return State{}, err
	}
	var head string
	working := map[int]string{} // task with a worktree → its HEAD
	paused := map[int]string{}  // READY task with a branch and no worktree → its branch
	var own []int               // tasks with a worktree and commits of their own: checked against main
	for _, c := range s.Tasks {
		work := Resolve(c.Num, s.WorktreeDir, s.Worktrees, s.Branches)
		w, ok := work.Live()
		if !ok && work.Branch() != "" && get(c, "status") == "ready" {
			paused[c.Num] = "refs/heads/" + work.Branch()
		}
		if !ok || w.Head == "" {
			continue
		}
		working[c.Num] = w.Head
		if head == "" {
			if head, err = code.Head(); err != nil {
				return State{}, err
			}
		}
		if s.LastOwn[w.Path], err = code.LastOwnCommit(w.Head, head); err != nil {
			return State{}, err
		}
		if s.LastOwn[w.Path].IsZero() {
			if t, ok := worktreeCreated(w.Path); ok {
				s.Started[w.Path] = t
			}
		} else {
			own = append(own, c.Num)
		}
	}
	if err := s.collectConflicts(code, head, own, working, paused); err != nil {
		return State{}, err
	}
	if err := s.collectDropped(apeDir); err != nil {
		return State{}, err
	}

	if s.Done, err = (gitx.Repo{Dir: apeDir}).Log(now.Add(-24 * time.Hour)); err != nil {
		return State{}, err
	}
	// Unreadable or missing counts as outdated: init rewrites it either way.
	fw, err := os.ReadFile(filepath.Join(apeDir, "_FRAMEWORK.md"))
	s.Outdated = err != nil || !bytes.Equal(fw, framework.FrameworkMD())
	return s, nil
}

// collectConflicts fills Conflicts (each paused READY task against each task with a worktree) and MainConflicts
// (each of own, against head), running the merges in parallel.
func (s *State) collectConflicts(code gitx.Repo, head string, own []int, working, paused map[int]string) error {
	type job struct {
		a, b string
		key  [2]int // {0, n}: n against main
		c    Conflict
		err  error
	}
	var jobs []*job
	for _, n := range own {
		jobs = append(jobs, &job{a: head, b: working[n], key: [2]int{0, n}})
	}
	for r, br := range paused {
		for n, h := range working {
			jobs = append(jobs, &job{a: br, b: h, key: [2]int{r, n}})
		}
	}
	if len(own) > 0 {
		b, err := code.CurrentBranch()
		if err != nil {
			return err
		}
		s.MainBranch = cmp.Or(b, "HEAD")
	}
	var wg sync.WaitGroup
	sem := make(chan struct{}, 8)
	for _, j := range jobs {
		wg.Go(func() {
			sem <- struct{}{}
			defer func() { <-sem }()
			if j.c.Files, j.err = code.Conflicts(j.a, j.b); j.err == nil && len(j.c.Files) > 0 {
				j.c.Regenerated, j.err = code.Generated(j.c.Files)
			}
		})
	}
	wg.Wait()
	for _, j := range jobs {
		switch {
		case j.err != nil:
			return j.err
		case len(j.c.Files) == 0:
		case j.key[0] == 0:
			s.MainConflicts[j.key[1]] = j.c
		default:
			s.Conflicts[j.key] = j.c
		}
	}
	return nil
}

// collectDropped fills Dropped: of the tasks blocked tasks need, those gone because they were dropped.
func (s *State) collectDropped(apeDir string) error {
	open := map[int]bool{}
	for _, c := range s.Tasks {
		open[c.Num] = true
	}
	var gone []int
	for _, c := range s.Tasks {
		if nums, ok := task.NeedsNums(get(c, "needs")); ok && get(c, "status") == "blocked" {
			for _, n := range nums {
				if !open[n] {
					gone = append(gone, n)
				}
			}
		}
	}
	closed, err := Closed(apeDir, gone)
	for n, verb := range closed {
		s.Dropped[n] = verb == "drop"
	}
	return err
}

// Closed returns how each of nums, tasks no longer in tasks/, was closed: "done" or "drop", from the newest such
// commit in the ape repo; absent if none (never filed, or deleted by hand). One git call; none for no nums.
func Closed(apeDir string, nums []int) (map[int]string, error) {
	closed := map[int]string{}
	if len(nums) == 0 {
		return closed, nil
	}
	alt := make([]string, len(nums))
	for i, n := range nums {
		alt[i] = strconv.Itoa(n)
	}
	repo := gitx.Repo{Dir: apeDir}
	if ok, err := repo.Run("rev-parse", "-q", "--verify", "HEAD^{commit}"); ok == "" || err != nil {
		return closed, nil // no commits yet
	}
	out, err := repo.Run("log", "--format=%s", "-E", `--grep=^(done|drop)\([Tt]?0*(`+strings.Join(alt, "|")+`)\):`,
		"HEAD", "--")
	if err != nil {
		return nil, err
	}
	for line := range strings.Lines(out) {
		verb, rest, _ := strings.Cut(line, "(")
		id, _, _ := strings.Cut(rest, ")")
		if n, ok := task.ParseID(id); ok && closed[n] == "" {
			closed[n] = verb // newest first: numbers are never reused, so this is how it ended
		}
	}
	return closed, nil
}

// worktreeCreated returns when the linked worktree at path was created: the mtime of `commondir` in its admin dir
// (<common dir>/worktrees/<name>, from `git rev-parse --git-dir` there). git writes that file once, at
// `worktree add`; the admin dir's own mtime moves with every index or HEAD write. ok is false for a main
// worktree or one git can't read (deleted by hand).
func worktreeCreated(path string) (time.Time, bool) {
	gitDir, err := gitx.Repo{Dir: path}.Run("rev-parse", "--path-format=absolute", "--git-dir")
	if err != nil {
		return time.Time{}, false
	}
	fi, err := os.Stat(filepath.Join(gitDir, "commondir"))
	if err != nil {
		return time.Time{}, false
	}
	return fi.ModTime(), true
}
