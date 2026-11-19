package commands

import (
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"testing"
)

func TestCommit(t *testing.T) {
	env := project(t)
	ape := env.ApeDir
	writeFile(t, filepath.Join(ape, "ref", "old.md"), "old\n")
	writeFile(t, filepath.Join(ape, "ref", "gone.md"), "gone\n")
	mustGit(t, ape, "add", "ref")
	mustGit(t, ape, "commit", "-q", "-m", "ref")
	writeFile(t, filepath.Join(ape, "ref", "old.md"), "changed\n")
	os.Remove(filepath.Join(ape, "ref", "gone.md"))
	writeFile(t, filepath.Join(ape, "ref", "new.md"), "new\n")
	writeFile(t, filepath.Join(ape, "staged.md"), "staged\n")
	mustGit(t, ape, "add", "staged.md")
	writeFile(t, filepath.Join(ape, "PROJECT.md"), "not named\n")

	// From the main root (ape/...), from inside ape/ (relative), and absolute.
	env.Cwd = filepath.Join(ape, "ref")
	out := run(t, env, Commit, "-m", "notes", "new.md", filepath.Join(env.MainRoot, "ape", "ref", "old.md"), "gone.md")
	if !regexp.MustCompile(`^[0-9a-f]{7,}\n$`).MatchString(out) {
		t.Fatalf("printed %q, want a short sha", out)
	}
	if head := mustGit(t, ape, "rev-parse", "--short", "HEAD"); out != head+"\n" {
		t.Fatalf("printed %q, HEAD is %s", out, head)
	}
	got := mustGit(t, ape, "show", "--name-status", "--format=%s", "HEAD")
	if want := "notes\n\nD\tref/gone.md\nA\tref/new.md\nM\tref/old.md"; got != want {
		t.Fatalf("HEAD:\n%s\nwant:\n%s", got, want)
	}
	if st := mustGit(t, ape, "status", "--porcelain"); st != "M PROJECT.md\nA  staged.md" {
		t.Fatalf("status:\n%s", st)
	}

	n := len(subjects(t, env))
	if out := run(t, env, Commit, "-m", "again", "new.md", "gone.md"); out != "nothing to commit\n" {
		t.Fatalf("nothing to commit printed %q", out)
	}
	if len(subjects(t, env)) != n {
		t.Fatal("nothing to commit made a commit")
	}
}

// TestCommitThroughWorktreeLink: from a worktree, ape/ is a symlink to the main root's; paths through it count as
// inside the ape dir.
func TestCommitThroughWorktreeLink(t *testing.T) {
	env := project(t)
	wt := startT(t, env, "T1-x")
	writeFile(t, filepath.Join(env.ApeDir, "tasks", "T1-x.md"), "---\nstatus: new\n---\n# x\n")
	env.Cwd = wt
	run(t, env, Commit, "-m", "task", "ape/tasks/T1-x.md")
	env.Cwd = filepath.Join(wt, "ape", "tasks")
	writeFile(t, filepath.Join(env.ApeDir, "ref", "new dir", "n.md"), "n\n")
	run(t, env, Commit, "-m", "note", "../ref/new dir/n.md")
	if got := subjects(t, env)[:2]; !slices.Equal(got, []string{"note", "task"}) {
		t.Fatalf("subjects = %q", got)
	}

	// Not found from the cwd → relative to the ape dir: an agent in its worktree names tasks/….
	env.Cwd = wt
	writeFile(t, filepath.Join(env.ApeDir, "tasks", "T1-x.md"), "---\nstatus: new\n---\n# x, edited\n")
	run(t, env, Commit, "-m", "edit", "tasks/T1-x.md")
	if got := mustGit(t, env.ApeDir, "show", "--name-only", "--format=%s", "HEAD"); got != "edit\n\ntasks/T1-x.md" {
		t.Fatalf("HEAD:\n%s", got)
	}
	// Both exist, the cwd's outside ape/ (the worktree's README): ape/'s wins.
	writeFile(t, filepath.Join(env.ApeDir, "README"), "ape's\n")
	run(t, env, Commit, "-m", "readme", "README")
	if got := mustGit(t, env.ApeDir, "show", "--name-only", "--format=%s", "HEAD"); got != "readme\n\nREADME" {
		t.Fatalf("HEAD:\n%s", got)
	}
}

// TestCommitPrefersApe: from the main root, a relative path that resolves outside ape/ is ape/'s when it exists
// there or is in its HEAD (T16).
func TestCommitPrefersApe(t *testing.T) {
	env := project(t)
	writeFile(t, filepath.Join(env.MainRoot, "DECISIONS.md"), "code's\n")
	writeFile(t, filepath.Join(env.ApeDir, "DECISIONS.md"), "ape's, edited\n")
	writeFile(t, filepath.Join(env.ApeDir, "ref", "gone.md"), "gone\n")
	mustGit(t, env.ApeDir, "add", "ref")
	mustGit(t, env.ApeDir, "commit", "-q", "-m", "ref")
	os.Remove(filepath.Join(env.ApeDir, "ref", "gone.md"))
	writeFile(t, filepath.Join(env.MainRoot, "ref", "gone.md"), "code's\n")

	run(t, env, Commit, "-m", "both", "DECISIONS.md", "ref/gone.md")
	got := mustGit(t, env.ApeDir, "show", "--name-status", "--format=%s", "HEAD")
	if want := "both\n\nM\tDECISIONS.md\nD\tref/gone.md"; got != want {
		t.Fatalf("HEAD:\n%s\nwant:\n%s", got, want)
	}
	// Neither in ape/ nor in its HEAD: still the code repo's, refused.
	wantCode(t, env, ExitUsage, Commit, "-m", "x", "ref/gone.md")
}

// A directory is the files under it that differ from HEAD: new (in new subdirectories too), edited, deleted;
// ignored ones and other directories' changes stay out (T36).
func TestCommitDirectory(t *testing.T) {
	env := project(t)
	ape := env.ApeDir
	writeFile(t, filepath.Join(ape, "ref", "audit", "old.md"), "old\n")
	writeFile(t, filepath.Join(ape, "ref", "audit", "gone.md"), "gone\n")
	writeFile(t, filepath.Join(ape, "ref", "kept", "k.md"), "k\n")
	writeFile(t, filepath.Join(ape, "ref", "dead", "d.md"), "d\n")
	mustGit(t, ape, "add", "ref")
	mustGit(t, ape, "commit", "-q", "-m", "ref")
	writeFile(t, filepath.Join(ape, "ref", "audit", "old.md"), "changed\n")
	os.Remove(filepath.Join(ape, "ref", "audit", "gone.md"))
	writeFile(t, filepath.Join(ape, "ref", "audit", "new", "n*.md"), "n\n")
	writeFile(t, filepath.Join(ape, "ref", "audit", "x.log"), "ignored\n")
	writeFile(t, filepath.Join(ape, ".gitignore"), "*.log\n")
	writeFile(t, filepath.Join(ape, "ref", "other.md"), "not named\n")
	os.RemoveAll(filepath.Join(ape, "ref", "dead"))

	env.Cwd = env.MainRoot
	run(t, env, Commit, "-m", "audit", "ape/ref/audit/", "ref/kept", "ref/dead")
	got := mustGit(t, ape, "show", "--name-status", "--format=%s", "HEAD")
	if want := "audit\n\nD\tref/audit/gone.md\nA\tref/audit/new/n*.md\nM\tref/audit/old.md\nD\tref/dead/d.md"; got != want {
		t.Fatalf("HEAD:\n%s\nwant:\n%s", got, want)
	}
	if st := mustGit(t, ape, "status", "--porcelain"); st != "?? .gitignore\n?? ref/other.md" {
		t.Fatalf("status:\n%s", st)
	}
	if out := run(t, env, Commit, "-m", "again", "ref/audit", "ref/kept"); out != "nothing to commit\n" {
		t.Fatalf("nothing to commit printed %q", out)
	}
}

func TestCommitRefuses(t *testing.T) {
	env := project(t)
	writeFile(t, filepath.Join(env.ApeDir, "ref", "a.md"), "a\n")
	n := len(subjects(t, env))
	for _, args := range [][]string{
		{"ape/ref/a.md"},                        // no message
		{"-m", " ", "ape/ref/a.md"},             // blank message
		{"-m", "x"},                             // no paths
		{"-m", "x", "README"},                   // in the code repo
		{"-m", "x", "ape/../README"},            // the same, spelt through ape/
		{"-m", "x", filepath.Dir(env.MainRoot)}, // further out
		{"-m", "x", "ape"},                      // the ape dir itself
		{"-m", "x", "ape/.git/config"},          // ape's git dir
		{"-m", "x", "ape/ref/a.md", "nope/b"},   // one bad path refuses all
	} {
		wantCode(t, env, ExitUsage, Commit, args...)
	}
	if len(subjects(t, env)) != n {
		t.Fatal("a refused commit committed")
	}
}
