package commands

import (
	"bytes"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/AaronCQL/ape/framework"
	"github.com/AaronCQL/ape/internal/locate"
)

// wtProject is a code repo with an ape dir set up by hand (as `ape init` would leave it), for start/pause/board.
type wtProject struct {
	t    *testing.T
	root string // main root, symlink-free
	ape  string
	out  bytes.Buffer
}

func newWtProject(t *testing.T) *wtProject {
	t.Helper()
	home := t.TempDir()
	for k, v := range map[string]string{
		"HOME": home, "XDG_CONFIG_HOME": home, "GIT_CONFIG_NOSYSTEM": "1", "APE_DIR": "",
		"GIT_AUTHOR_NAME": "t", "GIT_AUTHOR_EMAIL": "t@t", "GIT_COMMITTER_NAME": "t", "GIT_COMMITTER_EMAIL": "t@t",
	} {
		t.Setenv(k, v)
	}
	tmp, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	p := &wtProject{t: t, root: filepath.Join(tmp, "repo")}
	p.ape = filepath.Join(p.root, "ape")
	p.write(p.root, ".gitignore", "/ape\n")
	p.write(p.root, "src/main.go", "package main\n")
	p.git(p.root, "init", "-q", "-b", "main")
	p.git(p.root, "add", "-A")
	p.git(p.root, "commit", "-q", "-m", "code")

	p.write(p.ape, "_FRAMEWORK.md", string(framework.FrameworkMD()))
	p.write(p.ape, "tasks/.keep", "")
	p.git(p.ape, "init", "-q", "-b", "main")
	p.git(p.ape, "add", "-A")
	p.git(p.ape, "commit", "-q", "-m", "init")
	return p
}

func (p *wtProject) env() Env {
	p.out.Reset()
	return Env{Cwd: p.root, ApeDir: p.ape, MainRoot: p.root, Out: &p.out}
}

func (p *wtProject) write(dir, name, content string) {
	p.t.Helper()
	f := filepath.Join(dir, name)
	if err := os.MkdirAll(filepath.Dir(f), 0o755); err != nil {
		p.t.Fatal(err)
	}
	if err := os.WriteFile(f, []byte(content), 0o644); err != nil {
		p.t.Fatal(err)
	}
}

func (p *wtProject) git(dir string, args ...string) string {
	p.t.Helper()
	out, err := exec.Command("git", append([]string{"-C", dir}, args...)...).CombinedOutput()
	if err != nil {
		p.t.Fatalf("git %s: %v\n%s", strings.Join(args, " "), err, out)
	}
	return strings.TrimSpace(string(out))
}

// task writes and commits tasks/<name>.md with header lines kv ("status: ready", ...) and a title.
func (p *wtProject) task(name, title string, kv ...string) {
	p.t.Helper()
	body := "---\n" + strings.Join(kv, "\n") + "\n---\n# " + title + "\n"
	p.write(p.ape, "tasks/"+name+".md", body)
	p.git(p.ape, "add", "-A")
	p.git(p.ape, "commit", "-q", "-m", "task "+name)
}

func (p *wtProject) run(cmd func(Env, []string) error, args ...string) (string, error) {
	err := cmd(p.env(), args)
	return p.out.String(), err
}

// mustRun runs cmd and fails the test on an error.
func (p *wtProject) mustRun(cmd func(Env, []string) error, args ...string) string {
	p.t.Helper()
	out, err := p.run(cmd, args...)
	if err != nil {
		p.t.Fatalf("%v: %v", args, err)
	}
	return out
}

func (p *wtProject) wt(name string) string { return locate.WorktreePath(p.root, name) }

// wantExit checks err is an *Error with code (and, if msg isn't "", that message).
func wantExit(t *testing.T, err error, code int, msg string) {
	t.Helper()
	var e *Error
	if !errors.As(err, &e) || e.Code != code {
		t.Fatalf("err = %v, want exit %d", err, code)
	}
	if msg != "" && e.Msg != msg {
		t.Fatalf("msg = %q, want %q", e.Msg, msg)
	}
}

func pathExists(p string) bool {
	_, err := os.Lstat(p)
	return err == nil
}

func TestStart(t *testing.T) {
	p := newWtProject(t)
	p.task("T12-login", "Login", "kind: feat", "pri: 1", "status: ready", "area: src/")

	out := p.mustRun(Start, "12")
	wt := p.wt("T12-login")
	if want := wt + "\nbranch ape/T12-login\n"; out != want {
		t.Fatalf("out = %q, want %q", out, want)
	}
	if got := p.git(wt, "rev-parse", "--abbrev-ref", "HEAD"); got != "ape/T12-login" {
		t.Fatalf("worktree on %q", got)
	}
	if got, want := p.git(wt, "rev-parse", "HEAD"), p.git(p.root, "rev-parse", "HEAD"); got != want {
		t.Fatalf("branch at %s, want main's HEAD %s", got, want)
	}
	link := filepath.Join(wt, "ape")
	if target, err := os.Readlink(link); err != nil || target != p.ape {
		t.Fatalf("ape link -> %q (%v), want %q", target, err, p.ape)
	}
	if got, err := locate.ApeDir(filepath.Join(wt, "src")); err != nil || got != p.ape {
		t.Fatalf("locate.ApeDir from the worktree = %q, %v; want %q", got, err, p.ape)
	}
	if got := p.git(wt, "status", "--porcelain"); got != "" {
		t.Fatalf("fresh worktree not clean: %q", got)
	}

	_, err := p.run(Start, "12")
	wantExit(t, err, ExitConflict, "taken: worktree "+wt)
}

func TestStartRefuses(t *testing.T) {
	p := newWtProject(t)
	p.task("T1-idea", "Idea", "kind: feat", "status: new")
	p.task("T2-approve", "Approve", "kind: owner", "pri: 1", "status: ready")
	p.task("T3-wait", "Wait", "kind: feat", "pri: 1", "status: blocked", "area: x", "needs: 1")

	for _, tc := range []struct {
		args []string
		code int
		msg  string
	}{
		{[]string{"1"}, ExitUsage, "T1 is new; triage it"},
		{[]string{"3"}, ExitUsage, "T3 needs 1; once that's resolved: ape set T3 status=ready needs="},
		{[]string{"2"}, ExitUsage, "T2 is kind owner; only the owner can do it"},
		{[]string{"9"}, ExitNotFound, ""},
		{[]string{"x"}, ExitUsage, ""},
		{nil, ExitUsage, ""},
		{[]string{"1", "2"}, ExitUsage, ""},
	} {
		_, err := p.run(Start, tc.args...)
		wantExit(t, err, tc.code, tc.msg)
	}
	if pathExists(p.root + "-wt") {
		t.Fatal("a refused start made a worktree dir")
	}
}

func TestStartResumesPaused(t *testing.T) {
	p := newWtProject(t)
	p.task("T4-csv", "CSV", "kind: feat", "pri: 2", "status: ready", "area: src/")
	p.mustRun(Start, "4")
	wt := p.wt("T4-csv")
	p.write(wt, "src/csv.go", "package main\n")
	p.git(wt, "add", "-A")
	p.git(wt, "commit", "-q", "-m", "csv")
	sha := p.git(wt, "rev-parse", "HEAD")

	if out := p.mustRun(Pause, "4"); out != "paused T4; branch ape/T4-csv kept\n" {
		t.Fatalf("pause out = %q", out)
	}
	if pathExists(wt) {
		t.Fatal("worktree still there after pause")
	}
	if got := p.git(p.root, "rev-parse", "ape/T4-csv"); got != sha {
		t.Fatalf("branch at %s, want %s", got, sha)
	}

	p.git(p.root, "commit", "-q", "--allow-empty", "-m", "main moves on")
	p.mustRun(Start, "4")
	if got := p.git(wt, "rev-parse", "HEAD"); got != sha {
		t.Fatalf("resumed at %s, want the paused branch's %s", got, sha)
	}
	if !pathExists(filepath.Join(wt, "src/csv.go")) || !pathExists(filepath.Join(wt, "ape/_FRAMEWORK.md")) {
		t.Fatal("resumed worktree lacks the earlier commit or the ape link")
	}
}

func TestPauseDirty(t *testing.T) {
	p := newWtProject(t)
	p.task("T5-x", "X", "kind: bug", "pri: 1", "status: ready", "area: src/")
	p.mustRun(Start, "5")
	wt := p.wt("T5-x")
	want := "worktree " + wt + " has uncommitted changes; commit or stash, then pause"

	p.write(wt, "src/main.go", "package changed\n")
	_, err := p.run(Pause, "5")
	wantExit(t, err, ExitConflict, want)
	p.git(wt, "checkout", "--", "src/main.go")

	p.write(wt, "new.txt", "untracked\n")
	_, err = p.run(Pause, "5")
	wantExit(t, err, ExitConflict, want)
	if !pathExists(filepath.Join(wt, "ape")) {
		t.Fatal("a refused pause removed the ape link")
	}
	os.Remove(filepath.Join(wt, "new.txt"))

	p.mustRun(Pause, "5")
	if pathExists(wt) {
		t.Fatal("worktree still there")
	}
	if !pathExists(filepath.Join(p.ape, "_FRAMEWORK.md")) {
		t.Fatal("removing the worktree touched the ape dir behind the link")
	}
	_, err = p.run(Pause, "5")
	wantExit(t, err, ExitUsage, "T5 has no worktree; nothing to pause")
	_, err = p.run(Pause, "6")
	wantExit(t, err, ExitNotFound, "")
}

// A code repo that doesn't ignore ape/: start warns, pause still works (the link is untracked, not ignored).
func TestApeNotIgnored(t *testing.T) {
	p := newWtProject(t)
	p.git(p.root, "rm", "-q", ".gitignore")
	p.git(p.root, "commit", "-q", "-m", "no ignore")
	p.task("T6-y", "Y", "kind: chore", "pri: 3", "status: ready", "area: docs/")

	out := p.mustRun(Start, "6")
	if !strings.HasSuffix(out, "\nwarning: ape/ not ignored by the code repo; run ape init in the main worktree\n") {
		t.Fatalf("out = %q, want the warning", out)
	}
	p.mustRun(Pause, "6")
	if pathExists(p.wt("T6-y")) {
		t.Fatal("worktree still there")
	}
}

func TestPauseDeletedWorktree(t *testing.T) {
	p := newWtProject(t)
	p.task("T7-z", "Z", "kind: chore", "pri: 3", "status: ready", "area: docs/")
	p.mustRun(Start, "7")
	if err := os.RemoveAll(p.wt("T7-z")); err != nil {
		t.Fatal(err)
	}
	p.mustRun(Pause, "7")
	if strings.Contains(p.git(p.root, "worktree", "list"), "T7-z") {
		t.Fatal("worktree still registered")
	}
	p.mustRun(Start, "7") // and it can be started again
}
