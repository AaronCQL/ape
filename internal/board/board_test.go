package board

import (
	"flag"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/AaronCQL/ape/internal/gitx"
	"github.com/AaronCQL/ape/internal/task"
)

var update = flag.Bool("update", false, "rewrite testdata/*.golden")

var now = time.Date(2026, 11, 12, 12, 0, 0, 0, time.UTC)

// mk builds a task; kv alternates header keys and values, a later value replacing an earlier one.
func mk(num int, title string, kv ...string) *task.Task {
	c := &task.Task{Num: num, Slug: "s", Title: title}
next:
	for i := 0; i+1 < len(kv); i += 2 {
		for j := range c.Header {
			if c.Header[j].Key == kv[i] {
				c.Header[j].Value = kv[i+1]
				continue next
			}
		}
		c.Header = append(c.Header, task.Field{Key: kv[i], Value: kv[i+1]})
	}
	return c
}

func withAsk(c *task.Task, ask string) *task.Task { c.Ask = ask; return c }

func TestClash(t *testing.T) {
	for _, tc := range []struct {
		a, b string
		want bool
	}{
		{"auth", "auth", true},
		{"auth/", "auth", true},
		{"auth/", "auth/session", true},
		{"auth/session/", "auth", true},
		{"auth", "authz", false},
		{"auth/a", "auth/b", false},
		{"ui/ auth/", "db/ auth/x.go", true},
		{"ui/", "db/", false},
		{".", "db/x.go", true},
		{"ui/", "./", true},
		{"", "auth", false},
		{"/", "auth", false},
		{"a/b", "a/bc", false},
	} {
		if got := Clash(strings.Fields(tc.a), strings.Fields(tc.b)); got != tc.want {
			t.Errorf("Clash(%q, %q) = %v, want %v", tc.a, tc.b, got, tc.want)
		}
		if got := Clash(strings.Fields(tc.b), strings.Fields(tc.a)); got != tc.want {
			t.Errorf("Clash(%q, %q) = %v, want %v (symmetry)", tc.b, tc.a, got, tc.want)
		}
	}
}

// row is a Row's column and marks, for comparison.
type row struct {
	Col   Column
	Marks []string
}

func rowsByNum(b Board) map[int]row {
	m := map[int]row{}
	for _, r := range b.Rows {
		m[r.Task.Num] = row{r.Col, r.Marks}
	}
	return m
}

func wt(branch string) gitx.Worktree { return gitx.Worktree{Path: "/wt/" + branch, Branch: branch} }
func br(name string) gitx.Branch     { return gitx.Branch{Name: name} }

func TestBuildClassify(t *testing.T) {
	ready := func(num int, kv ...string) *task.Task {
		return mk(num, "t", append([]string{"kind", "feat", "pri", "2", "status", "ready", "area", "x/"}, kv...)...)
	}
	for _, tc := range []struct {
		name  string
		s     State
		want  map[int]row
		check []string
	}{
		{
			name: "doing: worktree on the task's branch",
			s: State{Tasks: []*task.Task{ready(1)}, Worktrees: []gitx.Worktree{wt("ape/T1-any-name")},
				LastOwn: map[string]time.Time{"/wt/ape/T1-any-name": now.Add(-12 * time.Minute)}},
			want: map[int]row{1: {Doing, []string{"12m since commit"}}},
		},
		{
			name: "doing: no commits beyond base",
			s:    State{Tasks: []*task.Task{ready(1)}, Worktrees: []gitx.Worktree{wt("ape/T1-x")}},
			want: map[int]row{1: {Doing, []string{"no commits"}}},
		},
		{
			name: "doing beats you for kind owner: the owner is doing it",
			s: State{Tasks: []*task.Task{
				mk(1, "t", "kind", "owner", "status", "blocked", "needs", "owner"),
				mk(2, "t", "kind", "owner", "status", "ready"),
			}, Worktrees: []gitx.Worktree{wt("ape/T1-x"), wt("ape/T2-x")}},
			want: map[int]row{1: {Doing, []string{"no commits"}}, 2: {Doing, []string{"no commits"}}},
		},
		{
			name: "you beats doing for needs owner: the agent waits on the owner",
			s: State{Tasks: []*task.Task{
				ready(1, "status", "blocked", "needs", "owner"),
				withAsk(ready(2, "status", "blocked", "needs", "owner"), "ok?"),
				ready(3, "area", "x/y", "excl", "yes"),
			}, Worktrees: []gitx.Worktree{wt("ape/T1-x"), wt("ape/T2-x")},
				LastOwn: map[string]time.Time{"/wt/ape/T2-x": now.Add(-4 * time.Hour)}},
			want: map[int]row{
				1: {Owner, []string{"needs owner", "doing"}},
				2: {Owner, []string{"doing"}},
				3: {Ready, []string{"clashes T1, T2"}},
			},
			check: []string{"T2: quiet 4h (worktree, no commit)"},
		},
		{
			name: "doing beats new",
			s:    State{Tasks: []*task.Task{mk(1, "t", "status", "new")}, Worktrees: []gitx.Worktree{wt("ape/T1-x")}},
			want: map[int]row{1: {Doing, []string{"no commits"}}},
		},
		{
			name: "you: needs owner, kind owner, ask hides the mark",
			s: State{Tasks: []*task.Task{
				ready(1, "status", "blocked", "needs", "owner"),
				mk(2, "t", "kind", "owner", "status", "ready"),
				withAsk(mk(3, "t", "kind", "owner", "status", "ready"), "ok?"),
			}},
			want: map[int]row{1: {Owner, []string{"needs owner"}}, 2: {Owner, []string{"needs owner"}}, 3: {Owner, nil}},
		},
		{
			name: "you beats new",
			s:    State{Tasks: []*task.Task{mk(1, "t", "kind", "owner", "status", "new")}},
			want: map[int]row{1: {Owner, []string{"needs owner"}}},
		},
		{
			name: "new: new, triage, from",
			s: State{Tasks: []*task.Task{
				mk(1, "t", "status", "new"),
				mk(2, "t", "status", "triage", "from", "web@3f2a9c1"),
			}},
			want: map[int]row{1: {New, nil}, 2: {New, []string{"triage", "from web@3f2a9c1"}}},
		},
		{
			name: "blocked on an existing task",
			s:    State{Tasks: []*task.Task{ready(1), ready(2, "status", "blocked", "needs", "1")}},
			want: map[int]row{1: {Ready, nil}, 2: {Blocked, []string{"needs T1"}}},
		},
		{
			name:  "blocked on a gone task",
			s:     State{Tasks: []*task.Task{ready(2, "status", "blocked", "needs", "0009")}},
			want:  map[int]row{2: {Blocked, []string{"needs T9 (gone)"}}},
			check: []string{"T2: needs T9: all done; ape set T2 status=ready needs="},
		},
		{
			name: "blocked on dropped tasks: CHECK per dropped one, no all-done hint",
			s: State{Tasks: []*task.Task{ready(1), ready(2, "status", "blocked", "needs", "1 9"),
				ready(3, "status", "blocked", "needs", "8 9")}, Dropped: map[int]bool{9: true, 8: false}},
			want: map[int]row{1: {Ready, nil}, 2: {Blocked, []string{"needs T1"}},
				3: {Blocked, []string{"needs T8 T9 (gone)"}}},
			check: []string{
				"T2: needs T9, dropped; ape set T2 needs=… (what replaces it), or drop T2",
				"T3: needs T9, dropped; ape set T3 needs=… (what replaces it), or drop T3",
			},
		},
		{
			name: "blocked on several tasks: the mark lists the open ones",
			s: State{Tasks: []*task.Task{ready(3), ready(7),
				ready(10, "status", "blocked", "needs", "3 5 07"), ready(11, "status", "blocked", "needs", "5 3")}},
			want: map[int]row{3: {Ready, nil}, 7: {Ready, nil},
				10: {Blocked, []string{"needs T3 T7"}}, 11: {Blocked, []string{"needs T3"}}},
		},
		{
			name:  "blocked on several tasks, all gone",
			s:     State{Tasks: []*task.Task{ready(10, "status", "blocked", "needs", "3 5 7")}},
			want:  map[int]row{10: {Blocked, []string{"needs T3 T5 T7 (gone)"}}},
			check: []string{"T10: needs T3 T5 T7: all done; ape set T10 status=ready needs="},
		},
		{
			name: "doing: detached worktree at the task's path (mid-rebase), any slug",
			s: State{Tasks: []*task.Task{ready(1), ready(2, "area", "y/")}, WorktreeDir: "/wt",
				Worktrees: []gitx.Worktree{{Path: "/wt/T1-old-name", Detached: true}, {Path: "/elsewhere/T2-x", Detached: true}},
				Branches:  []gitx.Branch{br("ape/T1-old-name"), br("ape/T2-x")},
				LastOwn:   map[string]time.Time{"/wt/T1-old-name": now.Add(-time.Hour)}},
			want: map[int]row{1: {Doing, []string{"1h since commit"}}, 2: {Ready, []string{"paused"}}},
		},
		{
			name: "prunable worktree: not doing, paused, CHECK",
			s: State{Tasks: []*task.Task{ready(1)},
				Worktrees: []gitx.Worktree{{Path: "/wt/ape/T1-x", Branch: "ape/T1-x", Prunable: true}},
				Branches:  []gitx.Branch{br("ape/T1-x")}},
			want:  map[int]row{1: {Ready, []string{"paused"}}},
			check: []string{"T1: worktree /wt/ape/T1-x missing; ape pause T1 prunes it"},
		},
		{
			name: "several branches",
			s: State{Tasks: []*task.Task{ready(1)}, Worktrees: []gitx.Worktree{wt("ape/T1-new")},
				Branches: []gitx.Branch{br("ape/T1-old"), br("ape/T1-new")}},
			want:  map[int]row{1: {Doing, []string{"no commits"}}},
			check: []string{"T1: several branches: ape/T1-new, ape/T1-old; delete the stale one"},
		},
		{
			name: "ready: clashes when either is excl, sorted, paused",
			s: State{
				Tasks: []*task.Task{
					ready(1, "area", "auth/"),
					ready(2, "area", "ui/ auth/session", "excl", "yes"),
					ready(3, "area", "db/"),
					ready(4, "area", "ui/"),
					ready(5, "area", "auth", "excl", "yes"),
					ready(6, "area", "ui/x"), // overlaps T4, neither excl: no clash
				},
				Worktrees: []gitx.Worktree{wt("ape/T4-ui"), wt("ape/T1-auth")},
				Branches:  []gitx.Branch{br("ape/T1-auth"), br("ape/T4-ui"), br("ape/T2-x"), br("ape/T3-db")},
			},
			want: map[int]row{
				1: {Doing, []string{"no commits"}},
				2: {Ready, []string{"clashes T1, T4", "paused"}},
				3: {Ready, []string{"paused"}},
				4: {Doing, []string{"no commits"}},
				5: {Ready, []string{"clashes T1"}},
				6: {Ready, nil},
			},
		},
		{
			name: "ready: excl on the task with a worktree; any overlap",
			s: State{
				Tasks:     []*task.Task{ready(1, "area", ".", "excl", "yes"), ready(2, "area", "docs/"), ready(3, "area", "ui/")},
				Worktrees: []gitx.Worktree{wt("ape/T1-fmt")},
			},
			want: map[int]row{1: {Doing, []string{"no commits"}}, 2: {Ready, []string{"clashes T1"}},
				3: {Ready, []string{"clashes T1"}}},
		},
		{
			name: "ready: real conflicts with a paused branch; regenerated only hidden; excl clash wins",
			s: State{
				Tasks: []*task.Task{ready(1, "area", "a/"), ready(2, "area", "b/"), ready(3, "area", "x/", "excl", "yes"),
					ready(4, "area", "x/y"), ready(5, "area", "q/")},
				Worktrees: []gitx.Worktree{wt("ape/T1-a"), wt("ape/T2-b"), wt("ape/T3-x")},
				Branches:  []gitx.Branch{br("ape/T1-a"), br("ape/T2-b"), br("ape/T3-x"), br("ape/T4-y"), br("ape/T5-q")},
				Conflicts: map[[2]int]Conflict{
					{4, 1}: {Files: []string{"a.go", "b.go", "c.go", "d.go", "e.go"}},
					{4, 2}: {Files: []string{"gen.txt"}, Regenerated: true},
					{4, 3}: {Files: []string{"x/y/z.go"}},
					{5, 2}: {Files: []string{"go.sum"}},
				},
			},
			want: map[int]row{
				1: {Doing, []string{"no commits"}}, 2: {Doing, []string{"no commits"}}, 3: {Doing, []string{"no commits"}},
				4: {Ready, []string{"clashes T3", "conflicts T1: a.go, b.go, c.go, +2 more", "paused"}},
				5: {Ready, []string{"conflicts T2: go.sum", "paused"}},
			},
		},
		{
			name: "doing: conflicts with main, regenerated only said so",
			s: State{
				Tasks:      []*task.Task{ready(1), ready(2, "area", "y/"), ready(3, "area", "z/")},
				Worktrees:  []gitx.Worktree{wt("ape/T1-a"), wt("ape/T2-b"), wt("ape/T3-c")},
				MainBranch: "master",
				MainConflicts: map[int]Conflict{
					1: {Files: []string{"x/a.go"}},
					2: {Files: []string{"corpus/e/expected.txt"}, Regenerated: true},
				},
			},
			want: map[int]row{
				1: {Doing, []string{"no commits", "conflicts master: x/a.go"}},
				2: {Doing, []string{"no commits", "conflicts master in regenerated files only: corpus/e/expected.txt"}},
				3: {Doing, []string{"no commits"}},
			},
		},
		{
			name: "broken labels go to new",
			s: State{Tasks: []*task.Task{
				mk(1, "t"),
				mk(2, "t", "status", "done"),
				mk(3, "t", "status", "blocked"),
				mk(4, "t", "status", "blocked", "needs", "soon"),
				mk(5, "t", "status", "blocked", "needs", "owner 3"),
			}},
			want: map[int]row{1: {New, nil}, 2: {New, nil}, 3: {New, nil}, 4: {New, nil}, 5: {New, nil}},
		},
		{
			name: "branches and worktrees without a task; other branches ignored",
			s: State{
				Tasks:     []*task.Task{ready(1)},
				Worktrees: []gitx.Worktree{{Path: "/main", Branch: "main"}, wt("ape/board"), wt("ape/T31-foo"), {Path: "/d"}},
				Branches:  []gitx.Branch{br("ape/board"), br("ape/12-short"), br("ape/T31-foo"), br("ape/T32-bar")},
			},
			want:  map[int]row{1: {Ready, nil}},
			check: []string{"ape/T31-foo: worktree without a task", "ape/T32-bar: branch without a task"},
		},
		{
			name: "problems and outdated",
			s: State{
				Problems: []task.Problem{{Num: 3, File: "T3-x.md", Msg: "invalid pri"}, {File: "notes.md", Msg: "bad filename"}},
				Outdated: true,
			},
			want:  map[int]row{},
			check: []string{"T3: invalid pri", "notes.md: bad filename", "_FRAMEWORK.md: outdated; run ape init"},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			tc.s.Now = now
			b := Build(tc.s)
			if got := rowsByNum(b); !reflect.DeepEqual(got, tc.want) {
				t.Errorf("rows = %v, want %v", got, tc.want)
			}
			if !reflect.DeepEqual(b.Check, tc.check) {
				t.Errorf("check = %q, want %q", b.Check, tc.check)
			}
		})
	}
}

func TestBuildSinceCommit(t *testing.T) {
	for _, tc := range []struct {
		ago   time.Duration
		mark  string
		quiet string
	}{
		{0, "0m since commit", ""},
		{59*time.Minute + 59*time.Second, "59m since commit", ""},
		{time.Hour, "1h since commit", ""},
		{3*time.Hour - time.Second, "2h since commit", ""},
		{3 * time.Hour, "3h since commit", "T1: quiet 3h (worktree, no commit)"},
		{48*time.Hour - time.Second, "47h since commit", "T1: quiet 47h (worktree, no commit)"},
		{48 * time.Hour, "2d since commit", "T1: quiet 2d (worktree, no commit)"},
		{-time.Minute, "0m since commit", ""},
	} {
		b := Build(State{
			Tasks:     []*task.Task{mk(1, "t", "status", "ready")},
			Worktrees: []gitx.Worktree{wt("ape/T1-x")},
			LastOwn:   map[string]time.Time{"/wt/ape/T1-x": now.Add(-tc.ago)},
			Now:       now,
		})
		if got := b.Rows[0].Marks; !reflect.DeepEqual(got, []string{tc.mark}) {
			t.Errorf("%v ago: marks %q, want %q", tc.ago, got, tc.mark)
		}
		var want []string
		if tc.quiet != "" {
			want = []string{tc.quiet}
		}
		if !reflect.DeepEqual(b.Check, want) {
			t.Errorf("%v ago: check %q, want %q", tc.ago, b.Check, want)
		}
	}
}

func TestBuildQuietWithoutCommits(t *testing.T) {
	for _, tc := range []struct {
		ago   time.Duration
		quiet []string
	}{
		{3*time.Hour - time.Second, nil},
		{4 * time.Hour, []string{"T1: quiet 4h (no commits since start)"}},
	} {
		b := Build(State{
			Tasks:     []*task.Task{mk(1, "t", "status", "ready")},
			Worktrees: []gitx.Worktree{wt("ape/T1-x")},
			Started:   map[string]time.Time{"/wt/ape/T1-x": now.Add(-tc.ago)},
			Now:       now,
		})
		if got := b.Rows[0].Marks; !reflect.DeepEqual(got, []string{"no commits"}) {
			t.Errorf("%v ago: marks %q", tc.ago, got)
		}
		if !reflect.DeepEqual(b.Check, tc.quiet) {
			t.Errorf("%v ago: check %q, want %q", tc.ago, b.Check, tc.quiet)
		}
	}
}

func TestResolve(t *testing.T) {
	wts := []gitx.Worktree{
		{Path: "/r", Branch: "main"},
		{Path: "/r-wt/T12-a", Branch: "ape/T12-a"},
		{Path: "/r-wt/T12-b", Detached: true},      // mid-rebase
		{Path: "/r-wt/T13-c", Branch: "ape/T13-c"}, // its branch decides, not its path
		{Path: "/r-wt/T12-d", Branch: "ape/T14-d"}, // likewise
		{Path: "/other/T12-e", Detached: true},     // outside the worktree dir
		{Path: "/r-wt/T12-gone", Branch: "ape/T12-gone", Prunable: true},
	}
	brs := []gitx.Branch{br("ape/T12-z"), br("ape/T12-a"), br("ape/12-short"), br("main"), br("ape/T13-c")}
	w := Resolve(12, "/r-wt", wts, brs)
	if want := []string{"ape/T12-a", "ape/T12-z"}; !reflect.DeepEqual(w.Branches, want) {
		t.Errorf("branches %q, want %q", w.Branches, want)
	}
	var paths []string
	for _, x := range w.Worktrees {
		paths = append(paths, x.Path)
	}
	if want := []string{"/r-wt/T12-a", "/r-wt/T12-b", "/r-wt/T12-gone"}; !reflect.DeepEqual(paths, want) {
		t.Errorf("worktrees %q, want %q", paths, want)
	}
	if w.Branch() != "" {
		t.Errorf("Branch() = %q with several", w.Branch())
	}
	if live, ok := w.Live(); !ok || live.Path != "/r-wt/T12-a" {
		t.Errorf("Live() = %v, %v", live, ok)
	}

	w = Resolve(40, "/r-wt", []gitx.Worktree{{Path: "/r-wt/T40-x", Branch: "ape/T40-x", Prunable: true}},
		[]gitx.Branch{br("ape/T40-x")})
	if _, ok := w.Live(); ok || w.Branch() != "ape/T40-x" {
		t.Errorf("prunable only: %+v", w)
	}
}

func TestBuildOrder(t *testing.T) {
	b := Build(State{Now: now, Tasks: []*task.Task{
		mk(9, "t", "status", "ready"),
		mk(8, "t", "status", "ready", "pri", "2"),
		mk(7, "t", "status", "new"),
		mk(6, "t", "status", "ready", "pri", "1"),
		mk(5, "t", "status", "ready", "pri", "2"),
		mk(4, "t", "status", "ready", "pri", "bogus"),
		mk(3, "t", "kind", "owner"),
		mk(1, "t", "status", "ready"),
		mk(2, "t", "kind", "owner", "pri", "3"),
		mk(10, "t", "status", "new", "needs", "owner"),
		mk(11, "t", "status", "blocked", "needs", "owner", "pri", "1"),
	}})
	var got []int
	for _, r := range b.Rows {
		got = append(got, r.Task.Num)
	}
	if want := []int{11, 2, 3, 10, 7, 6, 5, 8, 1, 4, 9}; !reflect.DeepEqual(got, want) {
		t.Errorf("order = %v, want %v", got, want)
	}
}

func TestBuildDone(t *testing.T) {
	b := Build(State{Now: now, Done: []gitx.LogEntry{
		{Subject: "done(T12): login works (code abc1234)"},
		{Subject: "set(T3): pri=1"},
		{Subject: "drop(T12345): dup of T3"},
		{Subject: "done x y"},
		{Subject: "done(T4):"},
		{Subject: "done T5 old-style: x"},
		{Subject: "done(x): y"},
		{Subject: "done(T6) no colon"},
		{Subject: "dropped(T7): x"},
	}})
	want := []string{"T12  done  login works (code abc1234)", "T12345 drop  dup of T3", "T4   done"}
	if !reflect.DeepEqual(b.Done, want) {
		t.Errorf("done = %q, want %q", b.Done, want)
	}
}

func TestFit(t *testing.T) {
	exact := strings.Repeat("é", width)
	if got := fit(exact); got != exact {
		t.Errorf("fit cut a line of exactly %d runes", width)
	}
	if got, want := fit(exact+"x"), strings.Repeat("é", width-1)+"…"; got != want {
		t.Errorf("fit(%d+1 runes) = %q, want %q", width, got, want)
	}
	if got, want := fit(strings.Repeat("a", width-2)+"  bc"), strings.Repeat("a", width-2)+"…"; got != want {
		t.Errorf("fit kept the spaces before …: %q", got)
	}
}

// exampleState is _FRAMEWORK.md's board example.
func exampleState() State {
	return State{
		Now: now,
		Tasks: []*task.Task{
			withAsk(mk(3, "Approve the pricing page copy", "kind", "owner", "pri", "1", "status", "ready"),
				"Ship the shorter copy? rec: yes; the long one tested worse."),
			mk(4, "OAuth login", "kind", "feat", "pri", "1", "status", "ready", "area", "auth/"),
			mk(5, "Session expires early", "kind", "bug", "pri", "1", "status", "ready", "area", "auth/session.go"),
			mk(7, "Export to CSV", "kind", "feat", "pri", "2", "status", "blocked", "area", "export/", "needs", "owner"),
			mk(9, "Upgrade the test runner", "kind", "chore", "pri", "2", "status", "ready", "area", "ci/"),
			mk(11, "Dark mode flickers on load", "kind", "bug", "status", "new", "from", "web@3f2a9c1"),
			mk(12, "Split auth/ into packages", "kind", "chore", "pri", "3", "status", "ready", "area", "auth/", "excl", "yes"),
		},
		Worktrees:     []gitx.Worktree{{Path: "/r", Branch: "main"}, wt("ape/T4-oauth-login")},
		Branches:      []gitx.Branch{br("ape/T4-oauth-login"), br("ape/T9-test-runner"), br("ape/T31-foo")},
		LastOwn:       map[string]time.Time{"/wt/ape/T4-oauth-login": now.Add(-12 * time.Minute)},
		Done:          []gitx.LogEntry{{Subject: "done(T2): signup form + validation; e2e green (code 8c1d2e4)"}},
		Conflicts:     map[[2]int]Conflict{{9, 4}: {Files: []string{"go.mod"}}},
		MainConflicts: map[int]Conflict{4: {Files: []string{"auth/session.go"}}},
		MainBranch:    "main",
	}
}

func TestRenderGolden(t *testing.T) {
	for _, tc := range []struct {
		name string
		s    State
	}{
		{"empty", State{Now: now}},
		{"example", exampleState()},
		{"everything", State{
			Now: now,
			Tasks: []*task.Task{
				withAsk(mk(1, "Pick a database", "kind", "owner", "pri", "1", "status", "ready"), "Postgres or SQLite? rec: SQLite."),
				mk(2, "Buy the domain", "kind", "owner", "status", "new"),
				mk(3, "Billing", "kind", "feat", "pri", "2", "status", "blocked", "area", "billing/", "needs", "owner"),
				withAsk(mk(4, "Payments", "kind", "feat", "pri", "1", "status", "blocked", "area", "pay/", "needs", "owner"),
					"Stripe or Paddle? Stripe has the better API and docs, Paddle handles VAT as merchant of record. "+
						"rec: Stripe, since we're EU-only for now and can file VAT ourselves."),
				mk(5, "Invoices", "kind", "feat", "pri", "2", "status", "blocked", "area", "invoice/", "needs", "owner"),
				mk(6, "Sign the contract", "kind", "owner", "pri", "1", "status", "ready"),
				mk(10, "Login", "kind", "feat", "pri", "1", "status", "ready", "area", "auth/"),
				mk(11, "Search", "kind", "feat", "pri", "2", "status", "ready", "area", "search/ ui/"),
				mk(12, "Stale thing", "kind", "chore", "pri", "3", "status", "ready", "area", "ci/"),
				mk(13, "Unlabelled work", "status", "ready"),
				mk(20, "Crash on save", "kind", "bug", "status", "new", "from", "app@1234567"),
				mk(21, "Slow start", "kind", "bug", "status", "triage"),
				mk(22, "Odd status", "kind", "bug", "status", "later"),
				mk(30, "Reports", "kind", "feat", "pri", "2", "status", "blocked", "area", "reports/", "needs", "11"),
				mk(31, "Exports", "kind", "feat", "pri", "3", "status", "blocked", "area", "export/", "needs", "99"),
				mk(32, "Launch", "kind", "feat", "pri", "2", "status", "blocked", "area", "launch/", "needs", "10 98 30"),
				mk(40, "Session fix", "kind", "bug", "pri", "1", "status", "ready", "area", "auth/session ui/nav", "excl", "yes"),
				mk(41, "Resume me", "kind", "spike", "pri", "2", "status", "ready", "area", "spikes/"),
				mk(42, "Nav polish", "kind", "feat", "status", "ready", "area", "ui"),
				mk(43, "Refunds", "kind", "feat", "pri", "2", "status", "ready", "area", "pay/refunds"),
			},
			Problems: []task.Problem{
				{Num: 22, File: "T22-odd.md", Msg: "status later not in new, triage, ready, blocked"},
				{File: "todo.md", Msg: "bad filename"},
			},
			Worktrees: []gitx.Worktree{
				{Path: "/r", Branch: "main"}, wt("ape/T4-payments"), wt("ape/T5-invoices"), wt("ape/T6-contract"),
				wt("ape/T10-login"), wt("ape/T11-search"), wt("ape/T12-stale"), wt("ape/T13-unlabelled"), wt("ape/T50-orphan"),
			},
			Branches: []gitx.Branch{
				br("ape/T10-login"), br("ape/T11-search"), br("ape/T12-stale"), br("ape/T13-unlabelled"),
				br("ape/T41-resume"), br("ape/T50-orphan"), br("ape/T51-gone"),
				br("ape/T4-payments"), br("ape/T5-invoices"), br("ape/T6-contract"),
			},
			LastOwn: map[string]time.Time{
				"/wt/ape/T4-payments": now.Add(-20 * time.Minute),
				"/wt/ape/T6-contract": now.Add(-time.Hour),
				"/wt/ape/T10-login":   now.Add(-5 * time.Minute),
				"/wt/ape/T11-search":  now.Add(-26 * time.Hour),
				"/wt/ape/T12-stale":   now.Add(-80 * time.Hour),
			},
			Started: map[string]time.Time{"/wt/ape/T13-unlabelled": now.Add(-4 * time.Hour)},
			Done: []gitx.LogEntry{
				{Subject: "done(T5): signup form + validation (code 8c1d2e4)"},
				{Subject: "drop(T6): dup of T20"},
				{Subject: "done(T7): CSV and JSON importers, streaming, 40 MB in 1.2 s, round-trip tests green; left: XLSX and the progress bar (T44) (code 1a2b3c4)"},
				{Subject: "done(T8): a"}, {Subject: "done(T9): b"}, {Subject: "done(T14): c"}, // beyond the first 5
			},
			Conflicts: map[[2]int]Conflict{
				{41, 10}: {Files: []string{"corpus/a/expected.txt"}, Regenerated: true},
				{41, 11}: {Files: []string{"search/index.go", "ui/list.go"}},
			},
			MainConflicts: map[int]Conflict{
				10: {Files: []string{"corpus/a/expected.txt"}, Regenerated: true},
				11: {Files: []string{"a", "b", "c", "d"}},
			},
			MainBranch: "main",
			Dropped:    map[int]bool{99: true},
			Outdated:   true,
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got := Render(Build(tc.s))
			path := filepath.Join("testdata", tc.name+".golden")
			if *update {
				if err := os.WriteFile(path, []byte(got), 0o644); err != nil {
					t.Fatal(err)
				}
			}
			want, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			if got != string(want) {
				t.Errorf("Render mismatch for %s (-update to accept)\ngot:\n%s\nwant:\n%s", path, got, want)
			}
		})
	}
}

// TestRenderMatchesFramework checks the example against _FRAMEWORK.md's own board, below its first line: that
// line's counts include tasks the example doesn't show.
func TestRenderMatchesFramework(t *testing.T) {
	doc, err := os.ReadFile(filepath.Join("..", "..", "framework", "_FRAMEWORK.md"))
	if err != nil {
		t.Fatal(err)
	}
	_, block, ok := strings.Cut(string(doc), "$ ape\n")
	block, _, ok2 := strings.Cut(block, "```")
	if !ok || !ok2 {
		t.Fatal("no `$ ape` block in _FRAMEWORK.md")
	}
	_, wantBody, _ := strings.Cut(block, "\n")
	_, gotBody, _ := strings.Cut(Render(Build(exampleState())), "\n")
	if gotBody != wantBody {
		t.Errorf("board body differs from _FRAMEWORK.md\ngot:\n%s\nwant:\n%s", gotBody, wantBody)
	}
}
