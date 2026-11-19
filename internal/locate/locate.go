// Package locate finds the ape dir and the code repo's main root (SPEC.md "Finding the ape dir").
package locate

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"github.com/AaronCQL/ape/internal/gitx"
)

// ErrNotFound means no ape dir was found from cwd upwards (and $APE_DIR is unset).
var ErrNotFound = errors.New("no ape/ found")

// Find returns the resolved (symlink-free, absolute) ape dir for cwd and the code repo's main root (SPEC.md
// "Finding the ape dir"). The main root comes from git, not from where the ape dir resolves to: `ape` may be a
// symlink to a directory outside the repo.
func Find(cwd string) (apeDir, mainRoot string, err error) {
	cwd, err = filepath.Abs(cwd)
	if err != nil {
		return "", "", err
	}
	if env := os.Getenv("APE_DIR"); env != "" {
		d := env
		if !filepath.IsAbs(d) {
			d = filepath.Join(cwd, d)
		}
		if !exists(filepath.Join(d, "_FRAMEWORK.md")) {
			return "", "", fmt.Errorf("$APE_DIR=%s has no _FRAMEWORK.md", env)
		}
		if apeDir, err = filepath.EvalSymlinks(d); err != nil {
			return "", "", err
		}
		return apeDir, mainRootOf(cwd, filepath.Dir(apeDir)), nil
	}
	// Walk the path as given (not resolved first), so a worktree's `ape` symlink is found by name.
	for d := cwd; ; d = filepath.Dir(d) {
		// Inside ape/: git there is the ape repo's, so ask from its parent (still the path as given).
		if filepath.Base(d) == "ape" && exists(filepath.Join(d, "_FRAMEWORK.md")) &&
			(exists(filepath.Join(d, "tasks")) || exists(filepath.Join(d, ".git"))) {
			apeDir, err = filepath.EvalSymlinks(d)
			return apeDir, mainRootOf(filepath.Dir(d), filepath.Dir(d)), err
		}
		if a := filepath.Join(d, "ape"); exists(filepath.Join(a, "_FRAMEWORK.md")) {
			apeDir, err = filepath.EvalSymlinks(a)
			return apeDir, mainRootOf(d, d), err
		}
		if filepath.Dir(d) == d {
			return "", "", ErrNotFound
		}
	}
}

// mainRootOf returns the main worktree of the code repo holding dir, resolved: the parent of
// `git rev-parse --path-format=absolute --git-common-dir` when that ends in /.git. Outside a repo (or with a
// bare or separate git dir) it's fallback, resolved.
func mainRootOf(dir, fallback string) string {
	common, err := gitx.Repo{Dir: dir}.Run("rev-parse", "--path-format=absolute", "--git-common-dir")
	root := fallback
	if err == nil && filepath.Base(common) == ".git" {
		root = filepath.Dir(common)
	}
	if r, err := filepath.EvalSymlinks(root); err == nil {
		return r
	}
	return root
}

// ApeDir returns Find's ape dir.
func ApeDir(cwd string) (string, error) {
	apeDir, _, err := Find(cwd)
	return apeDir, err
}

func exists(p string) bool {
	_, err := os.Stat(p)
	return err == nil
}

// WorktreePath returns <dirname(mainRoot)>/<basename(mainRoot)>-wt/<name>.
func WorktreePath(mainRoot, name string) string {
	return filepath.Join(filepath.Dir(mainRoot), filepath.Base(mainRoot)+"-wt", name)
}
