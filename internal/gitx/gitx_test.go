package gitx

import (
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestMain(m *testing.M) {
	// Keep the user's git config (hooks, signing, default branch) out of the tests.
	os.Setenv("GIT_CONFIG_GLOBAL", os.DevNull)
	os.Setenv("GIT_CONFIG_NOSYSTEM", "1")
	os.Exit(m.Run())
}

// newRepo returns an empty repo on branch main.
func newRepo(t *testing.T) Repo {
	t.Helper()
	r := Repo{Dir: t.TempDir()}
	git(t, r, "init", "-q", "-b", "main")
	git(t, r, "config", "user.name", "t")
	git(t, r, "config", "user.email", "t@t")
	return r
}

func git(t *testing.T, r Repo, args ...string) string {
	t.Helper()
	out, err := r.Run(args...)
	if err != nil {
		t.Fatal(err)
	}
	return out
}

func write(t *testing.T, r Repo, name, content string) {
	t.Helper()
	p := filepath.Join(r.Dir, name)
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

// commitAll writes files and commits everything.
func commitAll(t *testing.T, r Repo, msg string, files ...string) {
	t.Helper()
	for _, f := range files {
		write(t, r, f, msg+"\n")
	}
	git(t, r, "add", "-A")
	git(t, r, "commit", "-q", "--allow-empty", "-m", msg)
}

func count(t *testing.T, r Repo) int {
	t.Helper()
	var n int
	fmt.Sscan(git(t, r, "rev-list", "--count", "HEAD"), &n)
	return n
}

func TestRunError(t *testing.T) {
	r := newRepo(t)
	out, err := r.Run("rev-parse", "--verify", "nosuchref")
	if err == nil || !strings.Contains(err.Error(), "fatal: Needed a single revision") {
		t.Fatalf("err = %v, want git's stderr", err)
	}
	if out != "" {
		t.Fatalf("out = %q", out)
	}
}

func TestCommitExactPaths(t *testing.T) {
	r := newRepo(t)
	commitAll(t, r, "init", "mod.md", "del.md", "other.md")
	write(t, r, "tasks/new.md", "new\n")
	write(t, r, "mod.md", "changed\n")
	os.Remove(filepath.Join(r.Dir, "del.md"))
	write(t, r, "staged.md", "staged\n")
	git(t, r, "add", "staged.md")
	write(t, r, "other.md", "unstaged change\n")

	if err := r.Commit("one", "tasks/new.md", "mod.md", "del.md"); err != nil {
		t.Fatal(err)
	}
	got := git(t, r, "show", "--name-status", "--format=%s", "HEAD")
	want := "one\n\nM\tmod.md\nD\tdel.md\nA\ttasks/new.md"
	if sortedLines(got) != sortedLines(want) {
		t.Fatalf("HEAD:\n%s\nwant:\n%s", got, want)
	}
	if st := git(t, r, "status", "--porcelain"); st != "M other.md\nA  staged.md" {
		t.Fatalf("status after commit:\n%s", st)
	}
}

func sortedLines(s string) string {
	l := strings.Split(s, "\n")
	slices.Sort(l)
	return strings.Join(l, "\n")
}

func TestCommitNothing(t *testing.T) {
	r := newRepo(t)
	commitAll(t, r, "init", "a.md", "gone.md")
	git(t, r, "rm", "-q", "gone.md")
	git(t, r, "commit", "-q", "-m", "rm")
	write(t, r, "staged.md", "x\n")
	git(t, r, "add", "staged.md")
	// Unchanged, already deleted, never existed: nothing to commit, and other staged files don't count.
	if err := r.Commit("none", "a.md", "gone.md", "never.md"); err != nil {
		t.Fatal(err)
	}
	if n := count(t, r); n != 2 {
		t.Fatalf("%d commits, want 2", n)
	}
}

func TestCommitInEmptyRepo(t *testing.T) {
	r := newRepo(t)
	write(t, r, "a.md", "a\n")
	write(t, r, "b.md", "b\n")
	if err := r.Commit("init", "a.md"); err != nil {
		t.Fatal(err)
	}
	if got := git(t, r, "ls-tree", "--name-only", "HEAD"); got != "a.md" {
		t.Fatalf("tree = %q", got)
	}
}

func TestCommitWaitsForIndexLock(t *testing.T) {
	r := newRepo(t)
	commitAll(t, r, "init", "a.md")
	lock := filepath.Join(r.Dir, ".git", "index.lock")
	if err := os.WriteFile(lock, nil, 0o644); err != nil {
		t.Fatal(err)
	}
	go func() { time.Sleep(500 * time.Millisecond); os.Remove(lock) }()
	write(t, r, "a.md", "changed\n")
	if err := r.Commit("after lock", "a.md"); err != nil {
		t.Fatal(err)
	}
	if s := git(t, r, "log", "-1", "--format=%s"); s != "after lock" {
		t.Fatalf("HEAD = %q", s)
	}
	// The commit doesn't need the shared index; its refresh waited for the lock.
	if st := git(t, r, "status", "--porcelain"); st != "" {
		t.Fatalf("status after commit:\n%s", st)
	}
}

func TestCommitPaths(t *testing.T) {
	r := newRepo(t)
	if sha, err := r.CommitPaths("none", "never.md"); sha != "" || err != nil {
		t.Fatalf("CommitPaths with nothing to commit in an empty repo = %q, %v", sha, err)
	}
	if ok, _ := r.hasHead(); ok {
		t.Fatal("nothing to commit in an empty repo made a commit")
	}
	write(t, r, "a.md", "a\n")
	sha, err := r.CommitPaths("first\n\nbody", "a.md")
	if err != nil || sha != git(t, r, "rev-parse", "HEAD") {
		t.Fatalf("CommitPaths in an empty repo = %q, %v", sha, err)
	}
	if sha, err := r.CommitPaths("again", "a.md"); sha != "" || err != nil {
		t.Fatalf("CommitPaths with nothing to commit = %q, %v", sha, err)
	}
	if msg := git(t, r, "log", "-1", "--format=%B"); msg != "first\n\nbody" {
		t.Fatalf("message = %q", msg)
	}
	if _, err := r.CommitPaths("x"); err == nil {
		t.Fatal("CommitPaths without paths: no error")
	}
}

// TestCommitIgnoresSharedIndex: what's staged in the shared index, for paths or others, never reaches the commit;
// paths are committed as they are on disk.
func TestCommitIgnoresSharedIndex(t *testing.T) {
	r := newRepo(t)
	commitAll(t, r, "init", "a.md")
	write(t, r, "a.md", "staged\n")
	git(t, r, "add", "a.md")
	write(t, r, "a.md", "on disk\n")
	write(t, r, "b.md", "staged too\n")
	git(t, r, "add", "b.md")
	if err := r.Commit("one", "a.md"); err != nil {
		t.Fatal(err)
	}
	if got := git(t, r, "show", "HEAD:a.md"); got != "on disk" {
		t.Fatalf("HEAD:a.md = %q", got)
	}
	if got := git(t, r, "ls-tree", "--name-only", "HEAD"); got != "a.md" {
		t.Fatalf("tree = %q", got)
	}
	if st := git(t, r, "status", "--porcelain"); st != "A  b.md" {
		t.Fatalf("status:\n%s", st)
	}
}

func TestCommitConcurrent(t *testing.T) {
	r := newRepo(t)
	commitAll(t, r, "init", "base.md")
	const n = 10
	var wg sync.WaitGroup
	errs := make([]error, n)
	for i := range n {
		name := fmt.Sprintf("tasks/%04d.md", i)
		write(t, r, name, "x\n")
		wg.Go(func() { errs[i] = r.Commit("c "+name, name) })
	}
	wg.Wait()
	for i, err := range errs {
		if err != nil {
			t.Errorf("commit %d: %v", i, err)
		}
	}
	if c := count(t, r); c != n+1 {
		t.Fatalf("%d commits, want %d", c, n+1)
	}
	if st := git(t, r, "status", "--porcelain"); st != "" {
		t.Fatalf("left uncommitted:\n%s", st)
	}
}

// TestCommitWithHandCommits races Commit against `git commit -- <path>` processes, each retried on contention as
// a person would: every commit lands, with its content.
func TestCommitWithHandCommits(t *testing.T) {
	for round := range 3 {
		r := newRepo(t)
		const n = 8
		hand := func(i int) string { return fmt.Sprintf("hand/%d.md", i) }
		for i := range n {
			write(t, r, hand(i), "old\n")
		}
		commitAll(t, r, "init")
		for i := range n {
			write(t, r, hand(i), fmt.Sprintf("hand %d\n", i))
			write(t, r, fmt.Sprintf("ape/%d.md", i), fmt.Sprintf("ape %d\n", i))
		}
		var wg sync.WaitGroup
		errs := make([]error, 2*n)
		for i := range n {
			wg.Go(func() { errs[i] = r.Commit(fmt.Sprintf("ape %d", i), fmt.Sprintf("ape/%d.md", i)) })
			wg.Go(func() {
				deadline := time.Now().Add(commitBudget)
				for {
					_, err := r.Run("commit", "-q", "-m", fmt.Sprintf("hand %d", i), "--", hand(i))
					if err == nil || !contended(err) || time.Now().After(deadline) {
						errs[n+i] = err
						return
					}
					time.Sleep(jitter())
				}
			})
		}
		wg.Wait()
		for i, err := range errs {
			if err != nil {
				t.Errorf("round %d, committer %d: %v", round, i, err)
			}
		}
		if c := count(t, r); c != 2*n+1 {
			t.Fatalf("round %d: %d commits, want %d:\n%s", round, c, 2*n+1, git(t, r, "log", "--format=%s"))
		}
		for i := range n {
			for _, f := range []string{hand(i), fmt.Sprintf("ape/%d.md", i)} {
				if got, want := git(t, r, "show", "HEAD:"+f), strings.Split(f, "/")[0]+" "+fmt.Sprint(i); got != want {
					t.Errorf("round %d: HEAD:%s = %q, want %q", round, f, got, want)
				}
			}
		}
	}
}

func TestBranchExistsAndDeleteBranchAt(t *testing.T) {
	r := newRepo(t)
	commitAll(t, r, "init", "a.md")
	old := git(t, r, "rev-parse", "HEAD")
	git(t, r, "branch", "ape/T1-a")
	git(t, r, "tag", "ape/T2-b") // a tag isn't a branch
	for name, want := range map[string]bool{"ape/T1-a": true, "main": true, "ape/T2-b": false, "nosuch": false} {
		if ok, err := r.BranchExists(name); err != nil || ok != want {
			t.Errorf("BranchExists(%q) = %v, %v; want %v", name, ok, err, want)
		}
	}

	git(t, r, "switch", "-q", "ape/T1-a")
	commitAll(t, r, "work", "b.md")
	git(t, r, "switch", "-q", "main")
	if err := r.DeleteBranchAt("ape/T1-a", old); err == nil {
		t.Fatal("DeleteBranchAt with a stale sha: no error")
	}
	if ok, _ := r.BranchExists("ape/T1-a"); !ok {
		t.Fatal("DeleteBranchAt with a stale sha deleted the branch")
	}
	if err := r.DeleteBranchAt("ape/T1-a", git(t, r, "rev-parse", "ape/T1-a")); err != nil {
		t.Fatal(err)
	}
	if ok, _ := r.BranchExists("ape/T1-a"); ok {
		t.Fatal("branch still there")
	}
}

func TestDirty(t *testing.T) {
	r := newRepo(t)
	commitAll(t, r, "init", "a.md", "old.md", ".gitignore")
	write(t, r, ".gitignore", "*.log\n")
	git(t, r, "commit", "-qam", "ignore")
	if es, err := Dirty(r.Dir); err != nil || len(es) != 0 {
		t.Fatalf("clean: %q, %v", es, err)
	}
	// Config that would hide untracked files from a plain `git status`.
	git(t, r, "config", "status.showUntrackedFiles", "no")
	write(t, r, "new dir/n.md", "n\n")
	write(t, r, "x.log", "ignored\n")
	if es, err := Dirty(r.Dir); err != nil || !slices.Equal(es, []string{"?? new dir/"}) {
		t.Fatalf("untracked: %q, %v", es, err)
	}
	write(t, r, "a.md", "changed\n")
	git(t, r, "mv", "old.md", "renamed.md")
	es, err := Dirty(r.Dir)
	if err != nil {
		t.Fatal(err)
	}
	slices.Sort(es)
	if want := []string{" M a.md", "?? new dir/", "R  renamed.md"}; !slices.Equal(es, want) {
		t.Fatalf("Dirty = %q, want %q", es, want)
	}
	if _, err := Dirty(filepath.Join(r.Dir, "nosuch")); err == nil {
		t.Fatal("Dirty of a missing dir: no error")
	}
}

func TestWorktreesAndBranches(t *testing.T) {
	r := newRepo(t)
	commitAll(t, r, "init", "a.md")
	wtDir := t.TempDir()
	feat := filepath.Join(wtDir, "T12-x")
	det := filepath.Join(wtDir, "det")
	git(t, r, "worktree", "add", "-q", "-b", "ape/T12-x", feat, "main")
	git(t, r, "worktree", "add", "-q", "--detach", det, "main")
	git(t, r, "worktree", "lock", "--reason", "usb disk", det)
	git(t, r, "branch", "ape/T31-foo")
	git(t, r, "branch", "wip")
	git(t, r, "tag", "ape/T12-x") // makes %(refname:short) ambiguous

	gone := filepath.Join(wtDir, "gone")
	git(t, r, "worktree", "add", "-q", "-b", "ape/T40-gone", gone, "main")
	if err := os.RemoveAll(gone); err != nil {
		t.Fatal(err)
	}
	head := git(t, r, "rev-parse", "HEAD")

	wts, err := r.Worktrees()
	if err != nil {
		t.Fatal(err)
	}
	want := []Worktree{
		{Path: realpath(t, r.Dir), Branch: "main", Head: head},
		{Path: realpath(t, feat), Branch: "ape/T12-x", Head: head},
		{Path: realpath(t, det), Head: head, Detached: true, Locked: true},
		{Path: filepath.Join(realpath(t, wtDir), "gone"), Branch: "ape/T40-gone", Head: head, Prunable: true},
	}
	if !slices.Equal(wts, want) {
		t.Fatalf("worktrees = %+v\nwant %+v", wts, want)
	}

	bs, err := r.Branches("ape/")
	if err != nil {
		t.Fatal(err)
	}
	var names []string
	for _, b := range bs {
		names = append(names, b.Name)
		if time.Since(b.Last) > time.Minute || time.Since(b.Last) < -time.Minute {
			t.Errorf("%s: Last = %v", b.Name, b.Last)
		}
	}
	if !slices.Equal(names, []string{"ape/T12-x", "ape/T31-foo", "ape/T40-gone"}) {
		t.Fatalf("branches = %v", names)
	}
}

func realpath(t *testing.T, p string) string {
	t.Helper()
	p, err := filepath.EvalSymlinks(p)
	if err != nil {
		t.Fatal(err)
	}
	return p
}

func TestRefs(t *testing.T) {
	r := newRepo(t)
	commitAll(t, r, "init", "a.md")
	base := git(t, r, "rev-parse", "HEAD")
	git(t, r, "branch", "ape/T1-a")

	if head, err := r.Head(); err != nil || head != base || len(head) != 40 {
		t.Fatalf("Head = %q, %v", head, err)
	}
	if b, err := r.CurrentBranch(); err != nil || b != "main" {
		t.Fatalf("CurrentBranch = %q, %v", b, err)
	}
	if ahead, err := r.AheadOf("ape/T1-a", "main"); err != nil || ahead {
		t.Fatalf("AheadOf before commit = %v, %v", ahead, err)
	}
	if last, err := r.LastOwnCommit("ape/T1-a", "main"); err != nil || !last.IsZero() {
		t.Fatalf("LastOwnCommit before commit = %v, %v", last, err)
	}

	git(t, r, "switch", "-q", "ape/T1-a")
	commitAll(t, r, "work", "b.md")
	git(t, r, "switch", "-q", "main")
	if ahead, err := r.AheadOf("ape/T1-a", "main"); err != nil || !ahead {
		t.Fatalf("AheadOf = %v, %v", ahead, err)
	}
	if last, err := r.LastOwnCommit("ape/T1-a", "main"); err != nil || time.Since(last) > time.Minute {
		t.Fatalf("LastOwnCommit = %v, %v", last, err)
	}
	if ok, err := r.IsAncestor("ape/T1-a", "main"); err != nil || ok {
		t.Fatalf("IsAncestor(branch, main) before merge = %v, %v", ok, err)
	}
	if ok, err := r.IsAncestor("main", "ape/T1-a"); err != nil || !ok {
		t.Fatalf("IsAncestor(main, branch) = %v, %v", ok, err)
	}
	if ok, err := r.IsAncestor("main", "main"); err != nil || !ok {
		t.Fatalf("IsAncestor(main, main) = %v, %v", ok, err)
	}
	if _, err := r.IsAncestor("nosuch", "main"); err == nil {
		t.Fatal("IsAncestor of a missing ref: no error")
	}

	git(t, r, "switch", "-q", "--detach", "main")
	if b, err := r.CurrentBranch(); err != nil || b != "" {
		t.Fatalf("CurrentBranch detached = %q, %v", b, err)
	}
}

func TestMerged(t *testing.T) {
	r := newRepo(t)
	commitAll(t, r, "init", "a.md")
	git(t, r, "switch", "-q", "-c", "ape/T1-a")
	commitAll(t, r, "one", "b.md")
	commitAll(t, r, "two", "c.md")
	git(t, r, "switch", "-q", "main")
	commitAll(t, r, "other", "d.md")
	merged := func(a, b string, want bool) {
		t.Helper()
		if got, err := r.Merged(a, b); err != nil || got != want {
			t.Fatalf("Merged(%s, %s) = %v, %v; want %v", a, b, got, err, want)
		}
	}
	merged("main~1", "main", true) // ancestor
	merged("ape/T1-a", "main", false)

	git(t, r, "merge", "-q", "--squash", "ape/T1-a")
	git(t, r, "commit", "-q", "-m", "squash")
	merged("ape/T1-a", "main", true)
	commitAll(t, r, "later", "a.md", "d.md") // main moves on elsewhere: merging still changes nothing
	merged("ape/T1-a", "main", true)

	git(t, r, "rm", "-q", "c.md") // the branch's change undone on main: merging brings it back
	git(t, r, "commit", "-q", "-m", "revert c")
	merged("ape/T1-a", "main", false)

	git(t, r, "switch", "-q", "ape/T1-a") // a conflict isn't merged
	commitAll(t, r, "clash", "a.md")
	git(t, r, "switch", "-q", "main")
	merged("ape/T1-a", "main", false)

	if _, err := r.Merged("nosuch", "main"); err == nil {
		t.Fatal("Merged of a missing ref: no error")
	}
}

func TestLog(t *testing.T) {
	r := newRepo(t)
	if es, err := r.Log(time.Time{}); err != nil || len(es) != 0 {
		t.Fatalf("empty repo: %v, %v", es, err)
	}
	old := time.Now().Add(-48 * time.Hour).Unix()
	t.Setenv("GIT_COMMITTER_DATE", fmt.Sprintf("@%d +0000", old))
	commitAll(t, r, "old", "a.md")
	os.Unsetenv("GIT_COMMITTER_DATE")
	commitAll(t, r, "done(T1): a", "b.md")
	commitAll(t, r, "drop(T2): b", "c.md")

	es, err := r.Log(time.Now().Add(-24 * time.Hour))
	if err != nil {
		t.Fatal(err)
	}
	if len(es) != 2 || es[0].Subject != "drop(T2): b" || es[1].Subject != "done(T1): a" {
		t.Fatalf("Log = %v", es)
	}
	if time.Since(es[0].Time) > time.Minute {
		t.Fatalf("Time = %v", es[0].Time)
	}
	if es, _ := r.Log(time.Time{}); len(es) != 3 || es[2].Time.Unix() != old {
		t.Fatalf("Log(all) = %v", es)
	}
}

func TestPathsEverTouched(t *testing.T) {
	r := newRepo(t)
	if ps, err := r.PathsEverTouched("tasks"); err != nil || len(ps) != 0 {
		t.Fatalf("empty repo: %v, %v", ps, err)
	}
	commitAll(t, r, "1", "tasks/T1-a.md", "tasks/T2-b.md", "README.md")
	git(t, r, "rm", "-q", "tasks/T1-a.md")
	git(t, r, "commit", "-q", "-m", "2")
	git(t, r, "mv", "tasks/T2-b.md", "tasks/T3-c.md")
	git(t, r, "commit", "-q", "-m", "3")

	ps, err := r.PathsEverTouched("tasks")
	if err != nil {
		t.Fatal(err)
	}
	slices.Sort(ps)
	if want := []string{"tasks/T1-a.md", "tasks/T2-b.md", "tasks/T3-c.md"}; !slices.Equal(ps, want) {
		t.Fatalf("paths = %v, want %v", ps, want)
	}
}

// A git hook runs ape with GIT_DIR and friends set for the hook's repo; gitx works on its Dir regardless.
func TestIgnoresRepoEnv(t *testing.T) {
	r, other := newRepo(t), newRepo(t)
	commitAll(t, r, "mine", "a.md")
	commitAll(t, other, "other", "b.md")
	want := git(t, r, "rev-parse", "HEAD")
	t.Setenv("GIT_DIR", filepath.Join(other.Dir, ".git"))
	t.Setenv("GIT_WORK_TREE", other.Dir)
	t.Setenv("GIT_INDEX_FILE", filepath.Join(other.Dir, ".git", "index"))
	t.Setenv("GIT_COMMON_DIR", filepath.Join(other.Dir, ".git"))
	t.Setenv("GIT_OBJECT_DIRECTORY", filepath.Join(other.Dir, ".git", "objects"))
	if got, err := r.Head(); err != nil || got != want {
		t.Fatalf("Head = %q, %v; want %q", got, err, want)
	}
	write(t, r, "c.md", "c\n")
	if err := r.Commit("add c", "c.md"); err != nil {
		t.Fatal(err)
	}
	if got := git(t, r, "log", "-1", "--format=%s"); got != "add c" {
		t.Fatalf("last commit %q", got)
	}
	if es, err := Dirty(r.Dir); err != nil || len(es) != 0 {
		t.Fatalf("Dirty = %q, %v", es, err)
	}
	if got := git(t, other, "log", "-1", "--format=%s"); got != "other" {
		t.Fatalf("other repo's last commit %q", got)
	}
}
