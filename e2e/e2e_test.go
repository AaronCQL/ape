// Package e2e runs the real ape binary in temp git repos: one realistic scenario, the error surface, and races
// between concurrent ape processes (and hand commits). SPEC.md "Tests".
package e2e

import (
	"bytes"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"
)

// apeBin is the binary TestMain builds; env is the isolated environment every process here runs with.
var (
	apeBin string
	env    []string
)

func TestMain(m *testing.M) {
	os.Exit(testMain(m))
}

func testMain(m *testing.M) int {
	dir, err := os.MkdirTemp("", "ape-e2e-")
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	defer os.RemoveAll(dir)
	apeBin = filepath.Join(dir, "ape")
	build := exec.Command("go", "build", "-o", apeBin, ".")
	build.Dir = ".."
	if out, err := build.CombinedOutput(); err != nil {
		fmt.Fprintf(os.Stderr, "go build: %v\n%s", err, out)
		return 1
	}

	home := filepath.Join(dir, "home")
	if err := os.MkdirAll(filepath.Join(home, ".config"), 0o755); err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	// A fixed default branch, so messages name "main" whatever the machine's git defaults to.
	if err := os.WriteFile(filepath.Join(home, ".gitconfig"), []byte("[init]\n\tdefaultBranch = main\n"), 0o644); err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	for _, kv := range os.Environ() {
		k, _, _ := strings.Cut(kv, "=")
		if strings.HasPrefix(k, "GIT_") || k == "HOME" || k == "XDG_CONFIG_HOME" || k == "APE_DIR" ||
			strings.HasPrefix(k, "LC_") || k == "LANG" || k == "PWD" {
			continue
		}
		env = append(env, kv)
	}
	env = append(env, "HOME="+home, "XDG_CONFIG_HOME="+filepath.Join(home, ".config"), "GIT_CONFIG_NOSYSTEM=1",
		"GIT_AUTHOR_NAME=e2e", "GIT_AUTHOR_EMAIL=e2e@example.com",
		"GIT_COMMITTER_NAME=e2e", "GIT_COMMITTER_EMAIL=e2e@example.com", "LC_ALL=C")
	return m.Run()
}

// result is one process's outcome.
type result struct {
	stdout, stderr string
	code           int
}

func (r result) String() string {
	return fmt.Sprintf("exit %d\nstdout:\n%sstderr:\n%s", r.code, r.stdout, r.stderr)
}

// run runs name with args in dir, with env plus extra; a failure to start fails the test.
func run(t testing.TB, dir string, extra []string, name string, args ...string) result {
	t.Helper()
	cmd := exec.Command(name, args...)
	cmd.Dir = dir
	cmd.Env = append(slices.Clip(env), extra...)
	var stdout, stderr bytes.Buffer
	cmd.Stdout, cmd.Stderr = &stdout, &stderr
	err := cmd.Run()
	code := 0
	if err != nil {
		ee, ok := err.(*exec.ExitError)
		if !ok { // Errorf, not Fatalf: races call this from their own goroutines
			t.Errorf("%s %q: %v", name, args, err)
			return result{code: -1}
		}
		code = ee.ExitCode()
	}
	return result{stdout.String(), stderr.String(), code}
}

// ape runs the binary in dir.
func ape(t testing.TB, dir string, args ...string) result {
	t.Helper()
	return run(t, dir, nil, apeBin, args...)
}

// mustApe runs ape in dir, requires exit 0 and no stderr, and returns stdout.
func mustApe(t testing.TB, dir string, args ...string) string {
	t.Helper()
	r := ape(t, dir, args...)
	if r.code != 0 || r.stderr != "" {
		t.Fatalf("ape %q in %s:\n%s", args, dir, r)
	}
	return r.stdout
}

// wantErr runs ape in dir and requires exit code with nothing on stdout and exactly one stderr line
// "ape: ...", containing want; it returns that line.
func wantErr(t testing.TB, dir string, code int, want string, args ...string) string {
	t.Helper()
	r := ape(t, dir, args...)
	line := strings.TrimSuffix(r.stderr, "\n")
	switch {
	case r.code != code:
		t.Errorf("ape %q: want exit %d\n%s", args, code, r)
	case r.stdout != "":
		t.Errorf("ape %q: want no stdout\n%s", args, r)
	case !strings.HasPrefix(r.stderr, "ape: ") || strings.Contains(line, "\n") || !strings.HasSuffix(r.stderr, "\n"):
		t.Errorf("ape %q: want one stderr line \"ape: ...\"\n%s", args, r)
	case !strings.Contains(line, want):
		t.Errorf("ape %q: stderr lacks %q\n%s", args, want, r)
	}
	return line
}

// git runs git -C dir args and returns trimmed stdout; it fails the test on error.
func git(t testing.TB, dir string, args ...string) string {
	t.Helper()
	r := run(t, dir, nil, "git", args...)
	if r.code != 0 {
		t.Fatalf("git %q in %s:\n%s", args, dir, r)
	}
	return strings.TrimSpace(r.stdout)
}

func write(t testing.TB, p, s string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte(s), 0o644); err != nil {
		t.Fatal(err)
	}
}

func read(t testing.TB, p string) string {
	t.Helper()
	data, err := os.ReadFile(p)
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}

func exists(p string) bool {
	_, err := os.Lstat(p)
	return err == nil
}

// codeRepo makes a code repo "code" with one commit in a fresh temp dir and returns its resolved root.
func codeRepo(t testing.TB) string {
	t.Helper()
	tmp, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	root := filepath.Join(tmp, "code")
	if err := os.Mkdir(root, 0o755); err != nil {
		t.Fatal(err)
	}
	git(t, root, "init", "-q")
	write(t, filepath.Join(root, "main.go"), "package main\n\nfunc main() {}\n")
	git(t, root, "add", "main.go")
	git(t, root, "commit", "-q", "-m", "first")
	return root
}

// project is a code repo after ape init, with the code repo's AGENTS.md committed.
func project(t testing.TB) string {
	t.Helper()
	root := codeRepo(t)
	mustApe(t, root, "init")
	withoutSetupTask(t, root)
	git(t, root, "add", "AGENTS.md")
	git(t, root, "commit", "-q", "-m", "ape")
	return root
}

// withoutSetupTask rewrites init's commit without task 1, so tests number their own tasks from 1.
func withoutSetupTask(t testing.TB, root string) {
	t.Helper()
	apeDir := filepath.Join(root, "ape")
	git(t, apeDir, "rm", "-q", "tasks/T1-setup-ape.md")
	write(t, filepath.Join(apeDir, ".next"), "1\n")
	git(t, apeDir, "commit", "-q", "--amend", "--no-edit", "-a")
}

// subjects is the ape repo's commit subjects, newest first.
func subjects(t testing.TB, root string) []string {
	t.Helper()
	return strings.Split(git(t, filepath.Join(root, "ape"), "log", "--format=%s"), "\n")
}

// Start right after init, nothing committed in the code repo: the worktree's ape link is ignored, so
// `git add -A` there can't commit it (T11).
func TestStartRightAfterInit(t *testing.T) {
	root := codeRepo(t)
	mustApe(t, root, "init")
	out := mustApe(t, root, "start", "1")
	if strings.Contains(out, "warning") {
		t.Errorf("start warned:\n%s", out)
	}
	wt, _, _ := strings.Cut(out, "\n")
	if !exists(filepath.Join(wt, "ape", "_FRAMEWORK.md")) {
		t.Fatalf("no ape link in %s", wt)
	}
	if got := git(t, wt, "status", "--porcelain", "--untracked-files=all"); got != "" {
		t.Errorf("worktree status = %q, want clean", got)
	}
	git(t, wt, "add", "-A")
	if got := git(t, wt, "diff", "--cached", "--name-only"); got != "" {
		t.Errorf("git add -A staged %q", got)
	}
}

func TestScenario(t *testing.T) {
	root := codeRepo(t)
	apeDir := filepath.Join(root, "ape")
	tasks := filepath.Join(apeDir, "tasks")
	wtDir := root + "-wt"

	// init: ape/ created and committed; the code repo's files changed but not committed, with a reminder.
	out := mustApe(t, root, "init")
	for _, want := range []string{
		apeDir + ": created (git repo)\n",
		apeDir + ": committed init\n",
		filepath.Join(root, ".git/info/exclude") + ": added /ape\n",
		filepath.Join(root, "AGENTS.md") + ": points at ape/\n",
		"commit AGENTS.md in the code repo",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("init output lacks %q:\n%s", want, out)
		}
	}
	if fi, err := os.Stat(filepath.Join(apeDir, "_FRAMEWORK.md")); err != nil || fi.Mode().Perm() != 0o444 {
		t.Errorf("_FRAMEWORK.md: %v, %v; want mode 0444", fi, err)
	}
	for _, f := range []string{"PROJECT.md", "DECISIONS.md", "tasks/.keep", "tasks/T1-setup-ape.md"} {
		if !exists(filepath.Join(apeDir, f)) {
			t.Errorf("init didn't create %s", f)
		}
	}
	if got := read(t, filepath.Join(apeDir, ".next")); got != "2\n" {
		t.Errorf(".next = %q, want 2", got)
	}
	if got := subjects(t, root); !slices.Equal(got, []string{"init: set up ape/"}) {
		t.Errorf("ape log = %q, want [init: set up ape/]", got)
	}
	if got := mustApe(t, root); !strings.Contains(got, "READY\n  T1   chore p1  Set up ape/ from what this repo has\n") ||
		strings.Contains(got, "CHECK") {
		t.Errorf("board after init:\n%s", got)
	}
	withoutSetupTask(t, root)
	if got := read(t, filepath.Join(root, ".git/info/exclude")); !strings.HasSuffix(got, "\n/ape\n") {
		t.Errorf("info/exclude = %q", got)
	}
	if exists(filepath.Join(root, ".gitignore")) {
		t.Error("init wrote .gitignore")
	}
	if got := read(t, filepath.Join(root, "AGENTS.md")); !strings.HasPrefix(got, "<!-- ape -->\n") {
		t.Errorf("AGENTS.md doesn't start with the pointer block:\n%s", got)
	}
	if got := git(t, root, "status", "--porcelain"); got != "?? AGENTS.md" {
		t.Errorf("code repo status after init = %q; want just AGENTS.md (ape/ ignored)", got)
	}
	git(t, root, "add", "AGENTS.md")
	git(t, root, "commit", "-q", "-m", "ape")

	// new ×4.
	for i, args := range [][]string{
		{"new", "login", "--kind", "feat", "--title", "OAuth login"},
		{"new", "flicker", "--kind", "bug", "--from", "web@3f2a9c1", "--title", "Dark mode flickers on load"},
		{"new", "pricing", "--kind", "owner", "--title", "Approve the pricing page copy"},
		{"new", "csv-export"},
	} {
		want := filepath.Join(tasks, fmt.Sprintf("T%d-%s.md", i+1, args[1])) + "\n"
		if got := mustApe(t, root, args...); got != want {
			t.Errorf("ape %q printed %q, want %q", args, got, want)
		}
	}
	if got := read(t, filepath.Join(tasks, "T2-flicker.md")); got !=
		"---\nkind: bug\nstatus: new\nfrom: web@3f2a9c1\n---\n# Dark mode flickers on load\n\n" {
		t.Errorf("T2-flicker.md =\n%s", got)
	}
	if got := read(t, filepath.Join(tasks, "T4-csv-export.md")); got != "---\nstatus: new\n---\n# csv export\n\n" {
		t.Errorf("T4-csv-export.md =\n%s", got)
	}
	if got := read(t, filepath.Join(apeDir, ".next")); got != "5\n" {
		t.Errorf(".next = %q, want 5", got)
	}

	board := func(dir, want string) {
		t.Helper()
		if got := mustApe(t, dir); got != want {
			t.Errorf("board in %s:\n%s\nwant:\n%s", dir, got, want)
		}
	}
	board(root, `ape: 4 tasks · 1 owner · 0 doing · 3 new · 0 blocked · 0 ready · 0 done 24h
OWNER
  T3   owner     Approve the pricing page copy   [needs owner]
NEW
  T1   feat      OAuth login
  T2   bug       Dark mode flickers on load   [from web@3f2a9c1]
  T4             csv export
`)

	// Triage.
	if got := mustApe(t, root, "set", "2", "status=triage"); got !=
		filepath.Join(tasks, "T2-flicker.md")+"\nkind: bug\nstatus: triage\nfrom: web@3f2a9c1\n" {
		t.Errorf("set T2 status=triage printed:\n%s", got)
	}
	// Triage is a claim: a second one loses.
	if got := wantErr(t, root, 2, "", "set", "2", "status=triage"); got != "ape: T2 is already in triage" {
		t.Errorf("second triage claim: %q", got)
	}
	mustApe(t, root, "set", "4", "kind=feat")
	board(root, `ape: 4 tasks · 1 owner · 0 doing · 3 new · 0 blocked · 0 ready · 0 done 24h
OWNER
  T3   owner     Approve the pricing page copy   [needs owner]
NEW
  T1   feat      OAuth login
  T2   bug       Dark mode flickers on load   [triage; from web@3f2a9c1]
  T4   feat      csv export
`)
	mustApe(t, root, "set", "1", "pri=1", "area=auth/ web/login", "status=ready")
	mustApe(t, root, "set", "2", "pri=2", "area=web/", "status=blocked", "needs=1  3")
	mustApe(t, root, "set", "3", "pri=1", "status=ready")
	mustApe(t, root, "set", "4", "pri=2", "area=export/", "status=blocked", "needs=owner")
	if got := read(t, filepath.Join(tasks, "T1-login.md")); got !=
		"---\nkind: feat\npri: 1\nstatus: ready\narea: auth/ web/login\n---\n# OAuth login\n\n" {
		t.Errorf("T1-login.md after set =\n%s", got)
	}
	board(root, `ape: 4 tasks · 2 owner · 0 doing · 0 new · 1 blocked · 1 ready · 0 done 24h
OWNER
  T3   owner p1  Approve the pricing page copy   [needs owner]
  T4   feat  p2  csv export   [needs owner]
BLOCKED
  T2   bug   p2  Dark mode flickers on load   [needs T1 T3]
READY
  T1   feat  p1  OAuth login
`)

	// start 1: worktree, branch, ape/ linked in.
	wt1 := filepath.Join(wtDir, "T1-login")
	if got := mustApe(t, root, "start", "1"); got != wt1+"\nbranch ape/T1-login\n" {
		t.Errorf("start 1 printed %q", got)
	}
	if target, err := os.Readlink(filepath.Join(wt1, "ape")); err != nil || target != apeDir {
		t.Errorf("worktree's ape link = %q, %v; want %s", target, err, apeDir)
	}
	if got := git(t, wt1, "branch", "--show-current"); got != "ape/T1-login" {
		t.Errorf("worktree on branch %q", got)
	}
	board(root, `ape: 4 tasks · 2 owner · 1 doing · 0 new · 1 blocked · 0 ready · 0 done 24h
OWNER
  T3   owner p1  Approve the pricing page copy   [needs owner]
  T4   feat  p2  csv export   [needs owner]
DOING
  T1   feat  p1  OAuth login   [no commits]
BLOCKED
  T2   bug   p2  Dark mode flickers on load   [needs T1 T3]
`)

	// Work in the worktree; ape works from inside it (and a subdir), finding the main ape dir.
	write(t, filepath.Join(wt1, "auth", "auth.go"), "package auth\n")
	git(t, wt1, "add", "auth")
	git(t, wt1, "commit", "-q", "-m", "auth")
	doing := `ape: 4 tasks · 2 owner · 1 doing · 0 new · 1 blocked · 0 ready · 0 done 24h
OWNER
  T3   owner p1  Approve the pricing page copy   [needs owner]
  T4   feat  p2  csv export   [needs owner]
DOING
  T1   feat  p1  OAuth login   [0m since commit]
BLOCKED
  T2   bug   p2  Dark mode flickers on load   [needs T1 T3]
`
	board(wt1, doing)
	board(filepath.Join(wt1, "auth"), doing)
	board(root, doing)
	// A write from inside the worktree lands in the main ape repo.
	if got := mustApe(t, filepath.Join(wt1, "auth"), "set", "3", "area=docs/"); !strings.HasPrefix(got,
		filepath.Join(tasks, "T3-pricing.md")+"\n") {
		t.Errorf("set from the worktree printed %q", got)
	}
	if got := subjects(t, root)[0]; got != "set(T3): area=docs/" {
		t.Errorf("last ape commit = %q", got)
	}
	if got := git(t, wt1, "status", "--porcelain"); got != "" {
		t.Errorf("worktree status = %q; the ape link should be ignored", got)
	}
	// A hand edit committed from the worktree, its path relative to ape/.
	task1 := filepath.Join(tasks, "T1-login.md")
	write(t, task1, read(t, task1)+"## Progress\nauth.go stub\n")
	if got := mustApe(t, wt1, "commit", "-m", "progress 1", "tasks/T1-login.md"); got != git(t, apeDir, "rev-parse", "--short", "HEAD")+"\n" {
		t.Errorf("commit from the worktree printed %q", got)
	}

	// The agent asks the owner: its doing task moves to OWNER, marked doing (T10); answered, back to DOING.
	mustApe(t, wt1, "set", "1", "status=blocked", "needs=owner")
	board(root, `ape: 4 tasks · 3 owner · 0 doing · 0 new · 1 blocked · 0 ready · 0 done 24h
OWNER
  T1   feat  p1  OAuth login   [needs owner; doing]
  T3   owner p1  Approve the pricing page copy   [needs owner]
  T4   feat  p2  csv export   [needs owner]
BLOCKED
  T2   bug   p2  Dark mode flickers on load   [needs T1 T3]
`)
	mustApe(t, root, "set", "1", "status=ready", "needs=")
	board(root, doing)

	// Not merged yet: done refuses.
	wantErr(t, root, 2, "ape/T1-login not merged into main; merge first", "done", "1", "login works")
	// Squash-merged (as forges do): its tip isn't in main's history, but merging it changes nothing.
	git(t, root, "merge", "-q", "--squash", "ape/T1-login")
	git(t, root, "commit", "-q", "-m", "login")
	short := git(t, root, "rev-parse", "--short", "HEAD")
	if got := mustApe(t, root, "done", "1", "login works;\n tests green"); got !=
		filepath.Join(tasks, "T1-login.md")+" deleted\nremoved worktree "+wt1+", branch ape/T1-login\n" {
		t.Errorf("done T1 printed %q", got)
	}
	if exists(wt1) || exists(filepath.Join(tasks, "T1-login.md")) {
		t.Error("done T1 left the worktree or the task")
	}
	if got := git(t, root, "branch", "--list", "ape/*"); got != "" {
		t.Errorf("branches left after done: %q", got)
	}
	if got := git(t, root, "worktree", "list", "--porcelain"); strings.Count(got, "worktree ") != 1 {
		t.Errorf("worktrees after done:\n%s", got)
	}
	doneLine := "  T1   done  login works; tests green (code " + short + ")\n"
	board(root, `ape: 3 tasks · 2 owner · 0 doing · 0 new · 1 blocked · 0 ready · 1 done 24h
OWNER
  T3   owner p1  Approve the pricing page copy   [needs owner]
  T4   feat  p2  csv export   [needs owner]
BLOCKED
  T2   bug   p2  Dark mode flickers on load   [needs T3]
DONE 24h
`+doneLine)

	// start 2, pause it (dirty refused), resume it.
	mustApe(t, root, "set", "2", "status=ready", "needs=")
	wantErr(t, root, 3, "no T1", "start", "1")
	wt2 := filepath.Join(wtDir, "T2-flicker")
	if got := mustApe(t, root, "start", "2"); got != wt2+"\nbranch ape/T2-flicker\n" {
		t.Errorf("start 2 printed %q", got)
	}
	wantErr(t, root, 2, "taken: worktree "+wt2, "start", "2")
	write(t, filepath.Join(wt2, "web", "theme.css"), "body{}\n")
	git(t, wt2, "add", "web")
	git(t, wt2, "commit", "-q", "-m", "theme")
	write(t, filepath.Join(wt2, "main.go"), "package main // wip\n")
	wantErr(t, root, 2, "uncommitted changes; commit or stash, then pause", "pause", "2")
	if !exists(wt2) {
		t.Fatal("a refused pause removed the worktree")
	}
	git(t, wt2, "checkout", "-q", "--", "main.go")
	if got := mustApe(t, root, "pause", "2"); got != "paused T2; branch ape/T2-flicker kept\n" {
		t.Errorf("pause 2 printed %q", got)
	}
	if exists(wt2) {
		t.Error("pause left the worktree")
	}
	if got := git(t, root, "branch", "--list", "ape/*"); got != "ape/T2-flicker" {
		t.Errorf("branches after pause: %q", got)
	}
	wantErr(t, root, 1, "T2 has no worktree", "pause", "2")
	board(root, `ape: 3 tasks · 2 owner · 0 doing · 0 new · 0 blocked · 1 ready · 1 done 24h
OWNER
  T3   owner p1  Approve the pricing page copy   [needs owner]
  T4   feat  p2  csv export   [needs owner]
READY
  T2   bug   p2  Dark mode flickers on load   [paused]
DONE 24h
`+doneLine)
	if got := mustApe(t, root, "start", "2"); got != wt2+"\nbranch ape/T2-flicker\n" {
		t.Errorf("resuming start 2 printed %q", got)
	}
	if got := read(t, filepath.Join(wt2, "web", "theme.css")); got != "body{}\n" {
		t.Errorf("resumed worktree lacks the paused branch's commit: %q", got)
	}
	if out := mustApe(t, root); !strings.Contains(out, "DOING\n  T2   bug   p2  Dark mode flickers on load   [0m since commit]\n") {
		t.Errorf("board after resume:\n%s", out)
	}

	// drop with unmerged work: refused, then --force.
	wantErr(t, root, 2, "ape/T2-flicker has commits not in main; merge them, or drop --force", "drop", "2", "dup")
	if !exists(wt2) || !exists(filepath.Join(tasks, "T2-flicker.md")) {
		t.Fatal("a refused drop removed something")
	}
	if got := mustApe(t, root, "drop", "2", "superseded by T4", "--force"); got !=
		filepath.Join(tasks, "T2-flicker.md")+" deleted\nremoved worktree "+wt2+", branch ape/T2-flicker\n" {
		t.Errorf("drop --force printed %q", got)
	}
	if exists(wt2) {
		t.Error("drop --force left the worktree")
	}
	if got := git(t, root, "branch", "--list", "ape/*"); got != "" {
		t.Errorf("branches after drop: %q", got)
	}
	board(root, `ape: 2 tasks · 2 owner · 0 doing · 0 new · 0 blocked · 0 ready · 2 done 24h
OWNER
  T3   owner p1  Approve the pricing page copy   [needs owner]
  T4   feat  p2  csv export   [needs owner]
DONE 24h
  T2   drop  superseded by T4
`+doneLine)

	// init again: nothing to do.
	if got := mustApe(t, root, "init"); got != "up to date\n" {
		t.Errorf("second init printed %q", got)
	}
	want := []string{
		"drop(T2): superseded by T4",
		"set(T2): status=ready needs=",
		"done(T1): login works; tests green (code " + short + ")",
		"set(T1): status=ready needs=",
		"set(T1): status=blocked needs=owner",
		"progress 1",
		"set(T3): area=docs/",
		"set(T4): pri=2 area=export/ status=blocked needs=owner",
		"set(T3): pri=1 status=ready",
		"set(T2): pri=2 area=web/ status=blocked needs=T1 T3",
		"set(T1): pri=1 area=auth/ web/login status=ready",
		"set(T4): kind=feat",
		"set(T2): status=triage",
		"new(T4): csv export", "new(T3): Approve the pricing page copy", "new(T2): Dark mode flickers on load", "new(T1): OAuth login",
		"init: set up ape/",
	}
	if got := subjects(t, root); !slices.Equal(got, want) {
		t.Errorf("ape log:\n%s\nwant:\n%s", strings.Join(got, "\n"), strings.Join(want, "\n"))
	}
	if got := git(t, apeDir, "status", "--porcelain"); got != "" {
		t.Errorf("ape repo not clean: %q", got)
	}
	if got := git(t, root, "status", "--porcelain"); got != "" {
		t.Errorf("code repo not clean: %q", got)
	}
}

func TestErrors(t *testing.T) {
	root := project(t)
	apeDir := filepath.Join(root, "ape")
	mustApe(t, root, "new", "fresh", "--kind", "feat")                                // 1: status new
	mustApe(t, root, "new", "boss", "--kind", "owner")                                // 2
	mustApe(t, root, "new", "wait", "--kind", "feat")                                 // 3
	mustApe(t, root, "set", "2", "pri=1", "status=ready")                             //
	mustApe(t, root, "set", "3", "pri=1", "area=x/", "status=blocked", "needs=owner") //
	mustApe(t, root, "new", "go", "--kind", "chore")                                  // 4
	mustApe(t, root, "set", "4", "pri=2", "area=y/", "status=ready", "needs=owner")   // ready with needs
	before := subjects(t, root)
	task1 := read(t, filepath.Join(apeDir, "tasks", "T1-fresh.md"))

	for _, c := range []struct {
		code int
		want string
		args []string
	}{
		{1, "unknown command \"frob\"", []string{"frob"}},

		{1, "want one slug, got 0 arguments; usage: ape new", []string{"new"}},
		{1, "want one slug, got 2 arguments", []string{"new", "a", "b"}},
		{1, "bad slug \"Bad_Slug\"", []string{"new", "Bad_Slug"}},
		{1, "bad slug \"a--b\"", []string{"new", "a--b"}},
		{1, "bad kind \"epic\"", []string{"new", "x", "--kind", "epic"}},
		{1, "want one slug, got 2 arguments", []string{"new", "x", "--bogus"}}, // not a flag of new: a positional

		{1, "want a task ID and at least one k=v", []string{"set"}},
		{1, "want a task ID and at least one k=v", []string{"set", "1"}},
		{1, "\"x\" isn't a task ID", []string{"set", "x", "pri=1"}},
		{1, "\"0\" isn't a task ID", []string{"set", "0", "pri=1"}},
		{1, "\"pri\" isn't k=v", []string{"set", "1", "pri"}},
		{1, "unknown key \"color\"", []string{"set", "1", "color=red"}},
		{1, "T1: bad pri \"9\"", []string{"set", "1", "pri=9"}},
		{1, "T1: bad status \"done\"", []string{"set", "1", "status=done"}},
		{1, "T1: bad kind \"epic\"", []string{"set", "1", "kind=epic"}},
		{1, "T1: bad needs \"bob\"", []string{"set", "1", "needs=bob"}},
		{3, "no T99 in " + filepath.Join(apeDir, "tasks"), []string{"set", "99", "pri=1"}},

		{1, "usage: ape start <id>", []string{"start"}},
		{1, "usage: ape start <id>", []string{"start", "1", "2"}},
		{1, "\"x\" isn't a task ID", []string{"start", "x"}},
		{3, "no T99", []string{"start", "99"}},
		{1, "T1 is new; triage it", []string{"start", "1"}},
		{1, "T2 is kind owner", []string{"start", "2"}},
		{1, "T3 needs owner; once that's resolved: ape set T3 status=ready needs=", []string{"start", "3"}},
		{1, "T4 needs owner; once that's resolved: ape set T4 status=ready needs=", []string{"start", "4"}},

		{1, "usage: ape pause <id>", []string{"pause"}},
		{1, "\"-1\" isn't a task ID", []string{"pause", "-1"}},
		{3, "no T99", []string{"pause", "99"}},
		{1, "T1 has no worktree; nothing to pause", []string{"pause", "1"}},

		{1, "want a task ID and a quoted summary, got 0 arguments", []string{"done"}},
		{1, "want a task ID and a quoted summary, got 1 arguments", []string{"done", "1"}},
		{1, "empty summary", []string{"done", "1", " \n"}},
		{1, "\"x\" isn't a task ID", []string{"done", "x", "y"}},
		{3, "no T99", []string{"done", "99", "y"}},
		{3, "no T99", []string{"done", "99", "-20% p95; tests green"}}, // a summary, not a flag

		{1, "want a task ID and a quoted reason, got 0 arguments", []string{"drop"}},
		{1, "want a task ID and a quoted reason, got 3 arguments", []string{"drop", "1", "a", "b"}},
		{1, "empty reason", []string{"drop", "1", ""}},
		{1, "want a task ID and a quoted reason, got 3 arguments", []string{"drop", "1", "x", "--bogus"}},
		{3, "no T99", []string{"drop", "99", "y"}},

		{1, "init takes no arguments", []string{"init", "x"}},
	} {
		wantErr(t, root, c.code, c.want, c.args...)
	}
	if got := subjects(t, root); !slices.Equal(got, before) {
		t.Errorf("failed commands committed: %q", got[:len(got)-len(before)])
	}
	if got := read(t, filepath.Join(apeDir, "tasks", "T1-fresh.md")); got != task1 {
		t.Errorf("a rejected set changed task 1:\n%s", got)
	}
	if got := git(t, apeDir, "status", "--porcelain"); got != "" {
		t.Errorf("ape repo not clean after errors: %q", got)
	}

	// Outside any ape dir: every command but init is "not found" with the init hint.
	outside, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	const hint = "ape: no ape/ found here or above; run 'ape init' in the repo root"
	for _, args := range [][]string{
		{}, {"new", "x"}, {"set", "1", "pri=1"}, {"start", "1"}, {"pause", "1"}, {"done", "1", "x"},
		{"drop", "1", "x"},
	} {
		if got := wantErr(t, outside, 3, "", args...); got != hint {
			t.Errorf("ape %q outside: %q, want %q", args, got, hint)
		}
	}
	// A code repo without ape/ is just as outside.
	bare := codeRepo(t)
	if got := wantErr(t, bare, 3, "", "start", "1"); got != hint {
		t.Errorf("ape start in a repo without ape/: %q", got)
	}
	// $APE_DIR pointing at a dir without _FRAMEWORK.md.
	if r := run(t, root, []string{"APE_DIR=" + outside}, apeBin); r.code != 3 || r.stderr != hint+"\n" {
		t.Errorf("bad $APE_DIR:\n%s", r)
	}
	// $APE_DIR from outside finds the project.
	if r := run(t, outside, []string{"APE_DIR=" + apeDir}, apeBin); r.code != 0 ||
		!strings.HasPrefix(r.stdout, "ape: 4 tasks") {
		t.Errorf("$APE_DIR from outside:\n%s", r)
	}

	// init outside a git repo, and in a linked worktree.
	wantErr(t, outside, 1, "not in a git work tree; run ape init in your code repo's root", "init")
	linked := filepath.Join(filepath.Dir(bare), "linked")
	git(t, bare, "worktree", "add", "-q", "-b", "side", linked)
	wantErr(t, linked, 1, linked+" is a linked worktree; run ape init in the main worktree "+bare, "init")
	if exists(filepath.Join(linked, "ape")) || exists(filepath.Join(bare, "ape")) {
		t.Error("a refused init created ape/")
	}
}

// --- Races ---

// rounds runs fn rounds times as subtests, each on a fresh repo, and logs each round's wall time (of the race
// itself, as fn reports it). Skipped under -short.
func rounds(t *testing.T, fn func(t *testing.T) time.Duration) {
	if testing.Short() {
		t.Skip("race test; skipped with -short")
	}
	t.Parallel()
	var times []string
	for i := range 5 {
		t.Run(strconv.Itoa(i), func(t *testing.T) {
			d := fn(t)
			times = append(times, d.Round(time.Millisecond).String())
		})
	}
	t.Logf("race wall times: %s", strings.Join(times, " "))
}

// parallel runs fn(0..n-1) at once, after all goroutines are ready, and returns the wall time.
func parallel(n int, fn func(i int)) time.Duration {
	var ready, done sync.WaitGroup
	gate := make(chan struct{})
	ready.Add(n)
	done.Add(n)
	for i := range n {
		go func() {
			defer done.Done()
			ready.Done()
			<-gate
			fn(i)
		}()
	}
	ready.Wait()
	start := time.Now()
	close(gate)
	done.Wait()
	return time.Since(start)
}

// count is how many of ss match re.
func count(ss []string, re string) int {
	n := 0
	rx := regexp.MustCompile(re)
	for _, s := range ss {
		if rx.MatchString(s) {
			n++
		}
	}
	return n
}

func TestRaceNew(t *testing.T) {
	rounds(t, func(t *testing.T) time.Duration {
		root := project(t)
		tasks := filepath.Join(root, "ape", "tasks")
		res := make([]result, 20)
		d := parallel(20, func(i int) { res[i] = ape(t, root, "new", fmt.Sprintf("c%d", i)) })
		var nums []int
		for i, r := range res {
			if r.code != 0 || r.stderr != "" {
				t.Errorf("new c%d:\n%s", i, r)
				continue
			}
			m := regexp.MustCompile(`^` + regexp.QuoteMeta(tasks) + `/T(\d+)-c` + strconv.Itoa(i) + `\.md\n$`).
				FindStringSubmatch(r.stdout)
			if m == nil {
				t.Errorf("new c%d printed %q", i, r.stdout)
				continue
			}
			n, _ := strconv.Atoi(m[1])
			nums = append(nums, n)
		}
		slices.Sort(nums)
		want := make([]int, 20)
		for i := range want {
			want[i] = i + 1
		}
		if !slices.Equal(nums, want) {
			t.Errorf("numbers = %v, want 1..20", nums)
		}
		files, _ := filepath.Glob(filepath.Join(tasks, "*.md"))
		if len(files) != 20 {
			t.Errorf("%d task files, want 20", len(files))
		}
		if n := count(subjects(t, root), `^new\(T\d+\): c\d+$`); n != 20 {
			t.Errorf("%d new commits, want 20", n)
		}
		if got := read(t, filepath.Join(root, "ape", ".next")); got != "21\n" {
			t.Errorf(".next = %q, want 21", got)
		}
		if got := git(t, filepath.Join(root, "ape"), "status", "--porcelain"); got != "" {
			t.Errorf("ape repo not clean: %q", got)
		}
		return d
	})
}

func TestRaceStart(t *testing.T) {
	rounds(t, func(t *testing.T) time.Duration {
		root := project(t)
		mustApe(t, root, "new", "one", "--kind", "feat")
		mustApe(t, root, "set", "1", "pri=1", "area=a/", "status=ready")
		res := make([]result, 5)
		d := parallel(5, func(i int) { res[i] = ape(t, root, "start", "1") })
		var codes []int
		for _, r := range res {
			codes = append(codes, r.code)
			if r.code == 2 && !strings.HasPrefix(r.stderr, "ape: taken: worktree ") {
				t.Errorf("loser's stderr: %q", r.stderr)
			}
		}
		slices.Sort(codes)
		if !slices.Equal(codes, []int{0, 2, 2, 2, 2}) {
			t.Errorf("exit codes %v, want one 0 and four 2:\n%v", codes, res)
		}
		if got := git(t, root, "worktree", "list", "--porcelain"); strings.Count(got, "worktree ") != 2 ||
			strings.Count(got, "branch refs/heads/ape/T1-one") != 1 {
			t.Errorf("worktrees:\n%s", got)
		}
		return d
	})
}

func TestRaceSet(t *testing.T) {
	rounds(t, func(t *testing.T) time.Duration {
		root := project(t)
		apeDir := filepath.Join(root, "ape")
		for i := range 20 {
			mustApe(t, root, "new", fmt.Sprintf("c%d", i+1))
		}
		before := len(subjects(t, root))
		res := make([]result, 20)
		d := parallel(20, func(i int) { res[i] = ape(t, root, "set", strconv.Itoa(i+1), "pri=1") })
		for i, r := range res {
			if r.code != 0 {
				t.Errorf("set %d:\n%s", i+1, r)
			}
		}
		subs := subjects(t, root)
		if n := len(subs) - before; n != 20 || count(subs, `^set\(T\d+\): pri=1$`) != 20 {
			t.Errorf("%d new commits, want 20 set commits:\n%s", n, strings.Join(subs, "\n"))
		}
		for i := range 20 {
			if c := read(t, filepath.Join(apeDir, "tasks", fmt.Sprintf("T%d-c%d.md", i+1, i+1))); !strings.Contains(c, "\npri: 1\n") {
				t.Errorf("task %d lacks pri 1:\n%s", i+1, c)
			}
			// Committed, not just on disk.
			if c := git(t, apeDir, "show", fmt.Sprintf("HEAD:tasks/T%d-c%d.md", i+1, i+1)); !strings.Contains(c, "\npri: 1\n") {
				t.Errorf("task %d at HEAD lacks pri 1:\n%s", i+1, c)
			}
		}
		if got := git(t, apeDir, "status", "--porcelain"); got != "" {
			t.Errorf("ape repo not clean: %q", got)
		}
		return d
	})
}

// TestEditedTask: new, edit by hand, set: no ape commit in between. set, done and drop commit the task's own edits
// first, alone; other dirty files in ape/ stay as they are.
func TestEditedTask(t *testing.T) {
	root := project(t)
	apeDir := filepath.Join(root, "ape")
	task := func(name string) string { return filepath.Join(apeDir, "tasks", name) }
	edit := func(name, s string) { write(t, task(name), read(t, task(name))+s) }
	mustApe(t, root, "new", "login", "--kind", "feat")
	mustApe(t, root, "new", "csv")
	edit("T1-login.md", "Users sign in with OAuth. done: e2e.\n")
	write(t, filepath.Join(apeDir, "PROJECT.md"), "dirty\n")

	mustApe(t, root, "set", "1", "pri=1", "area=src/", "status=ready")
	if got := subjects(t, root)[:3]; !slices.Equal(got,
		[]string{"set(T1): pri=1 area=src/ status=ready", "edit(T1): hand edits", "new(T2): csv"}) {
		t.Errorf("ape log = %q", got)
	}
	if got := git(t, apeDir, "show", "--format=", "HEAD"); strings.Contains(got, "OAuth") {
		t.Errorf("set's commit holds the body edit:\n%s", got)
	}
	if got := git(t, apeDir, "show", "HEAD:tasks/T1-login.md"); !strings.Contains(got, "pri: 1") ||
		!strings.HasSuffix(got, "OAuth. done: e2e.") {
		t.Errorf("task at HEAD:\n%s", got)
	}
	if got := git(t, apeDir, "status", "--porcelain"); got != "M PROJECT.md" {
		t.Errorf("ape status = %q, want PROJECT.md alone", got)
	}

	edit("T1-login.md", "## Progress\nhalf\n")
	edit("T2-csv.md", "dup\n")
	mustApe(t, root, "done", "1", "shipped")
	mustApe(t, root, "drop", "2", "dup of T1")
	if got := subjects(t, root)[:4]; !slices.Equal(got,
		[]string{"drop(T2): dup of T1", "edit(T2): hand edits", "done(T1): shipped", "edit(T1): hand edits"}) {
		t.Errorf("ape log = %q", got)
	}
	if got := git(t, apeDir, "show", "HEAD~3:tasks/T1-login.md"); !strings.HasSuffix(got, "## Progress\nhalf") {
		t.Errorf("edit commit holds:\n%s", got)
	}
	if got := git(t, apeDir, "status", "--porcelain"); got != "M PROJECT.md" {
		t.Errorf("ape status = %q, want PROJECT.md alone", got)
	}
}

// TestRaceMixed races ape's own writes against `ape commit` of hand edits, as _FRAMEWORK.md tells agents to commit
// them: of tracked files and of new ones.
func TestRaceMixed(t *testing.T) {
	rounds(t, func(t *testing.T) time.Duration {
		root := project(t)
		apeDir := filepath.Join(root, "ape")
		note := func(i int) string { return fmt.Sprintf("ref/note%d.md", i) }
		for i := range 10 {
			mustApe(t, root, "new", fmt.Sprintf("c%d", i+1))
		}
		for i := range 5 { // notes 0-4 tracked; 5-9 new
			write(t, filepath.Join(apeDir, note(i)), "draft\n")
		}
		git(t, apeDir, "add", "ref")
		git(t, apeDir, "commit", "-q", "-m", "notes")
		before := len(subjects(t, root))
		for i := range 10 { // written before the race; committed during it
			write(t, filepath.Join(apeDir, note(i)), fmt.Sprintf("note %d\n", i))
		}
		for i := range 5 { // tasks 1-5 edited by hand: their set commits the edit first
			p := filepath.Join(apeDir, fmt.Sprintf("tasks/T%d-c%d.md", i+1, i+1))
			write(t, p, read(t, p)+"body\n")
		}
		res := make([]result, 20)
		d := parallel(20, func(i int) {
			if i%2 == 0 {
				res[i] = ape(t, root, "set", strconv.Itoa(i/2+1), "pri=1")
			} else {
				n := i / 2
				res[i] = ape(t, root, "commit", "-m", fmt.Sprintf("hand %d", n), "ape/"+note(n))
			}
		})
		for i, r := range res {
			if r.code != 0 || r.stderr != "" {
				t.Errorf("process %d:\n%s", i, r)
			}
		}
		subs := subjects(t, root)
		if n := len(subs) - before; n != 25 || count(subs, `^set\(T\d+\): pri=1$`) != 10 ||
			count(subs, `^hand \d$`) != 10 || count(subs, `^edit\(T[1-5]\): hand edits$`) != 5 {
			t.Errorf("%d new commits, want 10 set + 10 hand + 5 edit:\n%s", n, strings.Join(subs, "\n"))
		}
		for i := range 10 {
			c := git(t, apeDir, "show", fmt.Sprintf("HEAD:tasks/T%d-c%d.md", i+1, i+1))
			if !strings.Contains(c, "\npri: 1\n") {
				t.Errorf("task %d at HEAD lacks pri 1:\n%s", i+1, c)
			}
			if edited := i < 5; strings.HasSuffix(c, "body") != edited {
				t.Errorf("task %d at HEAD: hand edit %v, want %v:\n%s", i+1, !edited, edited, c)
			}
			if i < 5 && slices.Index(subs, fmt.Sprintf("edit(T%d): hand edits", i+1)) <
				slices.Index(subs, fmt.Sprintf("set(T%d): pri=1", i+1)) {
				t.Errorf("task %d: set before edit:\n%s", i+1, strings.Join(subs, "\n"))
			}
			if c := git(t, apeDir, "show", "HEAD:"+note(i)); c != fmt.Sprintf("note %d", i) {
				t.Errorf("ref/note%d.md at HEAD = %q", i, c)
			}
		}
		if got := git(t, apeDir, "status", "--porcelain"); got != "" {
			t.Errorf("ape repo not clean: %q", got)
		}
		return d
	})
}

// TestCommitAfterIndexRewrite injects, deterministically, what a concurrent hand commit (`git commit -- <path>`) can
// do to the shared index: git's partial commit reads the index before it locks it (builtin/commit.c:
// repo_read_index, then repo_hold_locked_index) and writes that copy back, wiping whatever was staged in between. A
// wrapper git resets the shared index once, while ape builds its commit (at write-tree). ape must then still commit,
// or fail; never exit 0 with nothing committed.
func TestCommitAfterIndexRewrite(t *testing.T) {
	realGit, err := exec.LookPath("git")
	if err != nil {
		t.Fatal(err)
	}
	for _, args := range [][]string{{"set", "1", "pri=2"}, {"new", "two"}} {
		t.Run(args[0], func(t *testing.T) {
			root := project(t)
			apeDir := filepath.Join(root, "ape")
			mustApe(t, root, "new", "one")
			before := subjects(t, root)
			bin := t.TempDir()
			write(t, filepath.Join(bin, "git"), `#!/bin/sh
case " $* " in
*" write-tree "*) [ -e "$MARK" ] || { : >"$MARK"; env -u GIT_INDEX_FILE "$REAL_GIT" -C "$APE" reset -q; } ;;
esac
exec "$REAL_GIT" "$@"
`)
			if err := os.Chmod(filepath.Join(bin, "git"), 0o755); err != nil {
				t.Fatal(err)
			}
			r := run(t, root, []string{"PATH=" + bin + ":" + os.Getenv("PATH"), "REAL_GIT=" + realGit,
				"APE=" + apeDir, "MARK=" + filepath.Join(bin, "mark")}, apeBin, args...)
			if !exists(filepath.Join(bin, "mark")) {
				t.Fatal("the wrapper never ran; ape no longer builds its tree with write-tree, so this test needs rethinking")
			}
			after := subjects(t, root)
			status := git(t, apeDir, "status", "--porcelain")
			if r.code == 0 && (len(after) != len(before)+1 || status != "") {
				t.Errorf("ape %q exited 0, but committed %d commits and left the ape repo %q:\n%s",
					args, len(after)-len(before), status, r)
			}
		})
	}
}

// A heredoc is the task's body; /dev/null (every other test's stdin) is none (T33).
func TestNewBodyFromStdin(t *testing.T) {
	root := project(t)
	script := apeBin + " new login-timeout --title 'Login times out' kind=bug pri=2 'area=auth/' status=ready <<'EOF'\n" +
		"Repro: log in, wait 5 min.\nEOF\n"
	if r := run(t, root, nil, "bash", "-c", script); r.code != 0 || r.stderr != "" {
		t.Fatalf("ape new with a heredoc:\n%s", r)
	}
	want := "---\nkind: bug\npri: 2\nstatus: ready\narea: auth/\n---\n# Login times out\n\nRepro: log in, wait 5 min.\n"
	if got := read(t, filepath.Join(root, "ape", "tasks", "T1-login-timeout.md")); got != want {
		t.Errorf("task:\n%s", got)
	}
	if got := subjects(t, root)[0]; got != "new(T1): Login times out" {
		t.Errorf("commit %q", got)
	}
}
