package commands

import (
	"bytes"
	"os"
	"slices"
	"strings"

	"github.com/AaronCQL/ape/internal/board"
	"github.com/AaronCQL/ape/internal/task"
)

const setUsage = `ape set <id> k=v... (keys: kind pri status area excl needs from; k= deletes k)`

// Set runs `ape set` with its arguments (after the subcommand name). See SPEC.md.
func Set(env Env, args []string) error {
	if len(args) < 2 {
		return usageErr(setUsage, "want a task ID and at least one k=v")
	}
	n, err := parseNum(args[0])
	if err != nil {
		return err
	}
	var keys, values, pairs []string
	for _, a := range args[1:] {
		k, v, ok := strings.Cut(a, "=")
		if !ok {
			return usageErr(setUsage, "%q isn't k=v", a)
		}
		if !slices.Contains(task.Keys, k) {
			return fail(ExitUsage, "unknown key %q; keys are %s", k, strings.Join(task.Keys, " "))
		}
		v = oneLine(v)
		if nums, ok := task.NeedsNums(v); ok && k == "needs" {
			v = task.JoinIDs(nums) // "3 t5" → "T3 T5": ape writes IDs one way
		}
		keys, values, pairs = append(keys, k), append(values, v), append(pairs, k+"="+v)
	}
	return withLock(env, func() error {
		c, err := findTask(env, n)
		if err != nil {
			return err
		}
		if c.Err != nil {
			return fail(ExitUsage, "%s is unreadable: %v; fix the file by hand", task.ID(n), c.Err)
		}
		// Hand edits don't take the lock: the task is parsed from these bytes, and written only if the file
		// still holds them.
		read, err := os.ReadFile(c.Path)
		if err != nil {
			return err
		}
		if c, err = task.Parse(c.Path, read); err != nil {
			return fail(ExitUsage, "%s is unreadable: %v; fix the file by hand", task.ID(n), err)
		}
		for i, k := range keys {
			// Triage is a claim: another agent's set status=triage on a task in triage loses.
			if k == "status" && values[i] == "triage" && c.Get("status") == "triage" {
				return fail(ExitConflict, "%s is already in triage", task.ID(n))
			}
			c.Set(k, values[i])
		}
		if err := checkValues(c); err != nil {
			return err
		}
		if slices.Contains(keys, "needs") {
			if err := checkNeeds(env, c); err != nil {
				return err
			}
		}
		// After the read: an edit landing in between goes into this commit and fails the check below, instead of
		// being folded into set's commit.
		if err := commitEdits(env, c); err != nil {
			return err
		}
		setRead()
		if now, err := os.ReadFile(c.Path); err != nil || !bytes.Equal(now, read) {
			return fail(ExitConflict, "%s changed while ape set ran; run it again", task.ID(n))
		}
		if err := writeTask(c); err != nil {
			return err
		}
		msg := subject("set", n, strings.Join(pairs, " "))
		if err := apeRepo(env).Commit(msg, relPath(env, c.Path)); err != nil {
			return err
		}
		printf(env, "%s\n", c.Path)
		for _, f := range c.Header {
			printf(env, "%s: %s\n", f.Key, f.Value)
		}
		for _, p := range c.Check() {
			printf(env, "warning: %s\n", p)
		}
		return nil
	})
}

// setRead runs between set reading the task and checking it's unchanged; tests make a hand edit there.
var setRead = func() {}

// checkValues rejects a task whose kind, status, pri, excl or needs holds a value outside its set (SPEC.md "Task
// file"). The rest of Check (missing keys, combinations) are warnings: set builds a task up one key at a time.
func checkValues(c *task.Task) error {
	bad := func(key, want string) error {
		return fail(ExitUsage, "%s: bad %s %q; %s", task.ID(c.Num), key, c.Get(key), want)
	}
	if v := c.Get("kind"); v != "" && !slices.Contains(task.Kinds, v) {
		return bad("kind", "kind is one of "+strings.Join(task.Kinds, " "))
	}
	if v := c.Get("status"); v != "" && !slices.Contains(task.Statuses, v) {
		return bad("status", "status is one of "+strings.Join(task.Statuses, " "))
	}
	if v := c.Get("pri"); v != "" && v != "1" && v != "2" && v != "3" {
		return bad("pri", "pri is 1, 2 or 3")
	}
	if v := c.Get("excl"); v != "" && v != "yes" {
		return bad("excl", "excl is yes, or excl= to remove it")
	}
	if v := c.Get("needs"); v != "" && v != "owner" {
		if _, ok := task.NeedsNums(v); !ok {
			return bad("needs", `needs is owner, or task IDs: needs="T3 T5"`)
		}
	}
	return nil
}

// checkNeeds rejects needs naming c itself, a task not open in tasks/ (done, dropped, never filed), or one that
// (through needs) already waits on c: a cycle.
func checkNeeds(env Env, c *task.Task) error {
	nums, ok := task.NeedsNums(c.Get("needs"))
	if !ok {
		return nil // owner, or none
	}
	tasks, _, err := task.Load(tasksDir(env))
	if err != nil {
		return err
	}
	needs := map[int][]int{} // open task → the tasks it needs
	for _, t := range tasks {
		needs[t.Num], _ = task.NeedsNums(t.Get("needs"))
	}
	id := task.ID(c.Num)
	for _, m := range nums {
		if m == c.Num {
			return fail(ExitUsage, "%s can't need itself", id)
		}
		if _, open := needs[m]; open {
			continue
		}
		closed, err := board.Closed(env.ApeDir, []int{m})
		if err != nil {
			return err
		}
		switch closed[m] {
		case "done":
			return fail(ExitUsage, "%s is done; nothing to wait for", task.ID(m))
		case "drop":
			return fail(ExitUsage, "%s was dropped; need what replaces it", task.ID(m))
		}
		return fail(ExitNotFound, "no %s in %s", task.ID(m), tasksDir(env))
	}
	needs[c.Num] = nums
	if path := cycle(c.Num, needs); path != nil {
		return fail(ExitUsage, "needs would loop: %s; drop one of them", strings.ReplaceAll(task.JoinIDs(path), " ", " → "))
	}
	return nil
}

// cycle returns a path n → … → n through needs, nil if there is none.
func cycle(n int, needs map[int][]int) []int {
	seen := map[int]bool{}
	var walk func(m int, path []int) []int
	walk = func(m int, path []int) []int {
		path = append(path, m)
		if m == n && len(path) > 1 {
			return path
		}
		if seen[m] {
			return nil
		}
		seen[m] = true
		for _, k := range needs[m] {
			if p := walk(k, path); p != nil {
				return p
			}
		}
		return nil
	}
	return walk(n, nil)
}
