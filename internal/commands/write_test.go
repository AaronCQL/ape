package commands

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/AaronCQL/ape/framework"
	"github.com/AaronCQL/ape/internal/gitx"
	"github.com/AaronCQL/ape/internal/locate"
)

// isolateGit keeps the user's git config, hooks and identity out of the test.
func isolateGit(t *testing.T) {
	t.Helper()
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(home, ".config"))
	t.Setenv("GIT_CONFIG_NOSYSTEM", "1")
	t.Setenv("APE_DIR", "")
	for _, k := range []string{"GIT_AUTHOR", "GIT_COMMITTER"} {
		t.Setenv(k+"_NAME", "t")
		t.Setenv(k+"_EMAIL", "t@t")
	}
}

// codeRepoT makes a code repo on branch main with one commit; it returns its resolved root.
func codeRepoT(t *testing.T) string {
	t.Helper()
	root, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	root = filepath.Join(root, "proj")
	if err := os.Mkdir(root, 0o755); err != nil {
		t.Fatal(err)
	}
	mustGit(t, root, "init", "-q", "-b", "main")
	writeFile(t, filepath.Join(root, "README"), "hi\n")
	mustGit(t, root, "add", "README")
	mustGit(t, root, "commit", "-q", "-m", "first")
	return root
}

// project is a code repo after ape init, and the Env commands get there.
func project(t *testing.T) Env {
	t.Helper()
	isolateGit(t)
	root := codeRepoT(t)
	if err := Init(Env{Cwd: root, Out: io.Discard}, nil); err != nil {
		t.Fatal(err)
	}
	// Without init's task 1, so tests number their own tasks from 1.
	apeDir := filepath.Join(root, "ape")
	mustGit(t, apeDir, "rm", "-q", "tasks/T1-setup-ape.md")
	writeFile(t, filepath.Join(apeDir, ".next"), "1\n")
	mustGit(t, apeDir, "commit", "-q", "--amend", "--no-edit", "-a")
	ape, main, err := locate.Find(root)
	if err != nil {
		t.Fatal(err)
	}
	return Env{Cwd: root, ApeDir: ape, MainRoot: main, Out: io.Discard}
}

func mustGit(t *testing.T, dir string, args ...string) string {
	t.Helper()
	out, err := gitx.Repo{Dir: dir}.Run(args...)
	if err != nil {
		t.Fatal(err)
	}
	return out
}

func writeFile(t *testing.T, p, s string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte(s), 0o644); err != nil {
		t.Fatal(err)
	}
}

func readFile(t *testing.T, p string) string {
	t.Helper()
	b, err := os.ReadFile(p)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

func exists(p string) bool {
	_, err := os.Lstat(p)
	return err == nil
}

// run runs cmd and returns its output.
func run(t *testing.T, env Env, cmd func(Env, []string) error, args ...string) string {
	t.Helper()
	var out bytes.Buffer
	env.Out = &out
	if err := cmd(env, args); err != nil {
		t.Fatalf("%q: %v", args, err)
	}
	return out.String()
}

// wantCode runs cmd and checks it fails with code; it returns the message.
func wantCode(t *testing.T, env Env, code int, cmd func(Env, []string) error, args ...string) string {
	t.Helper()
	var out bytes.Buffer
	env.Out = &out
	err := cmd(env, args)
	var e *Error
	if !errors.As(err, &e) || e.Code != code {
		t.Fatalf("%q: got %v, want exit %d", args, err, code)
	}
	if out.Len() > 0 {
		t.Errorf("%q: failed but printed %q", args, out.String())
	}
	return e.Msg
}

// subjects is the ape repo's commit subjects, newest first.
func subjects(t *testing.T, env Env) []string {
	t.Helper()
	return strings.Split(mustGit(t, env.ApeDir, "log", "--format=%s"), "\n")
}

func taskPath(env Env, name string) string { return filepath.Join(env.ApeDir, "tasks", name) }

// startT does what `ape start` does: branch, worktree, ape symlink. It returns the worktree path.
func startT(t *testing.T, env Env, name string) string {
	t.Helper()
	wt := locate.WorktreePath(env.MainRoot, name)
	mustGit(t, env.MainRoot, "worktree", "add", "-q", "-b", "ape/"+name, wt, "main")
	if err := os.Symlink(env.ApeDir, filepath.Join(wt, "ape")); err != nil {
		t.Fatal(err)
	}
	return wt
}

// commitIn commits a new file in dir.
func commitIn(t *testing.T, dir, file string) {
	t.Helper()
	writeFile(t, filepath.Join(dir, file), file+"\n")
	mustGit(t, dir, "add", file)
	mustGit(t, dir, "commit", "-q", "-m", "add "+file)
}

func TestInitClaudeMD(t *testing.T) {
	isolateGit(t)
	root := codeRepoT(t)
	writeFile(t, filepath.Join(root, "CLAUDE.md"), "# Claude\n")
	writeFile(t, filepath.Join(root, ".claude/CLAUDE.md"), "")
	out := run(t, Env{Cwd: root, Out: io.Discard}, Init)
	for _, name := range []string{"AGENTS.md", "CLAUDE.md", ".claude/CLAUDE.md"} {
		if !strings.Contains(out, filepath.Join(root, name)+": points at ape/\n") {
			t.Errorf("output lacks %s:\n%s", name, out)
		}
	}
	if got, want := readFile(t, filepath.Join(root, "CLAUDE.md")), string(framework.AgentsPointer)+"\n# Claude\n"; got != want {
		t.Errorf("CLAUDE.md = %q, want %q", got, want)
	}
	if got := readFile(t, filepath.Join(root, ".claude/CLAUDE.md")); got != string(framework.AgentsPointer) {
		t.Errorf(".claude/CLAUDE.md = %q", got)
	}
	if !strings.HasSuffix(out, "commit .claude/CLAUDE.md, AGENTS.md and CLAUDE.md in the code repo: worktrees branch from HEAD\nnext: T1 (ape/tasks/T1-setup-ape.md): move what's only yours into ape/, shared rules stay; read it before starting\n") {
		t.Errorf("reminder:\n%s", out)
	}
	mustGit(t, root, "add", "-A")
	mustGit(t, root, "commit", "-q", "-m", "ape")
	if out := run(t, Env{Cwd: root, Out: io.Discard}, Init); out != "up to date\n" {
		t.Errorf("rerun = %q", out)
	}
}

func TestInitClaudeImportsAgents(t *testing.T) {
	isolateGit(t)
	root := codeRepoT(t)
	writeFile(t, filepath.Join(root, "AGENTS.md"), "# Rules\n")
	writeFile(t, filepath.Join(root, "CLAUDE.md"), "@AGENTS.md\n")
	writeFile(t, filepath.Join(root, ".claude/CLAUDE.md"), "x\n@../AGENTS.md\n")
	run(t, Env{Cwd: root, Out: io.Discard}, Init)
	if got := readFile(t, filepath.Join(root, "CLAUDE.md")); got != "@AGENTS.md\n" {
		t.Errorf("CLAUDE.md = %q", got)
	}
	if got := readFile(t, filepath.Join(root, ".claude/CLAUDE.md")); got != "x\n@../AGENTS.md\n" {
		t.Errorf(".claude/CLAUDE.md = %q", got)
	}
	root2 := codeRepoT(t)
	writeFile(t, filepath.Join(root2, "AGENTS.md"), "# Rules\n")
	if err := os.Symlink("AGENTS.md", filepath.Join(root2, "CLAUDE.md")); err != nil {
		t.Fatal(err)
	}
	run(t, Env{Cwd: root2, Out: io.Discard}, Init)
	if fi, err := os.Lstat(filepath.Join(root2, "CLAUDE.md")); err != nil || fi.Mode()&os.ModeSymlink == 0 {
		t.Errorf("CLAUDE.md link replaced: %v %v", fi, err)
	}
	if got := readFile(t, filepath.Join(root2, "AGENTS.md")); !strings.HasPrefix(got, string(framework.AgentsPointer)) {
		t.Errorf("AGENTS.md = %q", got)
	}
}

func TestInit(t *testing.T) {
	isolateGit(t)
	root := codeRepoT(t)
	writeFile(t, filepath.Join(root, "AGENTS.md"), "# Rules\nbe nice\n")
	writeFile(t, filepath.Join(root, ".gitignore"), "/bin")
	env := Env{Cwd: filepath.Join(root), Out: io.Discard}

	out := run(t, env, Init)
	ape := filepath.Join(root, "ape")
	for _, want := range []string{ape + ": created (git repo)", ape + "/_FRAMEWORK.md: created",
		ape + "/PROJECT.md: created", ape + "/DECISIONS.md: created", ape + "/tasks/.keep: created",
		ape + "/tasks/T1-setup-ape.md: created", ape + "/.next: created", ape + ": committed init", root + "/.git/info/exclude: added /ape",
		root + "/AGENTS.md: points at ape/"} {
		if !strings.Contains(out, want+"\n") {
			t.Errorf("output lacks %q:\n%s", want, out)
		}
	}
	if got := readFile(t, filepath.Join(ape, "_FRAMEWORK.md")); got != string(framework.FrameworkMD()) {
		t.Error("_FRAMEWORK.md isn't the embedded one")
	}
	if fi, err := os.Stat(filepath.Join(ape, "_FRAMEWORK.md")); err != nil || fi.Mode().Perm() != 0o444 {
		t.Errorf("_FRAMEWORK.md mode: %v %v", fi.Mode(), err)
	}
	if got := readFile(t, filepath.Join(ape, ".next")); got != "2\n" {
		t.Errorf(".next = %q", got)
	}
	if got := readFile(t, filepath.Join(ape, "tasks/T1-setup-ape.md")); got != string(framework.SetupTask) {
		t.Error("task 1 isn't the embedded setup task")
	}
	if got := mustGit(t, ape, "ls-files"); got != ".next\nDECISIONS.md\nPROJECT.md\n_FRAMEWORK.md\ntasks/.keep\ntasks/T1-setup-ape.md" {
		t.Errorf("committed: %q", got)
	}
	if got := mustGit(t, ape, "status", "--porcelain"); got != "" {
		t.Errorf("ape repo dirty: %q", got)
	}
	if got := readFile(t, filepath.Join(root, ".gitignore")); got != "/bin" {
		t.Errorf(".gitignore = %q", got)
	}
	if got := readFile(t, filepath.Join(root, ".git/info/exclude")); !strings.HasSuffix(got, "\n/ape\n") {
		t.Errorf("info/exclude = %q", got)
	}
	agents := readFile(t, filepath.Join(root, "AGENTS.md"))
	if want := string(framework.AgentsPointer) + "\n# Rules\nbe nice\n"; agents != want {
		t.Errorf("AGENTS.md = %q, want %q", agents, want)
	}
	if got := mustGit(t, root, "log", "--format=%s"); got != "first" {
		t.Errorf("init committed in the code repo: %q", got)
	}

	// Twice: nothing changes.
	if out := run(t, env, Init); out != "up to date\ncommit AGENTS.md in the code repo: worktrees branch from HEAD\n" {
		t.Errorf("second init: %q", out)
	}
	if got := mustGit(t, ape, "rev-list", "--count", "HEAD"); got != "1" {
		t.Errorf("second init committed: %s commits", got)
	}
	if got := readFile(t, filepath.Join(root, "AGENTS.md")); got != agents {
		t.Errorf("second init changed AGENTS.md: %q", got)
	}

	// An outdated framework file and pointer block are rewritten; from a subdirectory too.
	fw := filepath.Join(ape, "_FRAMEWORK.md")
	os.Chmod(fw, 0o644)
	writeFile(t, fw, "old\n")
	os.Chmod(fw, 0o444)
	mustGit(t, ape, "commit", "-q", "-am", "old")
	writeFile(t, filepath.Join(root, "AGENTS.md"), "x\n<!-- ape -->\nold pointer\n<!-- /ape -->\n\n# Rules\n")
	sub := filepath.Join(root, "sub")
	os.Mkdir(sub, 0o755)
	out = run(t, Env{Cwd: sub}, Init)
	if out != fw+": updated to "+framework.Version+"\n"+root+"/AGENTS.md: points at ape/\ncommit AGENTS.md in the code repo: worktrees branch from HEAD\n" {
		t.Errorf("update output: %q", out)
	}
	if readFile(t, fw) != string(framework.FrameworkMD()) {
		t.Error("_FRAMEWORK.md not rewritten")
	}
	if fi, _ := os.Stat(fw); fi.Mode().Perm() != 0o444 {
		t.Errorf("rewritten _FRAMEWORK.md mode %v", fi.Mode())
	}
	if got := subjects(t, Env{ApeDir: ape})[0]; got != "framework: "+framework.Version {
		t.Errorf("commit %q", got)
	}
	if got, want := readFile(t, filepath.Join(root, "AGENTS.md")), string(framework.AgentsPointer)+"\nx\n# Rules\n"; got != want {
		t.Errorf("AGENTS.md = %q, want %q", got, want)
	}
	if exists(filepath.Join(sub, "ape")) {
		t.Error("init from a subdirectory made ape/ there")
	}
}

// No AGENTS.md, no info/: both created. A /ape line in .gitignore is left there, and ape/ is excluded anyway.
func TestInitNoAgentsNoInfo(t *testing.T) {
	isolateGit(t)
	root := codeRepoT(t)
	if err := os.RemoveAll(filepath.Join(root, ".git/info")); err != nil {
		t.Fatal(err)
	}
	writeFile(t, filepath.Join(root, ".gitignore"), "/ape\n")
	run(t, Env{Cwd: root}, Init)
	if got := readFile(t, filepath.Join(root, "AGENTS.md")); got != string(framework.AgentsPointer) {
		t.Errorf("AGENTS.md = %q", got)
	}
	if got := readFile(t, filepath.Join(root, ".gitignore")); got != "/ape\n" {
		t.Errorf(".gitignore = %q", got)
	}
	if got := readFile(t, filepath.Join(root, ".git/info/exclude")); got != "/ape\n" {
		t.Errorf("info/exclude = %q", got)
	}
}

// Start right after init, nothing committed in the code repo: the worktree's ape link is ignored (T11).
func TestInitThenStartClean(t *testing.T) {
	isolateGit(t)
	root := codeRepoT(t)
	run(t, Env{Cwd: root, Out: io.Discard}, Init)
	env := Env{Cwd: root, ApeDir: filepath.Join(root, "ape"), MainRoot: root, Out: io.Discard}
	writeFile(t, taskPath(env, "T2-x.md"), "---\nkind: chore\npri: 1\nstatus: ready\narea: x/\n---\n# X\n")
	mustGit(t, env.ApeDir, "add", "-A")
	mustGit(t, env.ApeDir, "commit", "-q", "-m", "x")
	if out := run(t, env, Start, "2"); strings.Contains(out, "warning") {
		t.Errorf("start warned:\n%s", out)
	}
	wt := locate.WorktreePath(root, "T2-x")
	if got := mustGit(t, wt, "status", "--porcelain", "--untracked-files=all"); got != "" {
		t.Errorf("worktree status = %q", got)
	}
}

func TestInitErrors(t *testing.T) {
	isolateGit(t)
	notRepo := t.TempDir()
	wantCode(t, Env{Cwd: notRepo}, ExitUsage, Init)
	if exists(filepath.Join(notRepo, "ape")) {
		t.Error("made ape/ outside a repo")
	}

	root := codeRepoT(t)
	wantCode(t, Env{Cwd: root}, ExitUsage, Init, "extra")

	wt := filepath.Join(filepath.Dir(root), "linked")
	mustGit(t, root, "worktree", "add", "-q", "-b", "other", wt)
	if msg := wantCode(t, Env{Cwd: wt}, ExitUsage, Init); !strings.Contains(msg, root) {
		t.Errorf("message doesn't name the main worktree: %s", msg)
	}

	os.Mkdir(filepath.Join(root, "ape"), 0o755)
	wantCode(t, Env{Cwd: root}, ExitUsage, Init)
}

func TestInitAfterFailedCommit(t *testing.T) {
	isolateGit(t)
	root := codeRepoT(t)
	ape := filepath.Join(root, "ape")
	// A first init whose commit failed leaves an ape repo without commits; rerunning finishes it.
	os.Mkdir(ape, 0o755)
	mustGit(t, ape, "init", "-q")
	writeFile(t, filepath.Join(ape, "PROJECT.md"), "mine\n")
	run(t, Env{Cwd: root}, Init)
	if got := subjects(t, Env{ApeDir: ape}); len(got) != 1 || got[0] != "init: set up ape/" {
		t.Errorf("commits %q", got)
	}
	if readFile(t, filepath.Join(ape, "PROJECT.md")) != "mine\n" {
		t.Error("overwrote PROJECT.md")
	}
}

func TestNew(t *testing.T) {
	env := project(t)
	out := run(t, env, New, "login-timeout", "--kind", "bug", "--from=web@3f2a9c1")
	p := taskPath(env, "T1-login-timeout.md")
	if out != p+"\n" {
		t.Errorf("output %q", out)
	}
	if got := readFile(t, p); got != "---\nkind: bug\nstatus: new\nfrom: web@3f2a9c1\n---\n# login timeout\n\n" {
		t.Errorf("task:\n%s", got)
	}
	run(t, env, New, "--title", "Export  to\nCSV", "export")
	if got := readFile(t, taskPath(env, "T2-export.md")); got != "---\nstatus: new\n---\n# Export to CSV\n\n" {
		t.Errorf("task:\n%s", got)
	}
	if got := readFile(t, filepath.Join(env.ApeDir, ".next")); got != "3\n" {
		t.Errorf(".next = %q", got)
	}
	if got := subjects(t, env)[:2]; got[0] != "new(T2): Export to CSV" || got[1] != "new(T1): login timeout" {
		t.Errorf("commits %q", got)
	}
	if got := mustGit(t, env.ApeDir, "status", "--porcelain"); got != "" {
		t.Errorf("ape repo dirty: %q", got)
	}

	for _, args := range [][]string{
		{}, {"a", "b"}, {"Bad"}, {"a--b"}, {"-x"}, {"x", "--kind", "epic"}, {"x", "--kind"}, {"x", "--nope"},
	} {
		wantCode(t, env, ExitUsage, New, args...)
	}
	if got := readFile(t, filepath.Join(env.ApeDir, ".next")); got != "3\n" {
		t.Errorf("failed new changed .next: %q", got)
	}
}

func TestNewLabelsAndBody(t *testing.T) {
	env := project(t)
	run(t, env, New, "dep")
	env.In = strings.NewReader("\nWhat happened.\n## Ask\nShip it? rec: yes.\n\n")
	run(t, env, New, "flaky", "--title", "Flaky login", "pri=2", "kind=bug", "area=auth/  web/", "status=blocked",
		"needs=1")
	want := "---\nkind: bug\npri: 2\nstatus: blocked\narea: auth/ web/\nneeds: T1\n---\n# Flaky login\n\n" +
		"What happened.\n## Ask\nShip it? rec: yes.\n"
	if got := readFile(t, taskPath(env, "T2-flaky.md")); got != want {
		t.Errorf("task:\n%s", got)
	}
	if got := subjects(t, env)[:2]; got[0] != "new(T2): Flaky login" || got[1] != "new(T1): dep" {
		t.Errorf("commits %q", got)
	}
	if got := mustGit(t, env.ApeDir, "status", "--porcelain"); got != "" {
		t.Errorf("ape repo dirty: %q", got)
	}
	env.In = strings.NewReader(" \n")
	run(t, env, New, "blank", "--kind", "chore", "kind=feat")
	if got := readFile(t, taskPath(env, "T3-blank.md")); got != "---\nkind: feat\nstatus: new\n---\n# blank\n\n" {
		t.Errorf("task:\n%s", got)
	}

	env.In = nil
	for _, c := range []struct {
		code int
		msg  string
		args []string
	}{
		{ExitUsage, "unknown key", []string{"x", "prio=1"}},
		{ExitUsage, "bad pri", []string{"x", "pri=4"}},
		{ExitUsage, "ready without pri, ready without area", []string{"x", "kind=bug", "status=ready"}},
		{ExitUsage, "needs on a task that isn't blocked", []string{"x", "needs=T1"}},
		{ExitNotFound, "no T9", []string{"x", "status=blocked", "needs=T9"}},
		{ExitUsage, "can't need itself", []string{"x", "status=blocked", "needs=T4"}},
	} {
		if msg := wantCode(t, env, c.code, New, c.args...); !strings.Contains(msg, c.msg) {
			t.Errorf("%q: %q lacks %q", c.args, msg, c.msg)
		}
	}
	if got := readFile(t, filepath.Join(env.ApeDir, ".next")); got != "4\n" {
		t.Errorf("refused new changed .next: %q", got)
	}
}

func TestNewNeverReuses(t *testing.T) {
	env := project(t)
	for _, s := range []string{"a", "b", "c"} {
		run(t, env, New, s)
	}
	run(t, env, Drop, "3", "gone")
	run(t, env, Drop, "2", "gone")
	run(t, env, New, "d")
	if !exists(taskPath(env, "T4-d.md")) {
		t.Fatal("new after drops didn't take 4")
	}

	// Without .next: from history, though the highest tasks are deleted.
	run(t, env, Drop, "4", "gone")
	os.Remove(filepath.Join(env.ApeDir, ".next"))
	run(t, env, New, "e")
	if !exists(taskPath(env, "T5-e.md")) {
		t.Fatal("new without .next didn't take 5")
	}
	if got := readFile(t, filepath.Join(env.ApeDir, ".next")); got != "6\n" {
		t.Errorf(".next = %q", got)
	}

	// A stale .next doesn't duplicate a task in tasks/.
	writeFile(t, filepath.Join(env.ApeDir, ".next"), "2\n")
	run(t, env, New, "f")
	if !exists(taskPath(env, "T6-f.md")) {
		t.Fatal("stale .next: didn't take 6")
	}

	writeFile(t, filepath.Join(env.ApeDir, ".next"), "x\n")
	wantCode(t, env, ExitUsage, New, "g")
}

func TestSet(t *testing.T) {
	env := project(t)
	p := taskPath(env, "T1-thing.md")
	body := "# Thing\r\nbody  with trailing spaces   \n\n## Ask\nwhy? rec: yes\n\n\n"
	writeFile(t, p, "---\nkind:bug\nstatus: new\nzzz: keep\n---\n"+body)
	mustGit(t, env.ApeDir, "add", ".")
	mustGit(t, env.ApeDir, "commit", "-q", "-m", "hand")

	out := run(t, env, Set, "1", "pri=1", "status=ready", "area=web/  api/", "needs=")
	want := p + "\nkind: bug\npri: 1\nstatus: ready\narea: web/ api/\nzzz: keep\n" + `warning: unknown key "zzz"` + "\n"
	if out != want {
		t.Errorf("output %q, want %q", out, want)
	}
	if got := readFile(t, p); got != "---\nkind:bug\npri: 1\nstatus: ready\narea: web/ api/\nzzz: keep\n---\n"+body {
		t.Errorf("task:\n%q", got)
	}
	if got := subjects(t, env)[0]; got != "set(T1): pri=1 status=ready area=web/ api/ needs=" {
		t.Errorf("commit %q", got)
	}

	// k= deletes; warnings for an incomplete task.
	out = run(t, env, Set, "0001", "kind=", "status=blocked", "needs=owner")
	if !strings.Contains(out, "\nwarning: missing kind\n") || strings.Contains(out, "kind:") {
		t.Errorf("output %q", out)
	}
	if got := readFile(t, p); got != "---\npri: 1\nstatus: blocked\narea: web/ api/\nneeds: owner\nzzz: keep\n---\n"+body {
		t.Errorf("task:\n%q", got)
	}

	// needs takes several tasks, whitespace collapsed, written as IDs.
	run(t, env, New, "two")
	run(t, env, New, "three")
	run(t, env, Set, "1", "needs= 2\t t3 ")
	if got := readFile(t, p); !strings.Contains(got, "\nneeds: T2 T3\n") {
		t.Errorf("task:\n%q", got)
	}

	before := readFile(t, p)
	head := mustGit(t, env.ApeDir, "rev-parse", "HEAD")
	for _, args := range [][]string{
		{"1"}, {"x", "pri=1"}, {"1", "pri"}, {"1", "title=x"}, {"1", "kind=epic"}, {"1", "status=doing"},
		{"1", "pri=4"}, {"1", "needs=me"}, {"1", "needs=0"}, {"1", "needs=owner 2"}, {"1", "needs=2,3"},
		{"1", "pri=1", "status=nope"}, {"1", "zzz="}, {"1", "excl=no"},
	} {
		wantCode(t, env, ExitUsage, Set, args...)
	}
	wantCode(t, env, ExitNotFound, Set, "9", "pri=1")
	if readFile(t, p) != before || mustGit(t, env.ApeDir, "rev-parse", "HEAD") != head {
		t.Error("a failed set wrote")
	}
}

// status=triage claims a task: a second claim is a conflict and writes nothing; other keys still set.
func TestSetTriageClaim(t *testing.T) {
	env := project(t)
	run(t, env, New, "bug")
	run(t, env, Set, "1", "status=triage")
	head := mustGit(t, env.ApeDir, "rev-parse", "HEAD")
	for _, args := range [][]string{{"1", "status=triage"}, {"1", "kind=bug", "status=triage"}} {
		if msg := wantCode(t, env, ExitConflict, Set, args...); msg != "T1 is already in triage" {
			t.Errorf("message %q", msg)
		}
	}
	if mustGit(t, env.ApeDir, "rev-parse", "HEAD") != head {
		t.Error("a refused claim committed")
	}
	run(t, env, Set, "1", "kind=bug", "pri=2", "area=x/", "status=ready")
	run(t, env, Set, "1", "status=triage") // back from ready: a claim again
}

func TestDone(t *testing.T) {
	env := project(t)
	run(t, env, New, "plain")
	out := run(t, env, Done, "1", "nothing  to\nit")
	if out != taskPath(env, "T1-plain.md")+" deleted\n" {
		t.Errorf("output %q", out)
	}
	if exists(taskPath(env, "T1-plain.md")) {
		t.Error("task still there")
	}
	if got := subjects(t, env)[0]; got != "done(T1): nothing to it" {
		t.Errorf("commit %q", got)
	}

	run(t, env, New, "feature")
	wt := startT(t, env, "T2-feature")
	commitIn(t, wt, "feature.go")

	// Unmerged: refused, nothing touched.
	if msg := wantCode(t, env, ExitConflict, Done, "2", "shipped"); !strings.Contains(msg, "not merged into main") {
		t.Errorf("message %q", msg)
	}
	mustGit(t, env.MainRoot, "merge", "-q", "--ff-only", "ape/T2-feature")

	// Merged but dirty: refused, worktree and its ape link kept.
	writeFile(t, filepath.Join(wt, "scratch"), "x")
	if msg := wantCode(t, env, ExitConflict, Done, "2", "shipped"); !strings.Contains(msg, "commit or stash") {
		t.Errorf("message %q", msg)
	}
	if !exists(filepath.Join(wt, "ape")) || !exists(taskPath(env, "T2-feature.md")) {
		t.Fatal("a refused done removed something")
	}
	os.Remove(filepath.Join(wt, "scratch"))

	out = run(t, env, Done, "2", "shipped")
	if want := taskPath(env, "T2-feature.md") + " deleted\nremoved worktree " + wt + ", branch ape/T2-feature\n"; out != want {
		t.Errorf("output %q, want %q", out, want)
	}
	if exists(wt) || exists(taskPath(env, "T2-feature.md")) {
		t.Error("worktree or task left")
	}
	if !exists(filepath.Join(env.ApeDir, "_FRAMEWORK.md")) {
		t.Fatal("removing the worktree followed its ape link")
	}
	if got := mustGit(t, env.MainRoot, "branch", "--list", "ape/*"); got != "" {
		t.Errorf("branch left: %q", got)
	}
	short := mustGit(t, env.MainRoot, "rev-parse", "--short", "HEAD")
	if got := subjects(t, env)[0]; got != "done(T2): shipped (code "+short+")" {
		t.Errorf("commit %q", got)
	}
	if got := mustGit(t, env.ApeDir, "status", "--porcelain"); got != "" {
		t.Errorf("ape repo dirty: %q", got)
	}

	wantCode(t, env, ExitNotFound, Done, "2", "again")
	run(t, env, New, "x")
	for _, args := range [][]string{{"3"}, {"3", "  "}, {"3", "a", "b"}, {"x", "y"}, {"3", "y", "--force"}} {
		wantCode(t, env, ExitUsage, Done, args...)
	}
}

func TestDoneMergedBranchNoWorktree(t *testing.T) {
	env := project(t)
	run(t, env, New, "paused")
	wt := startT(t, env, "T1-paused")
	commitIn(t, wt, "a")
	mustGit(t, env.MainRoot, "worktree", "remove", "--force", wt)
	mustGit(t, env.MainRoot, "merge", "-q", "--ff-only", "ape/T1-paused")
	out := run(t, env, Done, "1", "ok")
	if !strings.HasSuffix(out, "\nremoved branch ape/T1-paused\n") {
		t.Errorf("output %q", out)
	}
}

// A squash merge (or a forge's) leaves the branch's tip out of main's history; done and drop take it as merged.
func TestFinishSquashMerged(t *testing.T) {
	env := project(t)
	for i, verb := range []func(Env, []string) error{Done, Drop} {
		name := fmt.Sprintf("T%d-sq", i+1)
		run(t, env, New, "sq")
		wt := startT(t, env, name)
		commitIn(t, wt, fmt.Sprintf("a%d.go", i))
		commitIn(t, wt, fmt.Sprintf("b%d.go", i))
		mustGit(t, env.MainRoot, "merge", "-q", "--squash", "ape/"+name)
		mustGit(t, env.MainRoot, "commit", "-q", "-m", "squash")
		n := fmt.Sprint(i + 1)
		run(t, env, verb, n, "ok")
		if exists(wt) || mustGit(t, env.MainRoot, "branch", "--list", "ape/*") != "" {
			t.Errorf("task %s: worktree or branch left", n)
		}
	}

	// Squashed, then more work on the branch: not merged.
	run(t, env, New, "more")
	wt := startT(t, env, "T3-more")
	commitIn(t, wt, "c.go")
	mustGit(t, env.MainRoot, "merge", "-q", "--squash", "ape/T3-more")
	mustGit(t, env.MainRoot, "commit", "-q", "-m", "squash")
	commitIn(t, wt, "d.go")
	wantCode(t, env, ExitConflict, Done, "3", "ok")
	wantCode(t, env, ExitConflict, Drop, "3", "ok")
}

func TestDrop(t *testing.T) {
	env := project(t)
	run(t, env, New, "idea")
	wt := startT(t, env, "T1-idea")
	commitIn(t, wt, "half.go")
	writeFile(t, filepath.Join(wt, "dirty"), "x")

	if msg := wantCode(t, env, ExitConflict, Drop, "1", "not worth it"); !strings.Contains(msg, "--force") {
		t.Errorf("message %q", msg)
	}
	if !exists(wt) || !exists(taskPath(env, "T1-idea.md")) {
		t.Fatal("a refused drop removed something")
	}
	run(t, env, Drop, "1", "not worth it", "--force")
	if exists(wt) || exists(taskPath(env, "T1-idea.md")) {
		t.Error("worktree or task left")
	}
	if !exists(filepath.Join(env.ApeDir, "_FRAMEWORK.md")) {
		t.Fatal("removing the worktree followed its ape link")
	}
	if got := mustGit(t, env.MainRoot, "branch", "--list", "ape/*"); got != "" {
		t.Errorf("branch left: %q", got)
	}
	if got := subjects(t, env)[0]; got != "drop(T1): not worth it" {
		t.Errorf("commit %q", got)
	}

	// A branch with nothing unmerged goes without --force; a dirty worktree still needs it.
	run(t, env, New, "empty")
	wt = startT(t, env, "T2-empty")
	writeFile(t, filepath.Join(wt, "dirty"), "x")
	wantCode(t, env, ExitConflict, Drop, "2", "dup of T1")
	os.Remove(filepath.Join(wt, "dirty"))
	run(t, env, Drop, "2", "dup of T1")
	if exists(wt) || mustGit(t, env.MainRoot, "branch", "--list", "ape/*") != "" {
		t.Error("worktree or branch left")
	}

	wantCode(t, env, ExitNotFound, Drop, "2", "again")
	run(t, env, New, "x")
	for _, args := range [][]string{{"3"}, {"3", ""}, {"3", "a", "--forse"}, {"--force", "3"}} {
		wantCode(t, env, ExitUsage, Drop, args...)
	}
}

// writeTasks writes and commits tasks by hand: name → header lines (without ---), title "# <name>".
func writeTasks(t *testing.T, env Env, tasks map[string]string) {
	t.Helper()
	for name, header := range tasks {
		writeFile(t, taskPath(env, name+".md"), "---\n"+header+"---\n# "+name+"\n")
	}
	mustGit(t, env.ApeDir, "add", ".")
	mustGit(t, env.ApeDir, "commit", "-q", "-m", "hand")
}

const blockedOn = "kind: feat\npri: 2\nstatus: blocked\narea: x/\nneeds: "

// done releases what waited on it in its own commit; drop releases nothing and says who still needs it.
func TestDoneReleases(t *testing.T) {
	env := project(t)
	writeTasks(t, env, map[string]string{
		"T1-base":     "kind: feat\npri: 1\nstatus: ready\narea: x/\n",
		"T2-next":     blockedOn + "T1\n",
		"T3-two":      blockedOn + "T1 T4\n",
		"T4-other":    "kind: feat\npri: 1\nstatus: ready\narea: y/\n",
		"T5-owner":    blockedOn + "owner\n",
		"T6-edited":   blockedOn + "T1\n",
		"T7-broken":   "needs: T1\n",
		"T8-notready": "kind: feat\nstatus: triage\nneeds: T1\n",
	})
	writeFile(t, taskPath(env, "T7-broken.md"), "needs: T1\nno header\n")
	mustGit(t, env.ApeDir, "commit", "-q", "-am", "break T7")
	writeFile(t, taskPath(env, "T6-edited.md"), "---\n"+blockedOn+"T1\n---\n# T6-edited\nhand edit\n")
	before := map[string]string{}
	for _, n := range []string{"T5-owner", "T7-broken"} {
		before[n] = readFile(t, taskPath(env, n+".md"))
	}

	out := run(t, env, Done, "1", "landed")
	if want := taskPath(env, "T1-base.md") + " deleted\nreleased: T2 T6\n"; out != want {
		t.Errorf("output %q, want %q", out, want)
	}
	for name, want := range map[string]string{
		"T2-next":     "---\nkind: feat\npri: 2\nstatus: ready\narea: x/\n---\n# T2-next\n",
		"T3-two":      "---\n" + blockedOn + "T4\n---\n# T3-two\n",
		"T6-edited":   "---\nkind: feat\npri: 2\nstatus: ready\narea: x/\n---\n# T6-edited\nhand edit\n",
		"T8-notready": "---\nkind: feat\nstatus: triage\n---\n# T8-notready\n",
		"T5-owner":    before["T5-owner"],
		"T7-broken":   before["T7-broken"],
	} {
		if got := readFile(t, taskPath(env, name+".md")); got != want {
			t.Errorf("%s:\n%q\nwant\n%q", name, got, want)
		}
	}
	if got := subjects(t, env)[:2]; !reflect.DeepEqual(got, []string{"done(T1): landed", "edit(T6): hand edits"}) {
		t.Errorf("commits %q", got)
	}
	files := mustGit(t, env.ApeDir, "show", "--name-only", "--format=", "HEAD")
	if want := "tasks/T1-base.md\ntasks/T2-next.md\ntasks/T3-two.md\ntasks/T6-edited.md\ntasks/T8-notready.md"; files != want {
		t.Errorf("done commit has\n%s\nwant\n%s", files, want)
	}
	if got := mustGit(t, env.ApeDir, "status", "--porcelain"); got != "" {
		t.Errorf("ape repo dirty: %q", got)
	}

	// drop: T3 keeps needing T4, and the board says it was dropped.
	out = run(t, env, Drop, "4", "not needed")
	if want := taskPath(env, "T4-other.md") + " deleted\nstill needed by T3: ape set their needs to what replaces it, or drop them\n"; out != want {
		t.Errorf("output %q, want %q", out, want)
	}
	if got := mustGit(t, env.ApeDir, "show", "--name-only", "--format=", "HEAD"); got != "tasks/T4-other.md" {
		t.Errorf("drop commit has %q", got)
	}
	if got := readFile(t, taskPath(env, "T3-two.md")); !strings.Contains(got, "\nneeds: T4\n") {
		t.Errorf("drop released T3:\n%s", got)
	}
	board := run(t, env, Board)
	for _, line := range []string{
		"  T3   feat  p2  T3-two   [needs T4 (gone)]",
		"  T3: needs T4, dropped; ape set T3 needs=… (what replaces it), or drop T3",
	} {
		if !strings.Contains(board, "\n"+line+"\n") {
			t.Errorf("board lacks %q:\n%s", line, board)
		}
	}
}

// set needs=… names open tasks only, never the task itself, and never closes a loop.
func TestSetNeedsChecks(t *testing.T) {
	env := project(t)
	writeTasks(t, env, map[string]string{
		"T1-a": "kind: feat\npri: 1\nstatus: ready\narea: x/\n",
		"T2-b": blockedOn + "T1\n",
		"T3-c": blockedOn + "T2\n",
		"T4-d": "kind: feat\npri: 1\nstatus: ready\narea: x/\n",
		"T5-e": "kind: feat\npri: 1\nstatus: ready\narea: x/\n",
	})
	run(t, env, Done, "4", "landed")
	run(t, env, Drop, "5", "no")
	head := mustGit(t, env.ApeDir, "rev-parse", "HEAD")
	for _, tc := range []struct {
		code int
		args []string
		msg  string
	}{
		{ExitUsage, []string{"1", "status=blocked", "needs=1"}, "T1 can't need itself"},
		{ExitUsage, []string{"1", "needs=T4"}, "T4 is done; nothing to wait for"},
		{ExitUsage, []string{"1", "needs=5"}, "T5 was dropped; need what replaces it"},
		{ExitNotFound, []string{"1", "needs=9"}, "no T9 in " + filepath.Join(env.ApeDir, "tasks")},
		{ExitUsage, []string{"1", "needs=T3"}, "needs would loop: T1 → T3 → T2 → T1; drop one of them"},
		{ExitUsage, []string{"2", "needs=T3 T1"}, "needs would loop: T2 → T3 → T2; drop one of them"},
	} {
		if msg := wantCode(t, env, tc.code, Set, tc.args...); msg != tc.msg {
			t.Errorf("%q: %q, want %q", tc.args, msg, tc.msg)
		}
	}
	if mustGit(t, env.ApeDir, "rev-parse", "HEAD") != head {
		t.Error("a refused set committed")
	}
	run(t, env, Set, "3", "needs=T1 T2") // no loop: fine
	run(t, env, Set, "1", "needs=owner", "status=blocked")
	run(t, env, Set, "1", "needs=", "status=ready")
}
