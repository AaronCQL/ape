package commands

import (
	"errors"
	"flag"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"

	"github.com/AaronCQL/ape/internal/fsx"
	"github.com/AaronCQL/ape/internal/task"
)

const newUsage = `ape new <slug> [--kind k] [--from who@sha] [--title "…"]`

// New runs `ape new` with its arguments (after the subcommand name). See SPEC.md.
func New(env Env, args []string) error {
	fset := flag.NewFlagSet("new", flag.ContinueOnError)
	kind := fset.String("kind", "", "")
	from := fset.String("from", "", "")
	title := fset.String("title", "", "")
	pos, err := parseArgs(fset, args, newUsage)
	if err != nil {
		return err
	}
	if len(pos) != 1 {
		return usageErr(newUsage, "want one slug, got %d arguments", len(pos))
	}
	slug := pos[0]
	if !task.ValidSlug(slug) {
		return fail(ExitUsage, "bad slug %q; use lowercase letters and digits, words joined by single dashes", slug)
	}
	if *kind != "" && !slices.Contains(task.Kinds, *kind) {
		return fail(ExitUsage, "bad kind %q; kind is one of %s", *kind, strings.Join(task.Kinds, " "))
	}
	t := oneLine(*title)
	if t == "" {
		t = strings.ReplaceAll(slug, "-", " ")
	}
	var b strings.Builder
	b.WriteString("---\n")
	if *kind != "" {
		b.WriteString("kind: " + *kind + "\n")
	}
	b.WriteString("status: new\n")
	if f := oneLine(*from); f != "" {
		b.WriteString("from: " + f + "\n")
	}
	b.WriteString("---\n# " + t + "\n\n")

	return withLock(env, func() error {
		n, err := nextNumber(env)
		if err != nil {
			return err
		}
		path := filepath.Join(tasksDir(env), task.FileName(n, slug))
		if err := os.MkdirAll(tasksDir(env), 0o755); err != nil {
			return err
		}
		f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o644)
		if err != nil {
			return err
		}
		_, err = f.WriteString(b.String())
		if cerr := f.Close(); err == nil {
			err = cerr
		}
		if err != nil {
			return err
		}
		if err := fsx.WriteAtomic(nextPath(env), []byte(strconv.Itoa(n+1)+"\n"), 0o644); err != nil {
			return err
		}
		if err := apeRepo(env).Commit(subject("new", n, t), relPath(env, path), ".next"); err != nil {
			return err
		}
		printf(env, "%s\n", path)
		return nil
	})
}

func nextPath(env Env) string { return filepath.Join(env.ApeDir, ".next") }

// nextNumber is the number for a new task: .next, or without it 1 + the highest number in tasks/ or ever in the
// ape repo's history. Never below 1 + the highest in tasks/, so a stale .next can't make a duplicate.
func nextNumber(env Env) (int, error) {
	entries, err := os.ReadDir(tasksDir(env))
	if err != nil && !errors.Is(err, fs.ErrNotExist) {
		return 0, err
	}
	top := 0
	for _, e := range entries {
		top = max(top, leadingNum(e.Name()))
	}
	data, err := os.ReadFile(nextPath(env))
	switch {
	case err == nil:
		v, err := strconv.Atoi(strings.TrimSpace(string(data)))
		if err != nil || v < 1 {
			return 0, fail(ExitUsage, "%s holds %q, not a number; delete it and ape recomputes it",
				nextPath(env), strings.TrimSpace(string(data)))
		}
		return max(v, top+1), nil
	case !errors.Is(err, fs.ErrNotExist):
		return 0, err
	}
	paths, err := apeRepo(env).PathsEverTouched("tasks")
	if err != nil {
		return 0, err
	}
	for _, p := range paths {
		top = max(top, leadingNum(filepath.Base(p)))
	}
	return top + 1, nil
}

// leadingNum is the number a file name starts with ("T12-x.md" → 12), 0 if none. Lenient on purpose, as
// task.ParseID: a misnamed task ("12-x.md", "t012-x.md") still holds its number.
func leadingNum(name string) int {
	id, _, _ := strings.Cut(name, "-")
	n, _ := task.ParseID(id)
	return n
}
