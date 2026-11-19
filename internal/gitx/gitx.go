// Package gitx runs git for ape. Every call is `git -C <Dir> ...` with the environment's git.
package gitx

import (
	"bytes"
	"errors"
	"fmt"
	"math/rand/v2"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"time"
)

// Repo is a git working tree.
type Repo struct{ Dir string }

// Run runs git in r.Dir and returns trimmed stdout; the error carries git's stderr.
func (r Repo) Run(args ...string) (string, error) {
	out, err := r.run(args...)
	return strings.TrimSpace(out), err
}

// gitError is a failed git command; code is its exit status (-1 if it didn't run).
type gitError struct {
	args   []string
	code   int
	stderr string
}

func (e *gitError) Error() string {
	msg := e.stderr
	if msg == "" {
		msg = "exit status " + strconv.Itoa(e.code)
	}
	return fmt.Sprintf("git %s: %s", strings.Join(e.args, " "), msg)
}

// exitCode returns err's git exit status, or -1 if err isn't a git exit.
func exitCode(err error) int {
	var ge *gitError
	if errors.As(err, &ge) {
		return ge.code
	}
	return -1
}

// run runs git in r.Dir and returns untrimmed stdout.
func (r Repo) run(args ...string) (string, error) { return r.runEnv(nil, args...) }

// runEnv is run with extra environment variables.
func (r Repo) runEnv(env []string, args ...string) (string, error) {
	cmd := exec.Command("git", append([]string{"-C", r.Dir}, args...)...)
	cmd.Env = append(append(inherited(), "GIT_TERMINAL_PROMPT=0", "LC_ALL=C"), env...)
	var stdout, stderr bytes.Buffer
	cmd.Stdout, cmd.Stderr = &stdout, &stderr
	err := cmd.Run()
	if err == nil {
		return stdout.String(), nil
	}
	code := -1
	var ee *exec.ExitError
	if errors.As(err, &ee) {
		code = ee.ExitCode()
	}
	msg := strings.TrimSpace(stderr.String())
	if code == -1 && msg == "" {
		msg = err.Error()
	}
	return stdout.String(), &gitError{args: args, code: code, stderr: msg}
}

// repoVars point git at another repo than Dir; a git hook running ape sets them for its own repo.
var repoVars = []string{"GIT_DIR=", "GIT_WORK_TREE=", "GIT_INDEX_FILE=", "GIT_COMMON_DIR=", "GIT_OBJECT_DIRECTORY="}

// inherited is the process environment without repoVars.
func inherited() []string {
	var env []string
next:
	for _, kv := range os.Environ() {
		for _, v := range repoVars {
			if strings.HasPrefix(kv, v) {
				continue next
			}
		}
		env = append(env, kv)
	}
	return env
}

// Worktree is a linked or main worktree.
type Worktree struct {
	Path     string
	Branch   string // short name ("ape/T12-x"); "" if detached
	Head     string // sha of its checked-out commit; "" on an unborn branch
	Detached bool
	Prunable bool // its directory is gone (git worktree prune would remove it)
	Locked   bool // git worktree lock: git won't remove or prune it
}

// Worktrees lists the repo's worktrees (git worktree list --porcelain): the main one first, then the linked ones by
// path (git lists those in directory order, which differs by filesystem). A bare main repo isn't a worktree and is
// left out.
func (r Repo) Worktrees() ([]Worktree, error) {
	out, err := r.run("worktree", "list", "--porcelain")
	if err != nil {
		return nil, err
	}
	var wts []Worktree
	linked := 0 // index of the first linked worktree in wts
	for i, rec := range strings.Split(out, "\n\n") {
		var wt Worktree
		bare := false
		for _, line := range strings.Split(rec, "\n") {
			key, val, _ := strings.Cut(line, " ")
			switch key {
			case "worktree":
				wt.Path = val
			case "branch":
				wt.Branch = strings.TrimPrefix(val, "refs/heads/")
			case "HEAD":
				if strings.Trim(val, "0") != "" {
					wt.Head = val
				}
			case "detached":
				wt.Detached = true
			case "prunable":
				wt.Prunable = true
			case "locked":
				wt.Locked = true
			case "bare":
				bare = true
			}
		}
		if wt.Path != "" && !bare {
			wts = append(wts, wt)
			if i == 0 {
				linked = 1
			}
		}
	}
	slices.SortFunc(wts[linked:], func(a, b Worktree) int { return strings.Compare(a.Path, b.Path) })
	return wts, nil
}

// Branch is a local branch and its last commit time.
type Branch struct {
	Name string // short name
	Last time.Time
}

// Branches lists local branches under refs/heads/<prefix> in one call (git for-each-ref).
func (r Repo) Branches(prefix string) ([]Branch, error) {
	// Full refnames, filtered here: %(refname:short) can come out as "heads/x" when a tag x exists, and
	// for-each-ref's own patterns match only whole path components.
	out, err := r.run("for-each-ref", "--format=%(refname)%00%(committerdate:unix)", "refs/heads/")
	if err != nil {
		return nil, err
	}
	var bs []Branch
	for _, line := range strings.Split(out, "\n") {
		ref, ts, ok := strings.Cut(line, "\x00")
		name, isHead := strings.CutPrefix(ref, "refs/heads/")
		if !ok || !isHead || !strings.HasPrefix(name, prefix) {
			continue
		}
		t, err := unixTime(ts)
		if err != nil {
			return nil, fmt.Errorf("git for-each-ref: %s: %w", ref, err)
		}
		bs = append(bs, Branch{Name: name, Last: t})
	}
	return bs, nil
}

func unixTime(s string) (time.Time, error) {
	n, err := strconv.ParseInt(strings.TrimSpace(s), 10, 64)
	if err != nil {
		return time.Time{}, err
	}
	return time.Unix(n, 0), nil
}

// BranchExists reports whether the local branch name exists.
func (r Repo) BranchExists(name string) (bool, error) {
	_, err := r.run("rev-parse", "-q", "--verify", "refs/heads/"+name)
	if exitCode(err) == 1 {
		return false, nil
	}
	return err == nil, err
}

// DeleteBranchAt deletes the local branch name if it still points at sha (else it fails and keeps it): a
// compare-and-swap, so a commit made to it since sha was read is never lost.
func (r Repo) DeleteBranchAt(name, sha string) error {
	_, err := r.run("update-ref", "-d", "refs/heads/"+name, sha)
	return err
}

// Dirty lists what `git status` reports in the working tree dir, one "XY path" entry each (porcelain v1; a
// rename's or copy's original path is left out); none means clean. Untracked files and submodules always count,
// whatever the repo's config (status.showUntrackedFiles, diff.ignoreSubmodules) says; ignored files never do.
func Dirty(dir string) ([]string, error) {
	out, err := Repo{Dir: dir}.run("status", "--porcelain=v1", "-z", "--untracked-files=normal",
		"--ignore-submodules=none")
	if err != nil {
		return nil, err
	}
	var es []string
	fields := strings.Split(strings.TrimSuffix(out, "\x00"), "\x00")
	for i := 0; i < len(fields); i++ {
		e := fields[i]
		if e == "" {
			continue
		}
		es = append(es, e)
		if strings.ContainsAny(e[:2], "RC") { // -z: the original path follows as its own field
			i++
		}
	}
	return es, nil
}

// Head returns the full sha of HEAD.
func (r Repo) Head() (string, error) { return r.Run("rev-parse", "--verify", "HEAD^{commit}") }

// CurrentBranch returns HEAD's branch short name, "" if detached.
func (r Repo) CurrentBranch() (string, error) {
	out, err := r.Run("symbolic-ref", "-q", "--short", "HEAD")
	if exitCode(err) == 1 {
		return "", nil
	}
	return out, err
}

// IsAncestor reports whether commit a is an ancestor of (or equal to) b.
func (r Repo) IsAncestor(a, b string) (bool, error) {
	_, err := r.run("merge-base", "--is-ancestor", a, b)
	switch {
	case err == nil:
		return true, nil
	case exitCode(err) == 1:
		return false, nil
	}
	return false, err
}

// Merged reports whether commit a is merged into b: an ancestor of it, or merging it into b changes nothing
// (`git merge-tree --write-tree b a` gives b's tree), as after a squash or rebase merge. A conflict isn't merged.
func (r Repo) Merged(a, b string) (bool, error) {
	if ok, err := r.IsAncestor(a, b); ok || err != nil {
		return ok, err
	}
	merged, err := r.Run("merge-tree", "--write-tree", "--no-messages", b, a)
	if exitCode(err) == 1 {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	tree, err := r.Run("rev-parse", "--verify", b+"^{tree}")
	return err == nil && strings.SplitN(merged, "\n", 2)[0] == tree, err
}

// Conflicts returns the paths that merging commits a and b would leave conflicted (`git merge-tree --write-tree`,
// nothing written to any ref or worktree), sorted; none if the merge is clean.
func (r Repo) Conflicts(a, b string) ([]string, error) {
	out, err := r.run("merge-tree", "--write-tree", "--name-only", "-z", "--no-messages", a, b)
	if err == nil {
		return nil, nil
	}
	if exitCode(err) != 1 {
		return nil, err
	}
	fields := strings.Split(out, "\x00") // the merged tree, then each conflicted path once
	var paths []string
	for _, p := range fields[1:] {
		if p != "" {
			paths = append(paths, p)
		}
	}
	slices.Sort(paths)
	return paths, nil
}

// Generated reports whether every one of paths has the attribute linguist-generated set (.gitattributes in r's
// working tree): files a tool regenerates, so a conflict in them is resolved by regenerating, not by hand.
func (r Repo) Generated(paths []string) (bool, error) {
	if len(paths) == 0 {
		return false, nil
	}
	out, err := r.run(append([]string{"check-attr", "-z", "linguist-generated", "--"}, paths...)...)
	if err != nil {
		return false, err
	}
	f := strings.Split(out, "\x00") // path, attribute, value, repeated
	for i := 2; i < len(f); i += 3 {
		if f[i] != "set" && f[i] != "true" {
			return false, nil
		}
	}
	return true, nil
}

// AheadOf reports whether branch has commits not reachable from base.
func (r Repo) AheadOf(branch, base string) (bool, error) {
	out, err := r.Run("rev-list", "-1", base+".."+branch, "--")
	return out != "", err
}

// LastOwnCommit returns the time of branch's newest commit not reachable from base; zero if there is none.
func (r Repo) LastOwnCommit(branch, base string) (time.Time, error) {
	out, err := r.Run("log", "-1", "--format=%ct", base+".."+branch, "--")
	if err != nil || out == "" {
		return time.Time{}, err
	}
	return unixTime(out)
}

// Commit commits exactly paths (created, modified or deleted), as CommitPaths does.
func (r Repo) Commit(msg string, paths ...string) error {
	_, err := r.CommitPaths(msg, paths...)
	return err
}

// commitBudget is how long CommitPaths keeps retrying a lost race for HEAD, and how long it then tries to refresh
// the shared index.
const commitBudget = 15 * time.Second

// CommitPaths commits exactly paths (relative to r.Dir; created, modified or deleted) with msg on top of HEAD and
// returns the new commit's sha. Nothing to commit in paths is not an error: it makes no commit and returns "".
//
// The tree is built in a private index (HEAD's tree plus paths' working-tree state), never in the shared one: a
// concurrent `git commit -- <path>` reads the shared index before it locks it and writes that stale copy back, which
// would wipe whatever was staged in between. HEAD moves by compare-and-swap; if another commit moved it first, the
// whole sequence reruns on the new HEAD, 50-250 ms later with jitter, for up to ~15 s. Hooks don't run
// (commit-tree): ape's commits are machine-made. Afterwards the shared index is refreshed for paths only (best
// effort: a failure there leaves `git status` showing paths as changed, not a wrong commit).
func (r Repo) CommitPaths(msg string, paths ...string) (string, error) {
	if len(paths) == 0 {
		return "", errors.New("gitx: Commit needs at least one path")
	}
	tmp, err := os.MkdirTemp("", "gitx-index-")
	if err != nil {
		return "", err
	}
	defer os.RemoveAll(tmp)
	index := filepath.Join(tmp, "index")

	deadline := time.Now().Add(commitBudget)
	var sha string
	for {
		sha, err = r.commitOnce(index, msg, paths)
		if err == nil || !contended(err) || time.Now().After(deadline) {
			break
		}
		time.Sleep(jitter())
	}
	if err != nil || sha == "" {
		return "", err
	}
	r.refreshIndex(paths)
	return sha, nil
}

func jitter() time.Duration { return time.Duration(50+rand.IntN(200)) * time.Millisecond }

// contended reports whether err is another git process holding a lock ape needs (index.lock, HEAD's or its
// branch's ref lock) or having moved HEAD since ape read it.
func contended(err error) bool {
	msg := err.Error()
	return strings.Contains(msg, "index.lock") || strings.Contains(msg, "cannot lock ref")
}

// commitOnce builds the tree for paths on top of the current HEAD in the private index file index, and commits it
// if it differs from HEAD's tree. It returns the new commit, or "" if there was nothing to commit.
func (r Repo) commitOnce(index, msg string, paths []string) (string, error) {
	os.Remove(index) // a fresh index each try: no entry survives from an older HEAD
	priv := []string{"GIT_INDEX_FILE=" + index}
	head, err := r.Run("rev-parse", "-q", "--verify", "HEAD^{commit}")
	unborn := exitCode(err) == 1
	if err != nil && !unborn {
		return "", err
	}
	var headTree string
	if unborn { // compare with the empty tree: nothing to commit makes no empty first commit
		if headTree, err = r.Run("hash-object", "-t", "tree", os.DevNull); err != nil {
			return "", err
		}
	} else {
		if headTree, err = r.Run("rev-parse", head+"^{tree}"); err != nil {
			return "", err
		}
		if _, err := r.runEnv(priv, "read-tree", head); err != nil {
			return "", err
		}
	}
	// update-index, unlike `git add -A`, doesn't fail on a path that is neither on disk nor in the index.
	if _, err := r.runEnv(priv, append([]string{"update-index", "--add", "--remove", "--"}, paths...)...); err != nil {
		return "", err
	}
	tree, err := r.runEnv(priv, "write-tree")
	if tree = strings.TrimSpace(tree); err != nil || tree == headTree {
		return "", err
	}
	args := []string{"commit-tree", tree, "-m", msg}
	if !unborn {
		args = append(args, "-p", head)
	}
	sha, err := r.Run(args...)
	if err != nil {
		return "", err
	}
	// The old value makes this a compare-and-swap ("" = HEAD's branch must not exist yet); a lost race fails
	// with "cannot lock ref", which contended retries.
	subject, _, _ := strings.Cut(msg, "\n")
	if _, err := r.run("update-ref", "-m", "commit: "+subject, "HEAD", sha, head); err != nil {
		return "", err
	}
	return sha, nil
}

// refreshIndex stages paths' working-tree state in the shared index, so that `git status` shows them as committed.
// Best effort: it retries a held index.lock until the budget runs out, then gives up silently.
func (r Repo) refreshIndex(paths []string) {
	deadline := time.Now().Add(commitBudget)
	for {
		_, err := r.run(append([]string{"update-index", "--add", "--remove", "--"}, paths...)...)
		if err == nil || !contended(err) || time.Now().After(deadline) {
			return
		}
		time.Sleep(jitter())
	}
}

// LogEntry is one commit.
type LogEntry struct {
	Time    time.Time
	Subject string
}

// Log returns commits since t on HEAD, newest first. A repo without commits has none.
func (r Repo) Log(since time.Time) ([]LogEntry, error) {
	if ok, err := r.hasHead(); !ok {
		return nil, err
	}
	args := []string{"log", "--format=%ct%x00%s"}
	if s := since.Unix(); s > 0 { // git can't parse an earlier one (time.Time{} included): all of history
		args = append(args, "--since=@"+strconv.FormatInt(s, 10))
	}
	out, err := r.run(append(args, "HEAD", "--")...)
	if err != nil {
		return nil, err
	}
	var es []LogEntry
	for _, line := range strings.Split(out, "\n") {
		ts, subject, ok := strings.Cut(line, "\x00")
		if !ok {
			continue
		}
		t, err := unixTime(ts)
		if err != nil {
			return nil, fmt.Errorf("git log: %q: %w", line, err)
		}
		es = append(es, LogEntry{Time: t, Subject: subject})
	}
	return es, nil
}

// hasHead reports whether HEAD names a commit (false in a repo without commits).
func (r Repo) hasHead() (bool, error) {
	_, err := r.run("rev-parse", "-q", "--verify", "HEAD^{commit}")
	if exitCode(err) == 1 {
		return false, nil
	}
	return err == nil, err
}

// PathsEverTouched lists every path under dir ever present in the history of any ref (git log --all
// --name-only), each once, relative to the top of the tree.
func (r Repo) PathsEverTouched(dir string) ([]string, error) {
	out, err := r.run("log", "--all", "--name-only", "-z", "--format=", "--no-renames", "--", ":(literal)"+dir)
	if err != nil {
		return nil, err
	}
	seen := map[string]bool{}
	var ps []string
	for _, p := range strings.Split(out, "\x00") {
		p = strings.Trim(p, "\n")
		if p != "" && !seen[p] {
			seen[p] = true
			ps = append(ps, p)
		}
	}
	return ps, nil
}
