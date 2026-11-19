package commands

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/AaronCQL/ape/internal/board"
)

// The board end to end: board.Collect on real repos, then Build and Render.
func TestBoard(t *testing.T) {
	p := newWtProject(t)
	p.task("T2-signup-form", "Signup form", "kind: feat", "pri: 1", "status: ready", "area: web/")
	p.task("T3-pricing", "Approve the pricing page copy\n## Ask\nShip the shorter copy? rec: yes.",
		"kind: owner", "pri: 1", "status: ready")
	p.task("T4-oauth", "OAuth login", "kind: feat", "pri: 1", "status: ready", "area: src/auth")
	p.task("T5-session", "Session expires early", "kind: bug", "pri: 1", "status: ready", "area: src/", "excl: yes")
	p.task("T9-runner", "Upgrade the test runner", "kind: chore", "pri: 2", "status: ready", "area: tools/")
	p.task("T11-dark", "Dark mode flickers", "kind: bug", "status: new", "from: web@3f2a9c1")
	p.task("T12-wait", "Waits on 11", "kind: feat", "pri: 2", "status: blocked", "area: x/", "needs: 11")
	p.task("T13-orphan", "Waits on a gone task", "kind: feat", "pri: 3", "status: blocked", "area: y/", "needs: 99")

	p.mustRun(Start, "4")
	p.mustRun(Start, "9")
	runner := p.wt("T9-runner")
	p.write(runner, "tools/run.sh", "echo\n")
	p.git(runner, "add", "-A")
	p.git(runner, "commit", "-q", "-m", "runner")
	p.mustRun(Pause, "9")
	p.git(p.root, "branch", "ape/T31-foo")
	p.git(p.ape, "rm", "-q", "tasks/T2-signup-form.md")
	p.git(p.ape, "commit", "-q", "-m", "done(T2): form + validation (code 8c1d2e4)")

	want := `ape: 7 tasks · 1 owner · 1 doing · 1 new · 2 blocked · 2 ready · 1 done 24h
OWNER
  T3   owner p1  Approve the pricing page copy
       ? Ship the shorter copy? rec: yes.
DOING
  T4   feat  p1  OAuth login   [no commits]
NEW
  T11  bug       Dark mode flickers   [from web@3f2a9c1]
BLOCKED
  T12  feat  p2  Waits on 11   [needs T11]
  T13  feat  p3  Waits on a gone task   [needs T99 (gone)]
READY
  T5   bug   p1  Session expires early   [clashes T4]
  T9   chore p2  Upgrade the test runner   [paused]
DONE 24h
  T2   done  form + validation (code 8c1d2e4)
CHECK
  ape/T31-foo: branch without a task
  T13: needs T99: all done; ape set T13 status=ready needs=
`
	if got := p.mustRun(Board); got != want {
		t.Fatalf("board:\n%s\nwant:\n%s", got, want)
	}

	// A commit on the DOING branch, 4 h before "now": its age, and quiet in CHECK.
	auth := p.wt("T4-oauth")
	at := time.Date(2026, 1, 2, 3, 4, 0, 0, time.UTC)
	t.Setenv("GIT_COMMITTER_DATE", at.Format(time.RFC3339))
	p.git(auth, "commit", "-q", "--allow-empty", "-m", "wip")
	s, err := board.Collect(p.ape, p.root, at.Add(4*time.Hour))
	if err != nil {
		t.Fatal(err)
	}
	if got := s.LastOwn[auth]; !got.Equal(at) {
		t.Fatalf("LastOwn = %v, want %v", got, at)
	}
	got := board.Render(board.Build(s))
	for _, line := range []string{
		"  T4   feat  p1  OAuth login   [4h since commit]",
		"  T4: quiet 4h (worktree, no commit)",
	} {
		if !slices.Contains(strings.Split(got, "\n"), line) {
			t.Fatalf("board lacks %q:\n%s", line, got)
		}
	}

	// Bad labels and an outdated (then missing) _FRAMEWORK.md show in CHECK; the board still renders.
	p.write(p.ape, "tasks/T14-bad.md", "---\nkind: nope\nstatus: ready\n---\n# Bad\n")
	fw := filepath.Join(p.ape, "_FRAMEWORK.md")
	p.write(p.ape, "_FRAMEWORK.md", "old\n")
	got = p.mustRun(Board)
	if !strings.Contains(got, "\n  T14: ") || !strings.HasSuffix(got, "\n  _FRAMEWORK.md: outdated; run ape init\n") {
		t.Fatalf("board:\n%s", got)
	}
	os.Remove(fw)
	if s, err := board.Collect(p.ape, p.root, time.Now()); err != nil || !s.Outdated {
		t.Fatalf("missing _FRAMEWORK.md: Outdated = %v, %v", s.Outdated, err)
	}

	_, err = p.run(Board, "extra")
	wantExit(t, err, ExitUsage, "")
}

// Both repos without commits (a fresh `git init` + `ape init` before its commit): an empty board, no error.
func TestBoardEmpty(t *testing.T) {
	p := newWtProject(t)
	p.git(p.root, "update-ref", "-d", "HEAD")
	p.git(p.ape, "update-ref", "-d", "HEAD")
	if got, want := p.mustRun(Board), "ape: 0 tasks · 0 owner · 0 doing · 0 new · 0 blocked · 0 ready · 0 done 24h\n"; got != want {
		t.Fatalf("board = %q, want %q", got, want)
	}
}
