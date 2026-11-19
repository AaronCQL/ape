// Package task parses, edits and validates tasks: ape/tasks/T12-slug.md (SPEC.md "Task file").
package task

import (
	"bytes"
	"cmp"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
)

// Known header keys, in the order ape writes them.
var Keys = []string{"kind", "pri", "status", "area", "excl", "needs", "from"}

var (
	Kinds    = []string{"bug", "feat", "spike", "chore", "owner"}
	Statuses = []string{"new", "triage", "ready", "blocked"}
)

// Field is one header line.
type Field struct{ Key, Value string }

// Task is one tasks/T12-slug.md file.
type Task struct {
	Num    int
	Slug   string
	Path   string  // absolute
	Header []Field // in file order
	Title  string
	Ask    string // first non-empty line after "## Ask"; "" if none
	Body   []byte // everything after the header's closing "---\n", byte for byte
	// Err is why a well-named file failed to parse. Such a task has Num, Slug and Path, Title "(unreadable)",
	// no Header and no Body; Bytes returns the file as read, and Check reports nothing (Load's Problem does).
	Err error

	// raw holds each parsed header line as written, by key. Bytes reuses a line whose value is unchanged,
	// so a hand-formatted line ("kind:bug") survives an edit to another key.
	raw map[string]rawLine
	// open and close are the delimiter lines as written ("---", or "---\r" in a CRLF file); "" means "---".
	open, close string
	// cr is "\r" when the opening line ends in CRLF: a header line ape adds ends the same way.
	cr string
	// unterminated: the file ended at the closing "---" with no newline.
	unterminated bool
	// orig is an unreadable task's file, for Bytes.
	orig []byte
}

type rawLine struct{ value, line string }

// Problem is a validation finding, shown in the board's CHECK section.
type Problem struct {
	Num  int    // 0 if the file name has no number
	File string // base name
	Msg  string
}

// ID returns a task's ID: "T12". It names the task everywhere: prose, needs, the board, commits, its file
// (FileName), its branch and worktree.
func ID(num int) string { return "T" + strconv.Itoa(num) }

// ParseID parses a task ID as typed: "T12", "t12" or a bare "12" (leading zeros allowed). ok is false for anything
// else, and for 0.
func ParseID(s string) (num int, ok bool) {
	if s != "" && (s[0] == 'T' || s[0] == 't') {
		s = s[1:]
	}
	if !allDigits(s) {
		return 0, false
	}
	num, err := strconv.Atoi(s)
	return num, err == nil && num > 0
}

// FileName returns "T12-slug.md".
func FileName(num int, slug string) string { return ID(num) + "-" + slug + ".md" }

// ParseName splits "T12-slug.md" into its number and slug; ok is false for any other name.
// Only the canonical spelling is accepted (ParseName(FileName(n, s)) round-trips): "T012-x.md" and "12-x.md" are
// not tasks, so a number names one file. Number 0 is rejected (Problem.Num uses 0 for "no number").
func ParseName(base string) (num int, slug string, ok bool) {
	stem, found := strings.CutSuffix(base, ".md")
	if !found {
		return 0, "", false
	}
	id, slug, found := strings.Cut(stem, "-")
	if !found || !ValidSlug(slug) {
		return 0, "", false
	}
	num, ok = ParseID(id)
	if !ok || FileName(num, slug) != base {
		return 0, "", false
	}
	return num, slug, true
}

// ValidSlug reports whether s is [a-z0-9]+(-[a-z0-9]+)*.
func ValidSlug(s string) bool {
	if s == "" {
		return false
	}
	for part := range strings.SplitSeq(s, "-") {
		if part == "" {
			return false
		}
		for _, r := range part {
			if !('a' <= r && r <= 'z' || '0' <= r && r <= '9') {
				return false
			}
		}
	}
	return true
}

func allDigits(s string) bool {
	for _, r := range s {
		if r < '0' || r > '9' {
			return false
		}
	}
	return s != ""
}

const delim = "---"

// Unreadable is the Title of a task whose file doesn't parse (Task.Err).
const Unreadable = "(unreadable)"

// Parse parses a task file's contents. path is used for Num, Slug and Path. Lines may end in "\r\n": the "\r" is
// not part of a header line's value, and Bytes writes it back.
func Parse(path string, data []byte) (*Task, error) {
	num, slug, ok := ParseName(filepath.Base(path))
	if !ok {
		return nil, errors.New("bad filename")
	}
	abs, err := filepath.Abs(path)
	if err != nil {
		return nil, err
	}
	c := &Task{Num: num, Slug: slug, Path: abs, raw: map[string]rawLine{}}

	first, rest, found := bytes.Cut(data, []byte("\n"))
	if !found || trimCR(first) != delim {
		return nil, errors.New("no header (first line must be ---)")
	}
	c.open = string(first)
	if c.open != delim {
		c.cr = "\r"
	}
	for n := 2; ; n++ {
		if len(rest) == 0 {
			return nil, errors.New("header not closed (no --- line)")
		}
		line, after, found := bytes.Cut(rest, []byte("\n"))
		rest = after
		if trimCR(line) == delim {
			c.Body = rest
			c.close = string(line)
			c.unterminated = !found
			break
		}
		key, value, ok := strings.Cut(trimCR(line), ":")
		key, value = strings.TrimSpace(key), strings.TrimSpace(value)
		if !ok || key == "" {
			return nil, fmt.Errorf("header line %d: want key: value", n)
		}
		if _, dup := c.raw[key]; dup {
			return nil, fmt.Errorf("header line %d: duplicate key %q", n, key)
		}
		c.raw[key] = rawLine{value, string(line)}
		c.Header = append(c.Header, Field{key, value})
	}
	c.Title, c.Ask = scanBody(c.Body)
	return c, nil
}

func trimCR(line []byte) string { return strings.TrimSuffix(string(line), "\r") }

// scanBody finds the title (first "# " line) and the Ask (first non-empty line after "## Ask", unless that line
// is another heading, which means the Ask section is empty).
func scanBody(body []byte) (title, ask string) {
	inAsk, askDone := false, false
	for line := range strings.SplitSeq(string(body), "\n") {
		line = strings.TrimRight(line, " \t\r")
		switch {
		case title == "" && strings.HasPrefix(line, "# "):
			title = strings.TrimSpace(line[2:])
		case askDone:
		case line == "## Ask":
			inAsk = true
		case inAsk && strings.TrimSpace(line) != "":
			if !strings.HasPrefix(line, "#") {
				ask = strings.TrimSpace(line)
			}
			inAsk, askDone = false, true
		}
	}
	return title, ask
}

// Load parses every *.md in tasksDir. Unparsable files and bad names become Problems, not errors; an unparsable
// file with a task's name is also a Task, with Err set.
// A missing tasksDir is an empty list. Tasks are sorted by number, then slug.
func Load(tasksDir string) ([]*Task, []Problem, error) {
	entries, err := os.ReadDir(tasksDir)
	if errors.Is(err, fs.ErrNotExist) {
		return nil, nil, nil
	}
	if err != nil {
		return nil, nil, err
	}
	var tasks []*Task
	var problems []Problem
	for _, e := range entries {
		name := e.Name()
		if strings.HasPrefix(name, ".") || !strings.HasSuffix(name, ".md") || e.IsDir() {
			continue
		}
		path := filepath.Join(tasksDir, name)
		data, err := os.ReadFile(path)
		if err != nil {
			return nil, nil, err
		}
		c, err := Parse(path, data)
		if err != nil {
			num, slug, ok := ParseName(name)
			problems = append(problems, Problem{Num: num, File: name, Msg: err.Error()})
			if !ok {
				continue
			}
			// A well-named file is still task num: it counts, holds its number, and commands find it.
			abs, aerr := filepath.Abs(path)
			if aerr != nil {
				return nil, nil, aerr
			}
			c = &Task{Num: num, Slug: slug, Path: abs, Title: Unreadable, Err: err, orig: data}
		}
		tasks = append(tasks, c)
	}
	slices.SortStableFunc(tasks, func(a, b *Task) int {
		if a.Num != b.Num {
			return a.Num - b.Num
		}
		return strings.Compare(a.Slug, b.Slug)
	})
	return tasks, problems, nil
}

// Get returns a header value, "" if absent.
func (c *Task) Get(key string) string {
	for _, f := range c.Header {
		if f.Key == key {
			return f.Value
		}
	}
	return ""
}

// Set sets a header value in place; a new known key goes at its position in Keys order, an unknown one last;
// value "" deletes the key.
//
// Whitespace in value is collapsed to single spaces (a header value is one trimmed line). A new known key goes
// after the last known key that precedes it in Keys, or first if there is none.
func (c *Task) Set(key, value string) {
	value = strings.Join(strings.Fields(value), " ")
	i := slices.IndexFunc(c.Header, func(f Field) bool { return f.Key == key })
	switch {
	case value == "":
		c.Header = slices.DeleteFunc(c.Header, func(f Field) bool { return f.Key == key })
	case i >= 0:
		c.Header[i].Value = value
	default:
		c.Header = slices.Insert(c.Header, c.insertAt(key), Field{key, value})
	}
}

func (c *Task) insertAt(key string) int {
	rank := slices.Index(Keys, key)
	if rank < 0 {
		return len(c.Header)
	}
	at := 0
	for i, f := range c.Header {
		if r := slices.Index(Keys, f.Key); r >= 0 && r < rank {
			at = i + 1
		}
	}
	return at
}

// Bytes renders the task: header then Body unchanged. An unreadable task (Err) renders as its file was read.
func (c *Task) Bytes() []byte {
	if c.Err != nil {
		return bytes.Clone(c.orig)
	}
	var b bytes.Buffer
	b.WriteString(cmp.Or(c.open, delim) + "\n")
	for _, f := range c.Header {
		if r, ok := c.raw[f.Key]; ok && r.value == f.Value {
			b.WriteString(r.line)
		} else {
			b.WriteString(f.Key + ": " + f.Value + c.cr)
		}
		b.WriteByte('\n')
	}
	b.WriteString(cmp.Or(c.close, delim))
	if !c.unterminated || len(c.Body) > 0 {
		b.WriteByte('\n')
	}
	b.Write(c.Body)
	return b.Bytes()
}

// Check returns the task's own problems (values, required keys, unknown keys, title). See SPEC.md.
func (c *Task) Check() []string {
	if c.Err != nil {
		return nil
	}
	var ps []string
	add := func(format string, args ...any) { ps = append(ps, fmt.Sprintf(format, args...)) }
	kind, pri, status, area, needs := c.Get("kind"), c.Get("pri"), c.Get("status"), c.Get("area"), c.Get("needs")

	switch {
	case kind == "" && (status == "ready" || status == "blocked"): // triage sets it
		add("missing kind")
	case kind == "":
	case !slices.Contains(Kinds, kind):
		add("bad kind %q", kind)
	}
	if pri != "" && pri != "1" && pri != "2" && pri != "3" {
		add("bad pri %q", pri)
	}
	if excl := c.Get("excl"); excl != "" && excl != "yes" {
		add("bad excl %q", excl)
	}
	switch {
	case status == "":
		add("missing status")
	case !slices.Contains(Statuses, status):
		add("bad status %q", status)
	case status == "ready" || (status == "blocked" && needs != "" && needs != "owner"):
		// Ranked work needs pri and area; a task blocked on the owner may still be in triage,
		// and an owner task touches no code.
		if pri == "" {
			add("%s without pri", status)
		}
		if area == "" && kind != "owner" {
			add("%s without area", status)
		}
	}
	switch {
	case status == "blocked" && needs == "":
		add("blocked without needs")
	case needs == "":
	case status != "blocked":
		add("needs on a task that isn't blocked")
	case needs == "owner":
	default:
		if nums, ok := NeedsNums(needs); !ok {
			add("bad needs %q", needs)
		} else if slices.Contains(nums, c.Num) {
			add("needs itself")
		}
	}
	for _, f := range c.Header {
		if !slices.Contains(Keys, f.Key) {
			add("unknown key %q", f.Key)
		}
	}
	if c.Title == "" {
		add("no title")
	}
	return ps
}

// NeedsNums parses a needs value naming tasks: IDs separated by spaces, as ParseID reads them ("T3 t05 7" → 3 5
// 7). ok is false for anything else: "", "owner", a token that isn't an ID.
func NeedsNums(v string) (nums []int, ok bool) {
	for tok := range strings.FieldsSeq(v) {
		n, ok := ParseID(tok)
		if !ok {
			return nil, false
		}
		nums = append(nums, n)
	}
	return nums, len(nums) > 0
}

// JoinIDs renders nums as space-separated IDs: "T3 T5".
func JoinIDs(nums []int) string {
	ids := make([]string, len(nums))
	for i, n := range nums {
		ids[i] = ID(n)
	}
	return strings.Join(ids, " ")
}

// Validate returns every task's problems plus duplicate numbers.
func Validate(tasks []*Task) []Problem {
	count := map[int]int{}
	for _, c := range tasks {
		count[c.Num]++
	}
	var ps []Problem
	for _, c := range tasks {
		file := filepath.Base(c.Path)
		for _, msg := range c.Check() {
			ps = append(ps, Problem{c.Num, file, msg})
		}
		if count[c.Num] > 1 {
			ps = append(ps, Problem{c.Num, file, "duplicate number"})
		}
	}
	return ps
}
