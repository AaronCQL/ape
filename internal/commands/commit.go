package commands

import (
	"flag"
	"os"
	"path/filepath"
	"strings"
)

const commitUsage = `ape commit -m "<msg>" <paths…> (files or directories; relative to the cwd, else to ape/)`

// Commit runs `ape commit` with its arguments (after the subcommand name): it commits exactly the paths given (a
// directory: the files under it), as ape's own writes do, so that hand edits in ape/ never go through the shared
// index. See SPEC.md.
func Commit(env Env, args []string) error {
	fset := flag.NewFlagSet("commit", flag.ContinueOnError)
	msg := fset.String("m", "", "")
	pos, err := parseArgs(fset, args, commitUsage)
	if err != nil {
		return err
	}
	if strings.TrimSpace(*msg) == "" {
		return usageErr(commitUsage, "want a message (-m)")
	}
	if len(pos) == 0 {
		return usageErr(commitUsage, "want the paths to commit")
	}
	paths := make([]string, len(pos))
	for i, p := range pos {
		if paths[i], err = apePath(env, p); err != nil {
			return err
		}
	}
	return withLock(env, func() error {
		var files []string
		for _, p := range paths {
			under, err := filesUnder(env, p)
			if err != nil {
				return err
			}
			files = append(files, under...)
		}
		sha := ""
		if len(files) > 0 {
			if sha, err = apeRepo(env).CommitPaths(*msg, files...); err != nil {
				return err
			}
		}
		if sha == "" {
			printf(env, "nothing to commit\n")
			return nil
		}
		if short, err := apeRepo(env).Run("rev-parse", "--short", sha); err == nil {
			sha = short
		}
		printf(env, "%s\n", sha)
		return nil
	})
}

// apePath is p (absolute, or relative to the cwd, or else to the ape dir) relative to the ape dir. It must name a
// file, directory, symlink or deleted path inside the ape dir, not the ape dir itself (it would sweep up other agents'
// edits), outside its .git. The ape dir may be reached through a symlink
// (a worktree's ape/); p itself may no longer exist. A relative p that isn't an existing path inside the ape dir
// from the cwd is the ape dir's if that one exists or is in its HEAD: agents in a worktree name tasks/…, and from
// the repo root DECISIONS.md is ape/'s even when the code repo has one too (T16).
func apePath(env Env, p string) (string, error) {
	abs := p
	if !filepath.IsAbs(p) {
		abs = filepath.Join(env.Cwd, p)
		if _, in := apeRel(env, abs); !in || !lexists(abs) {
			if alt := filepath.Join(env.ApeDir, p); lexists(alt) || committed(env, alt) {
				abs = alt
			}
		}
	}
	rel, in := apeRel(env, abs)
	if !in {
		return "", fail(ExitUsage, "%s is outside %s; ape commit commits files in ape/ only", p, env.ApeDir)
	}
	if rel == ".git" || strings.HasPrefix(rel, ".git/") {
		return "", fail(ExitUsage, "%s is inside ape/.git; name files in ape/", p)
	}
	if rel == "." {
		return "", fail(ExitUsage, "%s is ape/ itself; name the files or directories in it to commit", p)
	}
	return rel, nil
}

// filesUnder is rel itself, or if it's a directory on disk or in the ape repo's HEAD, the files under it that may
// differ from HEAD: in HEAD, in the index, or untracked and not ignored. Unchanged ones commit nothing.
func filesUnder(env Env, rel string) ([]string, error) {
	repo := apeRepo(env)
	fi, err := os.Lstat(filepath.Join(env.ApeDir, rel))
	tree := inHead(env, rel, "tree")
	if !tree && (err != nil || !fi.IsDir()) {
		return []string{rel}, nil
	}
	lists := [][]string{{"ls-files", "-z", "--cached", "--others", "--exclude-standard", "--", rel}}
	if tree {
		lists = append(lists, []string{"ls-tree", "-r", "-z", "--name-only", "HEAD", "--", rel})
	}
	seen := map[string]bool{}
	var files []string
	for _, args := range lists {
		out, err := repo.Run(append([]string{"--literal-pathspecs"}, args...)...)
		if err != nil {
			return nil, err
		}
		for f := range strings.SplitSeq(out, "\x00") {
			if f != "" && !seen[f] {
				seen[f] = true
				files = append(files, f)
			}
		}
	}
	return files, nil
}

// apeRel is abs relative to the ape dir, and whether it's inside it. Symlinks are resolved in abs's directory, not
// abs itself: a symlink in ape/ is committed as a symlink.
func apeRel(env Env, abs string) (string, bool) {
	abs = filepath.Join(resolveExisting(filepath.Dir(abs)), filepath.Base(abs))
	rel, err := filepath.Rel(env.ApeDir, abs)
	return rel, err == nil && rel != ".." && !strings.HasPrefix(rel, "../")
}

// committed reports whether abs is a file, symlink or directory in the ape repo's HEAD: a deleted task is still
// named.
func committed(env Env, abs string) bool {
	rel, in := apeRel(env, abs)
	return in && rel != "." && (inHead(env, rel, "blob") || inHead(env, rel, "tree"))
}

// inHead reports whether the ape repo's HEAD has an object of type typ (blob, tree) at rel.
func inHead(env Env, rel, typ string) bool {
	got, err := apeRepo(env).Run("cat-file", "-t", "HEAD:"+filepath.ToSlash(rel))
	return err == nil && got == typ
}

func lexists(path string) bool {
	_, err := os.Lstat(path)
	return err == nil
}

// resolveExisting resolves symlinks in dir's longest existing prefix and appends the rest unchanged.
func resolveExisting(dir string) string {
	rest := ""
	for {
		if r, err := filepath.EvalSymlinks(dir); err == nil {
			return filepath.Join(r, rest)
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return filepath.Join(dir, rest)
		}
		rest = filepath.Join(filepath.Base(dir), rest)
		dir = parent
	}
}
