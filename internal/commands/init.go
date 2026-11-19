package commands

import (
	"bytes"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	"github.com/AaronCQL/ape/framework"
	"github.com/AaronCQL/ape/internal/gitx"
)

// Init runs `ape init` with its arguments (after the subcommand name). See SPEC.md.
func Init(env Env, args []string) error {
	if len(args) > 0 {
		return usageErr("ape init", "init takes no arguments")
	}
	root, err := initRoot(env.Cwd)
	if err != nil {
		return err
	}
	env.MainRoot, env.ApeDir = root, filepath.Join(root, "ape")
	changed, fresh := false, false
	note := func(path, what string) {
		printf(env, "%s: %s\n", path, what)
		changed = true
	}

	switch _, err := os.Stat(filepath.Join(env.ApeDir, ".git")); {
	case errors.Is(err, fs.ErrNotExist):
		if _, err := os.Stat(env.ApeDir); err == nil {
			return fail(ExitUsage, "%s exists but isn't a git repo; move it aside, or git init it, and rerun", env.ApeDir)
		}
		if err := os.Mkdir(env.ApeDir, 0o755); err != nil {
			return err
		}
		if _, err := apeRepo(env).Run("init", "-q"); err != nil {
			return err
		}
		note(env.ApeDir, "created (git repo)")
	case err != nil:
		return err
	}

	err = withLock(env, func() error {
		ape := apeRepo(env)
		// No commit yet (new, or a first init whose commit failed): scaffold what's missing and commit it all.
		if _, err := ape.Head(); err != nil {
			files := []struct {
				name string
				data []byte
			}{
				{"PROJECT.md", framework.ProjectMD},
				{"DECISIONS.md", framework.DecisionsMD},
				{"tasks/.keep", nil},
				{"tasks/T1-setup-ape.md", framework.SetupTask},
				{".next", []byte("2\n")},
			}
			paths := []string{"_FRAMEWORK.md"}
			if ok, err := writeFramework(env); err != nil {
				return err
			} else if ok {
				note(filepath.Join(env.ApeDir, "_FRAMEWORK.md"), "created")
			}
			for _, f := range files {
				p := filepath.Join(env.ApeDir, f.name)
				paths = append(paths, f.name)
				if _, err := os.Stat(p); err == nil {
					continue
				}
				if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
					return err
				}
				if err := os.WriteFile(p, f.data, 0o644); err != nil {
					return err
				}
				note(p, "created")
			}
			if err := ape.Commit("init: set up ape/", paths...); err != nil {
				return err
			}
			note(env.ApeDir, "committed init")
			fresh = true
			return nil
		}
		ok, err := writeFramework(env)
		if err != nil || !ok {
			return err
		}
		note(filepath.Join(env.ApeDir, "_FRAMEWORK.md"), "updated to "+framework.Version)
		return ape.Commit("framework: "+framework.Version, "_FRAMEWORK.md")
	})
	if err != nil {
		return err
	}

	// The common dir's info/exclude covers every worktree with no commit (T11): a worktree started before a
	// .gitignore line is committed would otherwise not ignore its ape link. initRoot made root/.git the common dir.
	exclude := filepath.Join(root, ".git", "info", "exclude")
	old, err := readIfExists(exclude)
	if err != nil {
		return err
	}
	if !hasLine(old, "/ape") {
		if len(old) > 0 && !bytes.HasSuffix(old, []byte("\n")) {
			old = append(old, '\n')
		}
		if err := os.MkdirAll(filepath.Dir(exclude), 0o755); err != nil {
			return err
		}
		if err := os.WriteFile(exclude, append(old, "/ape\n"...), 0o644); err != nil {
			return err
		}
		note(exclude, "added /ape")
	}

	// AGENTS.md always; CLAUDE.md files only if present: Claude Code reads them instead of AGENTS.md.
	pointed := []string{"AGENTS.md"}
	for _, name := range []string{"CLAUDE.md", ".claude/CLAUDE.md"} {
		if _, err := os.Stat(filepath.Join(root, name)); err == nil && !readsAgents(root, name) {
			pointed = append(pointed, name)
		}
	}
	for _, name := range pointed {
		p := filepath.Join(root, name)
		old, err := readIfExists(p)
		if err != nil {
			return err
		}
		if updated := withPointer(old); !bytes.Equal(updated, old) {
			if err := os.WriteFile(p, updated, 0o644); err != nil {
				return err
			}
			note(p, "points at ape/")
		}
	}

	if !changed {
		printf(env, "up to date\n")
	}
	out, _ := codeRepo(env).Run(append([]string{"status", "--porcelain", "--"}, pointed...)...)
	var dirty []string
	for l := range strings.Lines(out) { // "XY path"; our paths have no spaces
		if f := strings.Fields(l); len(f) > 1 {
			dirty = append(dirty, f[len(f)-1])
		}
	}
	if n := len(dirty); n > 0 {
		list := dirty[n-1]
		if n > 1 {
			list = strings.Join(dirty[:n-1], ", ") + " and " + list
		}
		printf(env, "commit %s in the code repo: worktrees branch from HEAD\n", list)
	}
	if fresh {
		printf(env, "next: T1 (ape/tasks/T1-setup-ape.md): move what's only yours into ape/, shared rules stay; read it before starting\n")
	}
	return nil
}

// readsAgents reports whether root/name already brings in AGENTS.md (a link to it, or an `@AGENTS.md` import line),
// so the pointer there would be read twice.
func readsAgents(root, name string) bool {
	p := filepath.Join(root, name)
	if target, err := filepath.EvalSymlinks(p); err == nil {
		if agents, err := filepath.EvalSymlinks(filepath.Join(root, "AGENTS.md")); err == nil && target == agents {
			return true
		}
	}
	data, err := os.ReadFile(p)
	if err != nil {
		return false
	}
	rel, _ := filepath.Rel(filepath.Dir(p), filepath.Join(root, "AGENTS.md"))
	for l := range strings.Lines(string(data)) {
		if f := strings.TrimSpace(l); f == "@"+rel || f == "@./"+rel {
			return true
		}
	}
	return false
}

// initRoot is the code repo's main root, which must be cwd's worktree (SPEC.md "init").
func initRoot(cwd string) (string, error) {
	out, err := gitx.Repo{Dir: cwd}.Run("rev-parse", "--path-format=absolute", "--git-common-dir", "--show-toplevel")
	if err != nil {
		return "", fail(ExitUsage, "not in a git work tree; run ape init in your code repo's root")
	}
	common, top, _ := strings.Cut(out, "\n")
	if filepath.Base(common) != ".git" || top == "" {
		return "", fail(ExitUsage, "%s isn't a normal repo's .git; run ape init in a repo with a main worktree", common)
	}
	root := filepath.Dir(common)
	if !samePath(root, top) {
		return "", fail(ExitUsage, "%s is a linked worktree; run ape init in the main worktree %s", top, root)
	}
	return root, nil
}

// writeFramework writes _FRAMEWORK.md (read-only) if it differs from this version's; ok reports a write.
func writeFramework(env Env) (ok bool, err error) {
	p := filepath.Join(env.ApeDir, "_FRAMEWORK.md")
	want := framework.FrameworkMD()
	old, err := readIfExists(p)
	if err != nil || bytes.Equal(old, want) && old != nil {
		return false, err
	}
	if old != nil {
		if err := os.Chmod(p, 0o644); err != nil {
			return false, err
		}
	}
	if err := os.WriteFile(p, want, 0o444); err != nil {
		return false, err
	}
	return true, os.Chmod(p, 0o444)
}

// readIfExists reads p; a missing file is nil, an empty one non-nil.
func readIfExists(p string) ([]byte, error) {
	data, err := os.ReadFile(p)
	if errors.Is(err, fs.ErrNotExist) {
		return nil, nil
	}
	if data == nil && err == nil {
		data = []byte{}
	}
	return data, err
}

func hasLine(data []byte, line string) bool {
	for l := range strings.SplitSeq(string(data), "\n") {
		if strings.TrimSpace(l) == line {
			return true
		}
	}
	return false
}

// withPointer returns an instructions file's content starting with the ape pointer block: an existing block (from
// "<!-- ape -->" to "<!-- /ape -->", perhaps an older version's, anywhere) is replaced, else the block is prepended.
func withPointer(old []byte) []byte {
	const open, close = "<!-- ape -->\n", "<!-- /ape -->"
	rest := old
	if i := bytes.Index(old, []byte(open)); i >= 0 && (i == 0 || old[i-1] == '\n') {
		if j := bytes.Index(old[i:], []byte(close)); j >= 0 {
			end := i + j + len(close)
			for k := 0; k < 2 && end < len(old) && old[end] == '\n'; k++ { // its newline and one blank line
				end++
			}
			rest = append(old[:i:i], old[end:]...)
		}
	}
	if len(rest) == 0 {
		return framework.AgentsPointer
	}
	return append(append(append([]byte{}, framework.AgentsPointer...), '\n'), rest...)
}
