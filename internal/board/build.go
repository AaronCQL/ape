package board

import (
	"cmp"
	"fmt"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/AaronCQL/ape/internal/task"
)

// quietAfter is how long a DOING task's branch may go without a commit before CHECK says so.
const quietAfter = 3 * time.Hour

// Build classifies s. Pure: no I/O.
func Build(s State) Board {
	byNum := map[int]bool{}
	for _, c := range s.Tasks {
		byNum[c.Num] = true
	}

	var check []string
	for _, p := range s.Problems {
		if p.Num == 0 {
			check = append(check, p.File+": "+p.Msg)
		} else {
			check = append(check, task.ID(p.Num)+": "+p.Msg)
		}
	}

	// Worktrees and branches without a task; a task's own are Resolve's.
	inWorktree := map[string]bool{}
	for _, w := range s.Worktrees {
		n, ok := BranchNum(w.Branch)
		if !ok {
			continue
		}
		inWorktree[w.Branch] = true
		if !byNum[n] {
			check = append(check, w.Branch+": worktree without a task")
		}
	}
	for _, br := range s.Branches {
		if n, ok := BranchNum(br.Name); ok && !inWorktree[br.Name] && !byNum[n] {
			check = append(check, br.Name+": branch without a task")
		}
	}

	var rows []Row
	var taskChecks []string
	paused := map[int]bool{}
	working := map[int]bool{} // has a worktree: DOING, or OWNER marked doing
	for _, c := range s.Tasks {
		r := Row{Task: c}
		status, needs := get(c, "status"), get(c, "needs")
		needsNums, needsTasks := task.NeedsNums(needs)
		work := Resolve(c.Num, s.WorktreeDir, s.Worktrees, s.Branches)
		if len(work.Branches) > 1 {
			taskChecks = append(taskChecks, fmt.Sprintf("%s: several branches: %s; delete the stale one",
				task.ID(c.Num), strings.Join(work.Branches, ", ")))
		}
		for _, w := range work.Worktrees {
			if w.Prunable {
				taskChecks = append(taskChecks, fmt.Sprintf("%s: worktree %s missing; ape pause %s prunes it",
					task.ID(c.Num), w.Path, task.ID(c.Num)))
			}
		}
		live, doing := work.Live()
		paused[c.Num] = !doing && len(work.Branches) > 0
		kind := get(c, "kind")
		var since string // DOING's mark
		if doing {
			working[c.Num] = true
			if last := s.LastOwn[live.Path]; last.IsZero() {
				since = "no commits"
				if start, ok := s.Started[live.Path]; ok && s.Now.Sub(start) >= quietAfter {
					taskChecks = append(taskChecks, fmt.Sprintf("%s: quiet %s (no commits since start)",
						task.ID(c.Num), age(s.Now.Sub(start))))
				}
			} else {
				d := s.Now.Sub(last)
				since = age(d) + " since commit"
				if d >= quietAfter {
					taskChecks = append(taskChecks, fmt.Sprintf("%s: quiet %s (worktree, no commit)", task.ID(c.Num), age(d)))
				}
			}
		}
		switch {
		case doing && (needs != "owner" || kind == "owner"):
			r.Col = Doing
			r.Marks = []string{since}
			if mc, ok := s.MainConflicts[c.Num]; ok {
				r.Marks = append(r.Marks, conflictMark(cmp.Or(s.MainBranch, "HEAD"), mc))
			}
		case needs == "owner" || kind == "owner":
			// Includes an agent's own task waiting on the owner's answer (T10).
			r.Col = Owner
			if c.Ask == "" {
				r.Marks = []string{"needs owner"}
			}
			if doing {
				r.Marks = append(r.Marks, "doing")
			}
		case status == "new" || status == "triage":
			r.Col = New
			if status == "triage" {
				r.Marks = append(r.Marks, "triage")
			}
			if from := get(c, "from"); from != "" {
				r.Marks = append(r.Marks, "from "+from)
			}
		case status == "blocked" && needsTasks:
			// The mark lists the tasks still open; none left → all of them, for CHECK's hint.
			r.Col = Blocked
			all, open := task.JoinIDs(needsNums), task.JoinIDs(slices.DeleteFunc(slices.Clone(needsNums),
				func(n int) bool { return !byNum[n] }))
			// A dropped task never landed: what needed it waits until the orchestrator points it elsewhere.
			dropped := false
			for _, n := range needsNums {
				if !byNum[n] && s.Dropped[n] {
					dropped = true
					taskChecks = append(taskChecks, fmt.Sprintf("%[1]s: needs %[2]s, dropped; ape set %[1]s needs=… (what replaces it), or drop %[1]s",
						task.ID(c.Num), task.ID(n)))
				}
			}
			switch {
			case open != "":
				r.Marks = []string{"needs " + open}
			case dropped:
				r.Marks = []string{"needs " + all + " (gone)"}
			default:
				r.Marks = []string{"needs " + all + " (gone)"}
				taskChecks = append(taskChecks, fmt.Sprintf(
					"%s: needs %s: all done; ape set %s status=ready needs=", task.ID(c.Num), all, task.ID(c.Num)))
			}
		case status == "ready":
			r.Col = Ready
		default:
			// Labels too broken to place (missing or unknown status, blocked without a usable needs): CHECK
			// already lists the problem; NEW is where triage fixes it.
			r.Col = New
		}
		rows = append(rows, r)
	}
	check = append(check, taskChecks...)
	if s.Outdated {
		check = append(check, "_FRAMEWORK.md: outdated; run ape init")
	}

	// A READY task against every task with a worktree: areas overlapping with either one excl clash; else, when both
	// have a branch, a real merge conflict (outside regenerated files) shows.
	for i := range rows {
		r := &rows[i]
		if r.Col != Ready {
			continue
		}
		area, excl := strings.Fields(get(r.Task, "area")), get(r.Task, "excl") == "yes"
		var with []int
		var conflicts []string
		for _, d := range rows { // rows are in number order here
			n := d.Task.Num
			switch {
			case !working[n]:
			case (excl || get(d.Task, "excl") == "yes") && Clash(area, strings.Fields(get(d.Task, "area"))):
				with = append(with, n)
			default:
				if mc, ok := s.Conflicts[[2]int{r.Task.Num, n}]; ok && !mc.Regenerated {
					conflicts = append(conflicts, conflictMark(task.ID(n), mc))
				}
			}
		}
		with = slices.Compact(with)
		if len(with) > 0 {
			r.Marks = append(r.Marks, "clashes "+strings.ReplaceAll(task.JoinIDs(with), " ", ", "))
		}
		r.Marks = append(r.Marks, conflicts...)
		if paused[r.Task.Num] {
			r.Marks = append(r.Marks, "paused")
		}
	}

	slices.SortStableFunc(rows, func(a, b Row) int {
		if a.Col != b.Col {
			return int(a.Col) - int(b.Col)
		}
		if pa, pb := priKey(a.Task), priKey(b.Task); pa != pb {
			return pa - pb
		}
		return a.Task.Num - b.Task.Num
	})

	var done []string
	for _, e := range s.Done {
		if l, ok := doneLine(e.Subject); ok {
			done = append(done, l)
		}
	}
	return Board{Rows: rows, Done: done, Check: check}
}

// shownFiles is how many conflicting files a mark names; the rest are counted.
const shownFiles = 3

// conflictMark renders a Conflict with what ("T4", "main"): "conflicts T4: a.go, b.go, c.go, +2 more", or for
// regenerated files only "conflicts main in regenerated files only: x".
func conflictMark(with string, c Conflict) string {
	files := c.Files
	more := ""
	if len(files) > shownFiles {
		files, more = files[:shownFiles], fmt.Sprintf(", +%d more", len(c.Files)-shownFiles)
	}
	what := "conflicts " + with
	if c.Regenerated {
		what += " in regenerated files only"
	}
	return what + ": " + strings.Join(files, ", ") + more
}

// Clash reports whether two area token lists clash (SPEC.md "State").
func Clash(a, b []string) bool {
	for _, x := range a {
		for _, y := range b {
			x, y := strings.TrimRight(x, "/"), strings.TrimRight(y, "/")
			if x == "" || y == "" {
				continue
			}
			if x == "." || y == "." || x == y || strings.HasPrefix(x, y+"/") || strings.HasPrefix(y, x+"/") {
				return true
			}
		}
	}
	return false
}

// BranchPrefix starts every branch ape creates: ape/T12-slug.
const BranchPrefix = "ape/"

// BranchNum parses the task number from "ape/T12-slug"; ok is false for any other branch. A worktree on such a
// branch makes T12 DOING (SPEC.md "State").
func BranchNum(branch string) (int, bool) {
	rest, ok := strings.CutPrefix(branch, BranchPrefix)
	if !ok {
		return 0, false
	}
	id, slug, ok := strings.Cut(rest, "-")
	if !ok || slug == "" || !strings.HasPrefix(id, "T") {
		return 0, false
	}
	return task.ParseID(id)
}

// atoi parses a non-negative decimal number with nothing else around it.
func atoi(s string) (int, bool) {
	if s == "" || strings.Trim(s, "0123456789") != "" {
		return 0, false
	}
	n, err := strconv.Atoi(s)
	return n, err == nil
}

// get returns c's first header value for key, "" if absent.
func get(c *task.Task, key string) string {
	for _, f := range c.Header {
		if f.Key == key {
			return f.Value
		}
	}
	return ""
}

// priKey orders by pri ascending, no (or unparsable) pri last.
func priKey(c *task.Task) int {
	if n, ok := atoi(get(c, "pri")); ok {
		return n
	}
	return 1 << 30
}

// age renders d as Nm under an hour, Nh under 48 h, else Nd.
func age(d time.Duration) string {
	switch {
	case d < 0:
		return "0m"
	case d < time.Hour:
		return fmt.Sprintf("%dm", int(d/time.Minute))
	case d < 48*time.Hour:
		return fmt.Sprintf("%dh", int(d/time.Hour))
	default:
		return fmt.Sprintf("%dd", int(d/(24*time.Hour)))
	}
}

// doneLine renders a "done(T12): text" or "drop(T12): text" subject; ok is false for any other subject.
func doneLine(subject string) (string, bool) {
	verb, rest, ok := strings.Cut(subject, "(")
	if !ok || verb != "done" && verb != "drop" {
		return "", false
	}
	id, text, ok := strings.Cut(rest, "):")
	n, idOK := task.ParseID(id)
	if !ok || !idOK {
		return "", false
	}
	return strings.TrimRight(fmt.Sprintf("%-4s %-5s %s", task.ID(n), verb, strings.TrimSpace(text)), " "), true
}
