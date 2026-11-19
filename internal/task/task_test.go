package task

import (
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"testing"
)

func TestFileNameParseName(t *testing.T) {
	for _, tc := range []struct {
		name string
		num  int
		slug string
		ok   bool
	}{
		{"T12-foo.md", 12, "foo", true},
		{"T1-a.md", 1, "a", true},
		{"T12345-big-one.md", 12345, "big-one", true},
		{"T7-a1-b2-c3.md", 7, "a1-b2-c3", true},
		{"0012-foo.md", 0, "", false},    // no T
		{"12-foo.md", 0, "", false},      // no T
		{"t12-foo.md", 0, "", false},     // lowercase t
		{"T012-foo.md", 0, "", false},    // padded
		{"T0-foo.md", 0, "", false},      // number 0
		{"T-foo.md", 0, "", false},       // no number
		{"T12-Foo.md", 0, "", false},     // uppercase
		{"T12-foo-.md", 0, "", false},    // trailing dash
		{"T12--foo.md", 0, "", false},    // double dash
		{"T12-.md", 0, "", false},        // empty slug
		{"T12.md", 0, "", false},         // no slug
		{"T12-foo.txt", 0, "", false},    // not .md
		{"T12-foo.md.bak", 0, "", false}, // not .md
		{"+T12-foo.md", 0, "", false},
		{"T+12-foo.md", 0, "", false},
		{"T1a2-foo.md", 0, "", false},
		{"foo.md", 0, "", false},
		{"T99999999999999999999-x.md", 0, "", false}, // overflow
	} {
		num, slug, ok := ParseName(tc.name)
		if num != tc.num || slug != tc.slug || ok != tc.ok {
			t.Errorf("ParseName(%q) = %d, %q, %v; want %d, %q, %v", tc.name, num, slug, ok, tc.num, tc.slug, tc.ok)
		}
		if tc.ok {
			if got := FileName(tc.num, tc.slug); got != tc.name {
				t.Errorf("FileName(%d, %q) = %q; want %q", tc.num, tc.slug, got, tc.name)
			}
		}
	}
}

func TestParseID(t *testing.T) {
	for _, tc := range []struct {
		in  string
		num int
		ok  bool
	}{
		{"T12", 12, true}, {"t12", 12, true}, {"12", 12, true}, {"T0012", 12, true}, {"0012", 12, true},
		{"T0", 0, false}, {"0", 0, false}, {"T", 0, false}, {"", 0, false}, {"TT12", 0, false}, {"T-1", 0, false},
		{"T+1", 0, false}, {"-1", 0, false}, {"D12", 0, false}, {"T12x", 0, false}, {"T99999999999999999999", 0, false},
	} {
		if num, ok := ParseID(tc.in); num != tc.num && tc.ok || ok != tc.ok {
			t.Errorf("ParseID(%q) = %d, %v; want %d, %v", tc.in, num, ok, tc.num, tc.ok)
		}
	}
	if got := ID(12); got != "T12" {
		t.Errorf("ID(12) = %q", got)
	}
	if got := JoinIDs([]int{3, 15}); got != "T3 T15" {
		t.Errorf("JoinIDs = %q", got)
	}
}

func TestValidSlug(t *testing.T) {
	for s, want := range map[string]bool{
		"a": true, "foo": true, "foo-bar": true, "a1-2b-c": true, "0": true,
		"": false, "-": false, "-a": false, "a-": false, "a--b": false, "Foo": false, "a_b": false, "a b": false,
		"a.b": false, "é": false,
	} {
		if got := ValidSlug(s); got != want {
			t.Errorf("ValidSlug(%q) = %v; want %v", s, got, want)
		}
	}
}

func TestParse(t *testing.T) {
	for _, tc := range []struct {
		name, data string
		header     []Field
		title, ask string
		body       string
		err        string // substring; "" = no error
	}{
		{
			name:   "full",
			data:   "---\nkind: bug\npri: 1\nstatus: ready\narea: a/ b/\n---\n# Fix it\nbody\n",
			header: []Field{{"kind", "bug"}, {"pri", "1"}, {"status", "ready"}, {"area", "a/ b/"}},
			title:  "Fix it", body: "# Fix it\nbody\n",
		},
		{
			name:   "trimmed, split at first colon, empty value",
			data:   "---\n  kind :  feat  \nfrom: me@abc:def\nneeds:\n---\n#  Title  \n",
			header: []Field{{"kind", "feat"}, {"from", "me@abc:def"}, {"needs", ""}},
			title:  "Title", body: "#  Title  \n",
		},
		{
			name:   "empty header, no body",
			data:   "---\n---\n",
			header: nil, body: "",
		},
		{
			name:   "closing --- at EOF",
			data:   "---\nkind: bug\n---",
			header: []Field{{"kind", "bug"}}, body: "",
		},
		{
			name:   "title is the first '# ' line; '#x' and '##' are not",
			data:   "---\n---\n#nospace\n## Sub\ntext\n# Real\n# Second\n",
			header: nil, title: "Real", body: "#nospace\n## Sub\ntext\n# Real\n# Second\n",
		},
		{
			name:   "ask",
			data:   "---\n---\n# T\n## Ask\n\n  Ship it? rec: yes  \nmore\n## Progress\n",
			header: nil, title: "T", ask: "Ship it? rec: yes",
			body: "# T\n## Ask\n\n  Ship it? rec: yes  \nmore\n## Progress\n",
		},
		{
			name:   "empty ask section",
			data:   "---\n---\n# T\n## Ask\n\n## Progress\nhalf done\n",
			header: nil, title: "T", body: "# T\n## Ask\n\n## Progress\nhalf done\n",
		},
		{
			name:   "## Asking is not ## Ask",
			data:   "---\n---\n# T\n## Asking\nno\n",
			header: nil, title: "T", body: "# T\n## Asking\nno\n",
		},
		{
			name:   "--- inside the body is body",
			data:   "---\nkind: bug\n---\n# T\n---\nx: y\n",
			header: []Field{{"kind", "bug"}}, title: "T", body: "# T\n---\nx: y\n",
		},
		{
			name:   "CRLF",
			data:   "---\r\nkind: bug\r\nneeds:\r\n---\r\n# T\r\n## Ask\r\nq?\r\n",
			header: []Field{{"kind", "bug"}, {"needs", ""}}, title: "T", ask: "q?",
			body: "# T\r\n## Ask\r\nq?\r\n",
		},
		{
			name:   "CRLF closed at EOF",
			data:   "---\r\nkind: bug\r\n---\r",
			header: []Field{{"kind", "bug"}}, body: "",
		},
		{name: "no header", data: "# Title\n", err: "no header"},
		{name: "empty file", data: "", err: "no header"},
		{name: "only opening", data: "---", err: "no header"},
		{name: "indented opening", data: " ---\n---\n", err: "no header"},
		{name: "unclosed", data: "---\nkind: bug\n", err: "not closed"},
		{name: "line without colon", data: "---\nkind bug\n---\n", err: "line 2"},
		{name: "blank header line", data: "---\nkind: bug\n\n---\n", err: "line 3"},
		{name: "empty key", data: "---\n: bug\n---\n", err: "line 2"},
		{name: "duplicate key", data: "---\nkind: bug\nkind: feat\n---\n", err: `duplicate key "kind"`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			c, err := Parse("/x/tasks/T42-the-slug.md", []byte(tc.data))
			if tc.err != "" {
				if err == nil || !strings.Contains(err.Error(), tc.err) {
					t.Fatalf("err = %v; want containing %q", err, tc.err)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if c.Num != 42 || c.Slug != "the-slug" || c.Path != "/x/tasks/T42-the-slug.md" {
				t.Errorf("num, slug, path = %d, %q, %q", c.Num, c.Slug, c.Path)
			}
			if !reflect.DeepEqual(c.Header, tc.header) {
				t.Errorf("header = %q; want %q", c.Header, tc.header)
			}
			if c.Title != tc.title || c.Ask != tc.ask {
				t.Errorf("title, ask = %q, %q; want %q, %q", c.Title, c.Ask, tc.title, tc.ask)
			}
			if string(c.Body) != tc.body {
				t.Errorf("body = %q; want %q", c.Body, tc.body)
			}
			if got := string(c.Bytes()); got != tc.data {
				t.Errorf("Bytes() = %q; want the input %q", got, tc.data)
			}
		})
	}
}

func TestParsePath(t *testing.T) {
	if _, err := Parse("/x/tasks/notes.md", []byte("---\n---\n")); err == nil || err.Error() != "bad filename" {
		t.Errorf("bad name: err = %v", err)
	}
	dir := t.TempDir()
	t.Chdir(dir)
	c, err := Parse("tasks/T1-a.md", []byte("---\n---\n"))
	if err != nil {
		t.Fatal(err)
	}
	if want := filepath.Join(dir, "tasks/T1-a.md"); c.Path != want {
		t.Errorf("Path = %q; want absolute %q", c.Path, want)
	}
}

// TestRoundTrip: an unmodified task renders byte for byte, with and without a trailing newline, and with
// hand-formatted header lines.
func TestRoundTrip(t *testing.T) {
	for _, data := range []string{
		"---\nkind: bug\nstatus: new\n---\n# T\n\nbody\n",
		"---\nkind: bug\nstatus: new\n---\n# T\n\nbody",
		"---\nkind: bug\n---\n# T",
		"---\nkind: bug\n---\n",
		"---\nkind: bug\n---",
		"---\n---\n",
		"---\nkind:bug\n  status :  new  \nfoo: bar\n---\n# T\n\n\n## Ask\nq?\n",
		"---\nkind: bug\n---\n# T\n\ttabs \t\n\x00binary\xff\n",
		"---\r\nkind: bug\r\nstatus: new\r\n---\r\n# T\r\n",
		"---\r\nkind: bug\nstatus: new\r\n---\n# T\r\n",
	} {
		c, err := Parse("/t/T1-x.md", []byte(data))
		if err != nil {
			t.Fatalf("%q: %v", data, err)
		}
		if got := string(c.Bytes()); got != data {
			t.Errorf("Bytes() = %q; want %q", got, data)
		}
	}
}

func TestGet(t *testing.T) {
	c := mustParse(t, "T1-x.md", "---\nkind: bug\nneeds:\n---\n")
	for key, want := range map[string]string{"kind": "bug", "needs": "", "pri": ""} {
		if got := c.Get(key); got != want {
			t.Errorf("Get(%q) = %q; want %q", key, got, want)
		}
	}
}

func TestSet(t *testing.T) {
	for _, tc := range []struct {
		name, in   string
		key, value string
		out        string
	}{
		{"change in place", "---\nkind: bug\nstatus: new\n---\n# T\n", "kind", "feat",
			"---\nkind: feat\nstatus: new\n---\n# T\n"},
		{"change keeps unknown key position", "---\nfoo: 1\nstatus: new\nbar: 2\n---\n", "status", "ready",
			"---\nfoo: 1\nstatus: ready\nbar: 2\n---\n"},
		{"new known key in Keys order", "---\nkind: bug\nstatus: new\nfrom: a@b\n---\n", "pri", "1",
			"---\nkind: bug\npri: 1\nstatus: new\nfrom: a@b\n---\n"},
		{"new known key after an earlier one", "---\nkind: bug\nstatus: new\nfrom: a@b\n---\n", "area", "x/ y/",
			"---\nkind: bug\nstatus: new\narea: x/ y/\nfrom: a@b\n---\n"},
		{"new first key goes first", "---\nstatus: new\n---\n", "kind", "bug",
			"---\nkind: bug\nstatus: new\n---\n"},
		{"new known key with only unknowns goes first", "---\nfoo: 1\n---\n", "pri", "2",
			"---\npri: 2\nfoo: 1\n---\n"},
		{"new known key after known, before trailing unknown", "---\nkind: bug\nfoo: 1\n---\n", "status", "new",
			"---\nkind: bug\nstatus: new\nfoo: 1\n---\n"},
		{"new last known key", "---\nkind: bug\nfoo: 1\n---\n", "from", "x@y",
			"---\nkind: bug\nfrom: x@y\nfoo: 1\n---\n"},
		{"new key into empty header", "---\n---\n# T\n", "status", "new",
			"---\nstatus: new\n---\n# T\n"},
		{"unknown key goes last", "---\nkind: bug\nstatus: new\n---\n", "zzz", "1",
			"---\nkind: bug\nstatus: new\nzzz: 1\n---\n"},
		{"empty deletes", "---\nkind: bug\nneeds: 3\nstatus: new\n---\nbody", "needs", "",
			"---\nkind: bug\nstatus: new\n---\nbody"},
		{"delete absent is a no-op", "---\nkind: bug\n---\n", "needs", "",
			"---\nkind: bug\n---\n"},
		{"whitespace-only deletes", "---\nkind: bug\n---\n", "kind", "  ",
			"---\n---\n"},
		{"value whitespace collapsed", "---\n---\n", "area", "  a/\n b/\t c/ ",
			"---\narea: a/ b/ c/\n---\n"},
		{"untouched hand-formatted lines kept", "---\nkind:bug\n status :new\n---\n", "pri", "3",
			"---\nkind:bug\npri: 3\n status :new\n---\n"},
		{"edited hand-formatted line normalised", "---\nkind:bug\n---\n", "kind", "feat",
			"---\nkind: feat\n---\n"},
		{"same value keeps raw line", "---\nkind:bug\n---\n", "kind", "bug",
			"---\nkind:bug\n---\n"},
		{"set into header closed at EOF", "---\nkind: bug\n---", "pri", "1",
			"---\nkind: bug\npri: 1\n---"},
		{"CRLF: new and edited lines end in CRLF", "---\r\nkind: bug\r\nstatus: new\r\n---\r\n# T\r\n", "pri", "1",
			"---\r\nkind: bug\r\npri: 1\r\nstatus: new\r\n---\r\n# T\r\n"},
		{"CRLF: edit in place", "---\r\nkind: bug\r\n---\r\n", "kind", "feat",
			"---\r\nkind: feat\r\n---\r\n"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			c := mustParse(t, "T1-x.md", tc.in)
			c.Set(tc.key, tc.value)
			if got := string(c.Bytes()); got != tc.out {
				t.Errorf("got  %q\nwant %q", got, tc.out)
			}
			// The rendered task parses back to the same header.
			back := mustParse(t, "T1-x.md", tc.out)
			if !reflect.DeepEqual(back.Header, c.Header) && !(len(back.Header) == 0 && len(c.Header) == 0) {
				t.Errorf("reparsed header %q; want %q", back.Header, c.Header)
			}
		})
	}
}

func TestBytesHandBuilt(t *testing.T) {
	c := &Task{Header: []Field{{"kind", "bug"}, {"status", "new"}}, Body: []byte("# T\n")}
	if got, want := string(c.Bytes()), "---\nkind: bug\nstatus: new\n---\n# T\n"; got != want {
		t.Errorf("got %q; want %q", got, want)
	}
}

func TestCheck(t *testing.T) {
	const ok = "kind: bug\npri: 1\nstatus: ready\narea: a/\n"
	for _, tc := range []struct {
		name, header string
		want         []string
	}{
		{"valid ready", ok, nil},
		{"valid new, minimal", "kind: feat\nstatus: new\n", nil},
		{"valid triage with from", "kind: spike\nstatus: triage\nfrom: web@3f2a9c1\n", nil},
		{"valid blocked on owner", "kind: chore\npri: 2\nstatus: blocked\narea: x\nneeds: owner\n", nil},
		{"valid blocked on task", "kind: owner\npri: 3\nstatus: blocked\narea: x\nneeds: 0007\n", nil},
		{"missing kind, new", "status: new\n", nil},
		{"missing kind, triage", "status: triage\n", nil},
		{"missing kind, ready", "pri: 1\nstatus: ready\narea: a\n", []string{"missing kind"}},
		{"missing kind, blocked", "status: blocked\nneeds: owner\n", []string{"missing kind"}},
		{"empty kind", "kind:\npri: 1\nstatus: ready\narea: a\n", []string{"missing kind"}},
		{"bad kind", "kind: x\nstatus: new\n", []string{`bad kind "x"`}},
		{"bad pri", "kind: bug\npri: 4\nstatus: new\n", []string{`bad pri "4"`}},
		{"bad pri word", "kind: bug\npri: high\nstatus: new\n", []string{`bad pri "high"`}},
		{"excl yes", "kind: bug\nexcl: yes\nstatus: new\n", nil},
		{"bad excl", "kind: bug\nexcl: no\nstatus: new\n", []string{`bad excl "no"`}},
		{"missing status", "kind: bug\n", []string{"missing status"}},
		{"bad status", "kind: bug\nstatus: doing\n", []string{`bad status "doing"`}},
		{"ready without pri", "kind: bug\nstatus: ready\narea: a\n", []string{"ready without pri"}},
		{"ready without area", "kind: bug\npri: 1\nstatus: ready\n", []string{"ready without area"}},
		{"ready without both", "kind: bug\nstatus: ready\n", []string{"ready without pri", "ready without area"}},
		{"blocked without needs", "kind: bug\nstatus: blocked\n", []string{"blocked without needs"}},
		{"blocked on owner, untriaged", "kind: bug\nstatus: blocked\nneeds: owner\n", nil},
		{"blocked on a task without pri, area", "kind: bug\nstatus: blocked\nneeds: 7\n",
			[]string{"blocked without pri", "blocked without area"}},
		{"owner task ready without area", "kind: owner\npri: 2\nstatus: ready\n", nil},
		{"needs on non-blocked", ok + "needs: owner\n", []string{"needs on a task that isn't blocked"}},
		{"needs on new", "kind: bug\nstatus: new\nneeds: 3\n", []string{"needs on a task that isn't blocked"}},
		{"needs itself", "kind: bug\npri: 1\nstatus: blocked\narea: a\nneeds: 42\n", []string{"needs itself"}},
		{"needs itself padded", "kind: bug\npri: 1\nstatus: blocked\narea: a\nneeds: 0042\n", []string{"needs itself"}},
		{"bad needs", "kind: bug\npri: 1\nstatus: blocked\narea: a\nneeds: bob\n", []string{`bad needs "bob"`}},
		{"bad needs sign", "kind: bug\npri: 1\nstatus: blocked\narea: a\nneeds: +3\n", []string{`bad needs "+3"`}},
		{"bad needs zero", "kind: bug\npri: 1\nstatus: blocked\narea: a\nneeds: 0\n", []string{`bad needs "0"`}},
		{"valid needs several", "kind: bug\npri: 1\nstatus: blocked\narea: a\nneeds: 3  05 7\n", nil},
		{"needs itself among several", "kind: bug\npri: 1\nstatus: blocked\narea: a\nneeds: 3 42\n",
			[]string{"needs itself"}},
		{"bad needs owner plus task", "kind: bug\npri: 1\nstatus: blocked\narea: a\nneeds: owner 3\n",
			[]string{`bad needs "owner 3"`}},
		{"bad needs comma", "kind: bug\npri: 1\nstatus: blocked\narea: a\nneeds: 3,5\n", []string{`bad needs "3,5"`}},
		{"unknown keys, in order", ok + "foo: 1\nBar: 2\n", []string{`unknown key "foo"`, `unknown key "Bar"`}},
		{"everything", "kind: x\npri: 9\nstatus: nope\nneeds: 1\nzz: 1\n",
			[]string{`bad kind "x"`, `bad pri "9"`, `bad status "nope"`, "needs on a task that isn't blocked",
				`unknown key "zz"`}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			c := mustParse(t, "T42-x.md", "---\n"+tc.header+"---\n# Title\n")
			if got := c.Check(); !reflect.DeepEqual(got, tc.want) {
				t.Errorf("Check() = %q; want %q", got, tc.want)
			}
		})
	}
	t.Run("no title", func(t *testing.T) {
		c := mustParse(t, "T42-x.md", "---\n"+ok+"---\nbody only\n## Ask\n")
		if got, want := c.Check(), []string{"no title"}; !reflect.DeepEqual(got, want) {
			t.Errorf("Check() = %q; want %q", got, want)
		}
	})
}

func TestValidate(t *testing.T) {
	good := "---\nkind: bug\nstatus: new\n---\n# T\n"
	tasks := []*Task{
		mustParse(t, "T1-a.md", good),
		mustParse(t, "T2-b.md", "---\npri: 1\nstatus: ready\narea: a\n---\n# T\n"),
		mustParse(t, "T2-c.md", good),
		mustParse(t, "T3-d.md", good),
	}
	want := []Problem{
		{2, "T2-b.md", "missing kind"},
		{2, "T2-b.md", "duplicate number"},
		{2, "T2-c.md", "duplicate number"},
	}
	if got := Validate(tasks); !reflect.DeepEqual(got, want) {
		t.Errorf("Validate = %+v\nwant %+v", got, want)
	}
	if got := Validate(nil); got != nil {
		t.Errorf("Validate(nil) = %+v", got)
	}
}

func TestLoad(t *testing.T) {
	t.Run("missing dir", func(t *testing.T) {
		tasks, problems, err := Load(filepath.Join(t.TempDir(), "nope"))
		if tasks != nil || problems != nil || err != nil {
			t.Errorf("got %v, %v, %v", tasks, problems, err)
		}
	})
	t.Run("not a dir", func(t *testing.T) {
		f := filepath.Join(t.TempDir(), "file")
		write(t, f, "")
		if _, _, err := Load(f); err == nil {
			t.Error("want an error for a file")
		}
	})
	t.Run("mixed", func(t *testing.T) {
		dir := t.TempDir()
		good := "---\nkind: bug\nstatus: new\n---\n# T\n"
		for name, data := range map[string]string{
			"T10-ten.md":    good,
			"T2-two.md":     good,
			"T12345-big.md": good,
			"T3-broken.md":  "no header\n",
			"notes.md":      good,
			"0012-old.md":   good,
			".keep":         "",
			".T4-hidden.md": good,
			"README.txt":    "x",
			"T5-x.md.orig":  good,
		} {
			write(t, filepath.Join(dir, name), data)
		}
		if err := os.Mkdir(filepath.Join(dir, "T6-dir.md"), 0o755); err != nil {
			t.Fatal(err)
		}
		tasks, problems, err := Load(dir)
		if err != nil {
			t.Fatal(err)
		}
		var got []string
		for _, c := range tasks {
			got = append(got, filepath.Base(c.Path))
			if c.Path != filepath.Join(dir, filepath.Base(c.Path)) {
				t.Errorf("Path = %q", c.Path)
			}
		}
		if want := []string{"T2-two.md", "T3-broken.md", "T10-ten.md", "T12345-big.md"}; !slices.Equal(got, want) {
			t.Errorf("tasks = %q; want %q", got, want)
		}
		for _, c := range tasks {
			if (c.Err != nil) != (c.Num == 3) {
				t.Errorf("task %d: Err = %v", c.Num, c.Err)
			}
		}
		wantP := []Problem{
			{0, "0012-old.md", "bad filename"},
			{3, "T3-broken.md", "no header (first line must be ---)"},
			{0, "notes.md", "bad filename"},
		}
		slices.SortFunc(problems, func(a, b Problem) int { return strings.Compare(a.File, b.File) })
		slices.SortFunc(wantP, func(a, b Problem) int { return strings.Compare(a.File, b.File) })
		if !reflect.DeepEqual(problems, wantP) {
			t.Errorf("problems = %+v\nwant %+v", problems, wantP)
		}
	})
}

// TestLoadUnreadable: a well-named file that doesn't parse is still a task (it counts, commands find it), and
// writing it back changes nothing.
func TestLoadUnreadable(t *testing.T) {
	dir := t.TempDir()
	data := "---\nkind: bug\n\nstatus: ready\n---\n# Blank line in the header\n"
	write(t, filepath.Join(dir, "T7-blank.md"), data)
	write(t, filepath.Join(dir, "T7-dup.md"), "---\nkind: bug\nstatus: new\n---\n# T\n")
	tasks, problems, err := Load(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(tasks) != 2 {
		t.Fatalf("tasks = %d; want 2", len(tasks))
	}
	c := tasks[0]
	if c.Num != 7 || c.Slug != "blank" || c.Path != filepath.Join(dir, "T7-blank.md") || c.Title != Unreadable ||
		c.Header != nil || c.Err == nil || !strings.Contains(c.Err.Error(), "line 3") {
		t.Errorf("unreadable task = %+v", c)
	}
	if want := []Problem{{7, "T7-blank.md", c.Err.Error()}}; !reflect.DeepEqual(problems, want) {
		t.Errorf("problems = %+v; want %+v", problems, want)
	}
	if got := c.Check(); got != nil {
		t.Errorf("Check = %q; want none (Load reports the parse error)", got)
	}
	want := []Problem{{7, "T7-blank.md", "duplicate number"}, {7, "T7-dup.md", "duplicate number"}}
	if got := Validate(tasks); !reflect.DeepEqual(got, want) {
		t.Errorf("Validate = %+v; want %+v", got, want)
	}
	c.Set("pri", "1")
	if got := string(c.Bytes()); got != data {
		t.Errorf("Bytes = %q; want the file unchanged", got)
	}
}

func mustParse(t *testing.T, name, data string) *Task {
	t.Helper()
	c, err := Parse("/t/tasks/"+name, []byte(data))
	if err != nil {
		t.Fatalf("Parse(%q): %v", data, err)
	}
	return c
}

func write(t *testing.T, path, data string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(data), 0o644); err != nil {
		t.Fatal(err)
	}
}
