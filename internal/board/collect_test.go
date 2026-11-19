package board

import (
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"testing"
	"time"

	"github.com/AaronCQL/ape/framework"
)

// TestCollect runs Collect on a real code repo with an ape dir: a worktree without an own commit gets Started
// (its creation time), one with a commit doesn't; an unreadable task still counts and keeps its worktree's task.
func TestCollect(t *testing.T) {
	t.Setenv("GIT_CONFIG_GLOBAL", os.DevNull)
	t.Setenv("GIT_CONFIG_NOSYSTEM", "1")
	tmp, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	root, ape := filepath.Join(tmp, "repo"), filepath.Join(tmp, "repo", "ape")
	if err := os.MkdirAll(filepath.Join(ape, "tasks"), 0o755); err != nil {
		t.Fatal(err)
	}
	writeT(t, filepath.Join(ape, "_FRAMEWORK.md"), string(framework.FrameworkMD()))
	writeT(t, filepath.Join(ape, "tasks", "T1-fresh.md"), "---\nkind: bug\npri: 1\nstatus: ready\narea: a/\n---\n# A\n")
	writeT(t, filepath.Join(ape, "tasks", "T2-busy.md"), "---\nkind: bug\n\nstatus: ready\n---\n# Blank line\n")
	gitT(t, ape, "init", "-q", "-b", "main")
	gitT(t, root, "init", "-q", "-b", "main")
	gitT(t, root, "commit", "-q", "--allow-empty", "-m", "init")
	fresh, busy := filepath.Join(tmp, "repo-wt", "T1-fresh"), filepath.Join(tmp, "repo-wt", "T2-busy")
	gitT(t, root, "worktree", "add", "-q", "-b", "ape/T1-fresh", fresh)
	gitT(t, root, "worktree", "add", "-q", "-b", "ape/T2-busy", busy)
	gitT(t, busy, "commit", "-q", "--allow-empty", "-m", "work")
	created := time.Date(2026, 11, 12, 7, 0, 0, 0, time.UTC)
	// The admin dir is named after the worktree's basename here; Collect asks git for it.
	if err := os.Chtimes(filepath.Join(root, ".git", "worktrees", "T1-fresh", "commondir"), created, created); err != nil {
		t.Fatal(err)
	}
	// Activity in the worktree (an index write) doesn't move its creation time.
	writeT(t, filepath.Join(fresh, "f"), "x")
	gitT(t, fresh, "add", "f")

	s, err := Collect(ape, root, now)
	if err != nil {
		t.Fatal(err)
	}
	if len(s.Tasks) != 2 || s.Tasks[1].Err == nil {
		t.Fatalf("tasks = %+v; want 2, the second unreadable", s.Tasks)
	}
	if got := s.Started[fresh]; !got.Equal(created) {
		t.Errorf("Started[%s] = %v; want %v", fresh, got, created)
	}
	if _, ok := s.Started[busy]; ok || s.LastOwn[busy].IsZero() {
		t.Errorf("ape/T2-busy: Started = %v, LastOwn = %v; want no Started, a LastOwn", s.Started, s.LastOwn)
	}
	b := Build(s)
	for _, line := range b.Check {
		if line == "ape/T2-busy: worktree without a task" {
			t.Errorf("unreadable task lost its worktree: %q", b.Check)
		}
	}
}

func gitT(t *testing.T, dir string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", append([]string{"-C", dir, "-c", "user.name=t", "-c", "user.email=t@t"}, args...)...)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, out)
	}
}

func writeT(t *testing.T, path, data string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(data), 0o644); err != nil {
		t.Fatal(err)
	}
}

// TestCollectConflicts runs the merge checks on real branches: a DOING task against main (real and regenerated-only
// conflicts), a paused READY task against a DOING one, a clean pair; and Dropped from the ape repo's log.
func TestCollectConflicts(t *testing.T) {
	t.Setenv("GIT_CONFIG_GLOBAL", os.DevNull)
	t.Setenv("GIT_CONFIG_NOSYSTEM", "1")
	tmp, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	root, ape := filepath.Join(tmp, "repo"), filepath.Join(tmp, "repo", "ape")
	if err := os.MkdirAll(filepath.Join(ape, "tasks"), 0o755); err != nil {
		t.Fatal(err)
	}
	ready := "---\nkind: bug\npri: 1\nstatus: ready\narea: a/\n---\n# A\n"
	for _, name := range []string{"T1-real", "T2-gen", "T3-paused", "T4-clean"} {
		writeT(t, filepath.Join(ape, "tasks", name+".md"), ready)
	}
	writeT(t, filepath.Join(ape, "tasks", "T5-blocked.md"),
		"---\nkind: bug\npri: 1\nstatus: blocked\narea: a/\nneeds: T8 T9\n---\n# B\n")
	gitT(t, ape, "init", "-q", "-b", "main")
	gitT(t, ape, "commit", "-q", "--allow-empty", "-m", "done(T8): landed")
	gitT(t, ape, "commit", "-q", "--allow-empty", "-m", "drop(T9): never mind")

	gitT(t, root, "init", "-q", "-b", "trunk")
	writeT(t, filepath.Join(root, "f"), "one\n")
	writeT(t, filepath.Join(root, "gen.txt"), "one\n")
	writeT(t, filepath.Join(root, ".gitattributes"), "gen.txt linguist-generated\n")
	gitT(t, root, "add", "f", "gen.txt", ".gitattributes")
	gitT(t, root, "commit", "-q", "-m", "init")
	edit := func(dir, file, text string) {
		t.Helper()
		writeT(t, filepath.Join(dir, file), text)
		gitT(t, dir, "add", file)
		gitT(t, dir, "commit", "-q", "-m", text)
	}
	wt := func(name string) string {
		p := filepath.Join(tmp, "repo-wt", name)
		gitT(t, root, "worktree", "add", "-q", "-b", "ape/"+name, p)
		return p
	}
	edit(wt("T1-real"), "f", "real\n")
	edit(wt("T2-gen"), "gen.txt", "gen\n")
	gitT(t, root, "branch", "ape/T3-paused")
	edit(wt("T4-clean"), "other", "x\n")
	gitT(t, root, "checkout", "-q", "ape/T3-paused")
	edit(root, "f", "paused\n")
	gitT(t, root, "checkout", "-q", "trunk")
	edit(root, "f", "main\n")
	edit(root, "gen.txt", "main\n")

	s, err := Collect(ape, root, now)
	if err != nil {
		t.Fatal(err)
	}
	wantMain := map[int]Conflict{1: {Files: []string{"f"}}, 2: {Files: []string{"gen.txt"}, Regenerated: true}}
	if !reflect.DeepEqual(s.MainConflicts, wantMain) || s.MainBranch != "trunk" {
		t.Errorf("MainConflicts = %v on %q; want %v on trunk", s.MainConflicts, s.MainBranch, wantMain)
	}
	if want := map[[2]int]Conflict{{3, 1}: {Files: []string{"f"}}}; !reflect.DeepEqual(s.Conflicts, want) {
		t.Errorf("Conflicts = %v; want %v", s.Conflicts, want)
	}
	if want := map[int]bool{8: false, 9: true}; !reflect.DeepEqual(s.Dropped, want) {
		t.Errorf("Dropped = %v; want %v", s.Dropped, want)
	}
}
