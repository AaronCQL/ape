package commands

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// boardHas fails unless the board has every line in lines.
func (p *wtProject) boardHas(lines ...string) {
	p.t.Helper()
	got := p.mustRun(Board)
	for _, l := range lines {
		if !strings.Contains("\n"+got, "\n"+l+"\n") {
			p.t.Fatalf("board lacks %q:\n%s", l, got)
		}
	}
}

// A task renamed after start keeps its branch and worktree: every command finds them by number.
func TestRenamedTask(t *testing.T) {
	p := newWtProject(t)
	p.task("T12-old", "Old", "kind: feat", "pri: 1", "status: ready", "area: src/")
	p.mustRun(Start, "12")
	wt := p.wt("T12-old")
	p.git(p.ape, "mv", "tasks/T12-old.md", "tasks/T12-new.md")
	p.git(p.ape, "commit", "-q", "-m", "rename")

	p.boardHas("DOING", "  T12  feat  p1  Old   [no commits]")
	_, err := p.run(Start, "12")
	wantExit(t, err, ExitConflict, "taken: worktree "+wt)
	if out := p.mustRun(Pause, "12"); out != "paused T12; branch ape/T12-old kept\n" {
		t.Fatalf("pause: %q", out)
	}
	if out := p.mustRun(Start, "12"); out != wt+"\nbranch ape/T12-old\n" {
		t.Fatalf("resume: %q", out)
	}
	p.git(wt, "commit", "-q", "--allow-empty", "-m", "work")
	p.git(p.root, "merge", "-q", "--ff-only", "ape/T12-old")
	out := p.mustRun(Done, "12", "ok")
	if !strings.HasSuffix(out, "\nremoved worktree "+wt+", branch ape/T12-old\n") || pathExists(wt) {
		t.Fatalf("done: %q", out)
	}
}

func TestSeveralBranches(t *testing.T) {
	p := newWtProject(t)
	p.task("T12-a", "A", "kind: feat", "pri: 1", "status: ready", "area: src/")
	p.git(p.root, "branch", "ape/T12-a")
	p.git(p.root, "branch", "ape/T12-b")
	want := "T12 has several branches: ape/T12-a, ape/T12-b; delete the stale one"
	for _, c := range []struct {
		cmd  func(Env, []string) error
		args []string
	}{{Start, []string{"12"}}, {Pause, []string{"12"}}, {Done, []string{"12", "x"}}, {Drop, []string{"12", "x"}}} {
		_, err := p.run(c.cmd, c.args...)
		wantExit(t, err, ExitConflict, want)
	}
	p.boardHas("  T12: several branches: ape/T12-a, ape/T12-b; delete the stale one")
	if !pathExists(filepath.Join(p.ape, "tasks/T12-a.md")) {
		t.Fatal("task gone")
	}
}

// A worktree detached mid-rebase still makes its task DOING, and its commits aren't lost to pause or done.
func TestDetachedWorktree(t *testing.T) {
	p := newWtProject(t)
	p.task("T12-x", "X", "kind: feat", "pri: 1", "status: ready", "area: src/")
	p.mustRun(Start, "12")
	wt := p.wt("T12-x")
	commit := func(file, msg string) {
		p.write(wt, file, msg+"\n")
		p.git(wt, "add", file)
		p.git(wt, "commit", "-q", "-m", msg)
	}
	commit("src/a.go", "on branch")
	p.git(wt, "checkout", "-q", "--detach")
	commit("src/b.go", "rebased, not on the branch yet")

	p.boardHas("DOING", "  T12  feat  p1  X   [0m since commit]")
	_, err := p.run(Start, "12")
	wantExit(t, err, ExitConflict, "taken: worktree "+wt)
	_, err = p.run(Pause, "12")
	wantExit(t, err, ExitConflict, "worktree "+wt+" is detached at commits not in ape/T12-x; finish the rebase "+
		"(or check out its branch), then pause")
	p.git(p.root, "merge", "-q", "--ff-only", "ape/T12-x")
	_, err = p.run(Done, "12", "ok")
	wantExit(t, err, ExitConflict, "worktree "+wt+" is detached at commits not in main; finish the rebase "+
		"(or check out its branch), then done")
	if !pathExists(wt) || p.git(p.root, "branch", "--list", "ape/T12-x") == "" {
		t.Fatal("a refused done removed something")
	}

	p.git(wt, "branch", "-f", "ape/T12-x", "HEAD")
	p.git(wt, "checkout", "-q", "ape/T12-x")
	p.git(wt, "checkout", "-q", "--detach") // detached within its branch: nothing to lose
	p.mustRun(Pause, "12")
	if pathExists(wt) {
		t.Fatal("worktree left")
	}
}

// A worktree deleted by hand: not DOING, CHECK says so; start prunes it and starts again.
func TestDeletedWorktree(t *testing.T) {
	p := newWtProject(t)
	p.task("T7-z", "Z", "kind: chore", "pri: 3", "status: ready", "area: docs/")
	p.mustRun(Start, "7")
	wt := p.wt("T7-z")
	if err := os.RemoveAll(wt); err != nil {
		t.Fatal(err)
	}
	p.boardHas("READY", "  T7   chore p3  Z   [paused]", "  T7: worktree "+wt+" missing; ape pause T7 prunes it")
	if out := p.mustRun(Start, "7"); out != wt+"\nbranch ape/T7-z\n" {
		t.Fatalf("start: %q", out)
	}
	p.boardHas("DOING")
}

// git's own clean check follows status.showUntrackedFiles; ape's doesn't, so untracked work is never deleted.
func TestUntrackedHiddenByConfig(t *testing.T) {
	p := newWtProject(t)
	p.git(p.root, "config", "status.showUntrackedFiles", "no")
	p.task("T5-x", "X", "kind: bug", "pri: 1", "status: ready", "area: src/")
	p.mustRun(Start, "5")
	wt := p.wt("T5-x")
	p.write(wt, "notes.txt", "precious\n")

	_, err := p.run(Pause, "5")
	wantExit(t, err, ExitConflict, "worktree "+wt+" has uncommitted changes; commit or stash, then pause")
	_, err = p.run(Done, "5", "ok")
	wantExit(t, err, ExitConflict, "worktree "+wt+" has uncommitted changes; commit or stash, then done")
	_, err = p.run(Drop, "5", "no")
	wantExit(t, err, ExitConflict, "worktree "+wt+" has uncommitted changes; commit or stash, then drop "+
		"(or drop --force to discard them)")
	if !pathExists(filepath.Join(wt, "notes.txt")) {
		t.Fatal("untracked file deleted")
	}
}

// done deletes the branch at the commit it checked against main, whatever its upstream says.
func TestDoneIgnoresUpstream(t *testing.T) {
	p := newWtProject(t)
	p.task("T3-x", "X", "kind: feat", "pri: 1", "status: ready", "area: src/")
	p.git(p.root, "branch", "up")
	p.mustRun(Start, "3")
	wt := p.wt("T3-x")
	p.git(wt, "commit", "-q", "--allow-empty", "-m", "work")
	p.git(p.root, "branch", "-q", "-u", "up", "ape/T3-x")
	p.git(p.root, "merge", "-q", "--ff-only", "ape/T3-x")
	p.mustRun(Done, "3", "ok")
	if p.git(p.root, "branch", "--list", "ape/*") != "" || pathExists(filepath.Join(p.ape, "tasks/T3-x.md")) {
		t.Fatal("branch or task left")
	}
}

func TestLockedWorktree(t *testing.T) {
	p := newWtProject(t)
	p.task("T4-x", "X", "kind: feat", "pri: 1", "status: ready", "area: src/")
	p.mustRun(Start, "4")
	wt := p.wt("T4-x")
	p.git(p.root, "worktree", "lock", wt)
	want := "worktree " + wt + " is locked; git worktree unlock it"
	_, err := p.run(Pause, "4")
	wantExit(t, err, ExitConflict, want)
	_, err = p.run(Drop, "4", "x", "--force")
	wantExit(t, err, ExitConflict, want)
	_, err = p.run(Done, "4", "x")
	wantExit(t, err, ExitConflict, want)
	if !pathExists(filepath.Join(wt, "ape")) || p.git(p.root, "branch", "--list", "ape/T4-x") == "" {
		t.Fatal("a refusal removed something")
	}
}

// A branch checked out in the main worktree: taken; pause, done and drop refuse.
func TestBranchInMainWorktree(t *testing.T) {
	p := newWtProject(t)
	p.task("T8-x", "X", "kind: feat", "pri: 1", "status: ready", "area: src/")
	p.git(p.root, "checkout", "-q", "-b", "ape/T8-x")
	_, err := p.run(Start, "8")
	wantExit(t, err, ExitConflict, "taken: worktree "+p.root)
	_, err = p.run(Pause, "8")
	wantExit(t, err, ExitConflict, "ape/T8-x is checked out in the main worktree "+p.root+
		"; check out another branch there first")
	if pathExists(p.root + "-wt") {
		t.Fatal("start made a worktree dir")
	}
}

func TestStartNoCommits(t *testing.T) {
	p := newWtProject(t)
	p.task("T1-x", "X", "kind: feat", "pri: 1", "status: ready", "area: src/")
	p.git(p.root, "checkout", "-q", "--orphan", "fresh")
	_, err := p.run(Start, "1")
	wantExit(t, err, ExitUsage, "code repo has no commits; commit first")
	if pathExists(p.root + "-wt") {
		t.Fatal("left a -wt dir")
	}
}

// set, done and drop first commit the task's own hand edits, alone, as "edit N slug"; other dirty files stay so.
func TestEditedTask(t *testing.T) {
	p := newWtProject(t)
	p.task("T2-x", "X", "kind: feat", "pri: 1", "status: ready", "area: src/")
	p.task("T4-z", "Z", "kind: feat")
	p.task("T5-v", "V", "kind: feat")
	log := func() string { return p.git(p.ape, "log", "--format=%s", "-3") }
	show := func(rev string) string { return p.git(p.ape, "show", "--format=", "--name-status", rev) }
	p.write(p.ape, "tasks/T2-x.md", "---\nkind: feat\npri: 1\nstatus: ready\narea: src/\n---\n# X\nbody\n")
	p.write(p.ape, "PROJECT.md", "dirty\n")
	p.write(p.ape, "tasks/T4-z.md", "---\nkind: feat\n---\n# Z\nother task\n")

	p.mustRun(Set, "2", "pri=2")
	if got, want := log(), "set(T2): pri=2\nedit(T2): hand edits\ntask T5-v"; got != want {
		t.Fatalf("log:\n%s\nwant:\n%s", got, want)
	}
	if got := p.git(p.ape, "show", "HEAD~:tasks/T2-x.md"); !strings.Contains(got, "pri: 1") ||
		!strings.HasSuffix(got, "# X\nbody") {
		t.Fatalf("edit commit holds %q", got)
	}
	if got := show("HEAD~"); got != "M\ttasks/T2-x.md" {
		t.Fatalf("edit commit touches %q", got)
	}
	if got := p.git(p.ape, "status", "--porcelain"); got != "M tasks/T4-z.md\n?? PROJECT.md" {
		t.Fatalf("status %q", got)
	}

	// Nothing pending: no edit commit.
	p.mustRun(Set, "2", "pri=3")
	if got := p.git(p.ape, "log", "--format=%s", "-2"); got != "set(T2): pri=3\nset(T2): pri=2" {
		t.Fatalf("log:\n%s", got)
	}

	// An untracked task (made by hand) is added by its edit commit.
	p.write(p.ape, "tasks/T3-y.md", "---\nstatus: new\n---\n# Y\n")
	p.mustRun(Set, "3", "kind=bug")
	if got := p.git(p.ape, "log", "--format=%s", "-2"); got != "set(T3): kind=bug\nedit(T3): hand edits" {
		t.Fatalf("log:\n%s", got)
	}

	// done and drop: the edits are in history, then the task goes.
	for _, c := range []struct {
		cmd           func(Env, []string) error
		n, verb, name string
	}{
		{Done, "T2", "done", "T2-x"}, {Drop, "t5", "drop", "T5-v"},
	} {
		p.write(p.ape, "tasks/"+c.name+".md", "---\nkind: feat\n---\n# T\n## Progress\nhalf\n")
		p.mustRun(c.cmd, c.n, "why")
		id := strings.ToUpper(c.n)
		if got, want := p.git(p.ape, "log", "--format=%s", "-2"), c.verb+"("+id+"): why\nedit("+id+"): hand edits"; got != want {
			t.Fatalf("log:\n%s\nwant:\n%s", got, want)
		}
		if got := p.git(p.ape, "show", "HEAD~:tasks/"+c.name+".md"); !strings.HasSuffix(got, "## Progress\nhalf") {
			t.Fatalf("edit commit holds %q", got)
		}
	}
	if got := p.git(p.ape, "status", "--porcelain"); got != "M tasks/T4-z.md\n?? PROJECT.md" {
		t.Fatalf("status %q", got)
	}
}

// A refused set or done commits nothing, the edit included.
func TestEditedTaskRefused(t *testing.T) {
	p := newWtProject(t)
	p.task("T2-x", "X", "kind: feat", "pri: 1", "status: ready", "area: src/")
	p.mustRun(Start, "2")
	p.write(p.wt("T2-x"), "src/new.go", "package main\n")
	p.git(p.wt("T2-x"), "add", "-A")
	p.git(p.wt("T2-x"), "commit", "-q", "-m", "work")
	p.write(p.ape, "tasks/T2-x.md", "---\nkind: feat\n---\n# X\nedited\n")
	head := p.git(p.ape, "rev-parse", "HEAD")
	_, err := p.run(Set, "2", "pri=7")
	wantExit(t, err, ExitUsage, `T2: bad pri "7"; pri is 1, 2 or 3`)
	_, err = p.run(Done, "2", "x")
	wantExit(t, err, ExitConflict, "ape/T2-x not merged into main; merge first")
	if got := p.git(p.ape, "rev-parse", "HEAD"); got != head {
		t.Fatal("refused done committed")
	}
	if got := p.git(p.ape, "status", "--porcelain"); got != "M tasks/T2-x.md" {
		t.Fatalf("status %q", got)
	}
}

func TestSetUnreadable(t *testing.T) {
	p := newWtProject(t)
	p.task("T2-x", "X", "kind: feat")
	p.write(p.ape, "tasks/T2-x.md", "no header\n")
	p.git(p.ape, "commit", "-q", "-am", "break it")
	_, err := p.run(Set, "2", "pri=2")
	var e *Error
	if wantExit(t, err, ExitUsage, ""); !errors.As(err, &e) || !strings.HasPrefix(e.Msg, "T2 is unreadable: ") ||
		!strings.HasSuffix(e.Msg, "; fix the file by hand") {
		t.Fatalf("err = %v", err)
	}
}

// A hand edit between set reading the task and writing it isn't overwritten.
func TestSetTaskChanged(t *testing.T) {
	p := newWtProject(t)
	p.task("T2-x", "X", "kind: feat")
	path := filepath.Join(p.ape, "tasks/T2-x.md")
	setRead = func() { os.WriteFile(path, []byte("---\nkind: bug\n---\n# X\n"), 0o644) }
	defer func() { setRead = func() {} }()
	_, err := p.run(Set, "2", "pri=2")
	wantExit(t, err, ExitConflict, "T2 changed while ape set ran; run it again")
	if got, _ := os.ReadFile(path); string(got) != "---\nkind: bug\n---\n# X\n" {
		t.Fatalf("task %q", got)
	}
}
