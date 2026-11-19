package locate

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

// setup makes <tmp>/repo (a git repo with ape/ in it) and a linked worktree <tmp>/repo-wt/T12-x whose `ape` links
// to it. It returns the resolved main root and the worktree path.
func setup(t *testing.T) (root, wt string) {
	t.Helper()
	t.Setenv("APE_DIR", "")
	t.Setenv("GIT_CONFIG_GLOBAL", os.DevNull)
	t.Setenv("GIT_CONFIG_NOSYSTEM", "1")
	tmp, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	root = filepath.Join(tmp, "repo")
	mkdir(t, filepath.Join(root, "src", "deep"))
	mkdir(t, filepath.Join(root, "ape", "tasks"))
	if err := os.WriteFile(filepath.Join(root, "ape", "_FRAMEWORK.md"), nil, 0o444); err != nil {
		t.Fatal(err)
	}
	git(t, root, "init", "-q", "-b", "main")
	git(t, root, "-c", "user.name=t", "-c", "user.email=t@t", "commit", "-q", "--allow-empty", "-m", "init")
	wt = WorktreePath(root, "T12-x")
	git(t, root, "worktree", "add", "-q", "-b", "ape/T12-x", wt)
	mkdir(t, filepath.Join(wt, "src"))
	if err := os.Symlink(filepath.Join(root, "ape"), filepath.Join(wt, "ape")); err != nil {
		t.Fatal(err)
	}
	return root, wt
}

func mkdir(t *testing.T, p string) {
	t.Helper()
	if err := os.MkdirAll(p, 0o755); err != nil {
		t.Fatal(err)
	}
}

func git(t *testing.T, dir string, args ...string) {
	t.Helper()
	if out, err := exec.Command("git", append([]string{"-C", dir}, args...)...).CombinedOutput(); err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, out)
	}
}

func TestFind(t *testing.T) {
	root, wt := setup(t)
	ape := filepath.Join(root, "ape")
	findFrom(t, ape, root, map[string]string{
		"root":               root,
		"nested subdir":      filepath.Join(root, "src", "deep"),
		"inside ape":         ape,
		"inside ape/tasks":   filepath.Join(ape, "tasks"),
		"worktree":           wt,
		"worktree subdir":    filepath.Join(wt, "src"),
		"via worktree's ape": filepath.Join(wt, "ape", "tasks"),
	})
	if got, err := ApeDir(wt); err != nil || got != ape {
		t.Errorf("ApeDir = %q, %v; want %q", got, err, ape)
	}
}

// TestFindApeElsewhere: the main root's `ape` is a symlink to a directory outside the repo (its own git repo).
// The main root is still the code repo's, not the ape dir's parent.
func TestFindApeElsewhere(t *testing.T) {
	root, wt := setup(t)
	ape := filepath.Join(filepath.Dir(root), "notes", "ape")
	mkdir(t, filepath.Dir(ape))
	if err := os.Rename(filepath.Join(root, "ape"), ape); err != nil {
		t.Fatal(err)
	}
	git(t, ape, "init", "-q")
	if err := os.Symlink(ape, filepath.Join(root, "ape")); err != nil {
		t.Fatal(err)
	}
	findFrom(t, ape, root, map[string]string{
		"root":                   root,
		"nested subdir":          filepath.Join(root, "src", "deep"),
		"via root's ape":         filepath.Join(root, "ape", "tasks"),
		"worktree":               filepath.Join(wt, "src"),
		"via worktree's ape":     filepath.Join(wt, "ape", "tasks"),
		"worktree's ape itself":  filepath.Join(wt, "ape"),
		"via root's ape, itself": filepath.Join(root, "ape"),
	})
}

// findFrom checks Find from each cwd (as given, symlinks unresolved, as a shell's $PWD keeps them).
func findFrom(t *testing.T, ape, root string, cwds map[string]string) {
	t.Helper()
	for name, cwd := range cwds {
		gotApe, gotRoot, err := Find(cwd)
		if err != nil || gotApe != ape || gotRoot != root {
			t.Errorf("%s: Find(%s) = %q, %q, %v; want %q, %q", name, cwd, gotApe, gotRoot, err, ape, root)
		}
	}
}

func TestApeDirNotFound(t *testing.T) {
	root, _ := setup(t)
	if _, err := ApeDir(filepath.Dir(root)); !errors.Is(err, ErrNotFound) {
		t.Fatalf("err = %v, want ErrNotFound", err)
	}
	// An ape/ without _FRAMEWORK.md isn't one.
	other := filepath.Join(filepath.Dir(root), "other")
	mkdir(t, filepath.Join(other, "ape", "tasks"))
	if _, err := ApeDir(filepath.Join(other, "ape", "tasks")); !errors.Is(err, ErrNotFound) {
		t.Fatalf("ape/ without _FRAMEWORK.md: err = %v, want ErrNotFound", err)
	}
}

func TestFindEnv(t *testing.T) {
	root, wt := setup(t)
	ape := filepath.Join(root, "ape")
	t.Setenv("APE_DIR", filepath.Join(wt, "ape"))
	// Outside any repo: the ape dir's parent.
	if gotApe, gotRoot, err := Find(t.TempDir()); err != nil || gotApe != ape || gotRoot != root {
		t.Fatalf("outside a repo: Find = %q, %q, %v", gotApe, gotRoot, err)
	}
	// In a repo: that repo's main root, wherever $APE_DIR points.
	other := filepath.Join(filepath.Dir(root), "other")
	mkdir(t, filepath.Join(other, "sub"))
	git(t, other, "init", "-q")
	if gotApe, gotRoot, err := Find(filepath.Join(other, "sub")); err != nil || gotApe != ape || gotRoot != other {
		t.Fatalf("in another repo: Find = %q, %q, %v; want root %q", gotApe, gotRoot, err, other)
	}
	if _, gotRoot, err := Find(filepath.Join(wt, "src")); err != nil || gotRoot != root {
		t.Fatalf("in a linked worktree: root = %q, %v; want %q", gotRoot, err, root)
	}
	t.Setenv("APE_DIR", filepath.Join(root, "src"))
	if _, err := ApeDir(root); err == nil {
		t.Fatal("$APE_DIR without _FRAMEWORK.md: no error")
	}
}

func TestWorktreePath(t *testing.T) {
	if got := WorktreePath("/a/repo", "T12-x"); got != "/a/repo-wt/T12-x" {
		t.Fatalf("WorktreePath = %q", got)
	}
}
