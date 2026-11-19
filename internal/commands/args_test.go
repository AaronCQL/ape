package commands

import (
	"flag"
	"slices"
	"strings"
	"testing"
)

func TestParseArgs(t *testing.T) {
	for _, tc := range []struct {
		args  []string
		pos   []string
		kind  string
		force bool
		err   string // substring; "" = no error
	}{
		{args: []string{"x", "--kind", "bug"}, pos: []string{"x"}, kind: "bug"},
		{args: []string{"--kind=bug", "x"}, pos: []string{"x"}, kind: "bug"},
		{args: []string{"-kind", "bug", "x", "-force"}, pos: []string{"x"}, kind: "bug", force: true},
		{args: []string{"1", "-20% p95; tests green"}, pos: []string{"1", "-20% p95; tests green"}},
		{args: []string{"1", "-x", "--bogus", "-"}, pos: []string{"1", "-x", "--bogus", "-"}},
		{args: []string{"x", "--kind", "-y"}, pos: []string{"x"}, kind: "-y"},
		{args: []string{"--force", "--", "--kind", "a"}, pos: []string{"--kind", "a"}, force: true},
		{args: []string{"x", "--kind"}, err: "flag needs an argument"},
		{args: []string{"x", "--force=maybe"}, err: "invalid boolean value"},
		{args: []string{"x", "-h"}, err: "help requested"},
		{args: []string{"--help"}, err: "help requested"},
	} {
		fs := flag.NewFlagSet("t", flag.ContinueOnError)
		kind := fs.String("kind", "", "")
		force := fs.Bool("force", false, "")
		pos, err := parseArgs(fs, tc.args, "ape t")
		if tc.err != "" {
			if err == nil || !strings.Contains(err.Error(), tc.err) || !strings.Contains(err.Error(), "usage: ape t") {
				t.Errorf("%q: err = %v; want containing %q and the usage", tc.args, err, tc.err)
			}
			continue
		}
		if err != nil || !slices.Equal(pos, tc.pos) || *kind != tc.kind || *force != tc.force {
			t.Errorf("%q: pos, kind, force, err = %q, %q, %v, %v; want %q, %q, %v", tc.args, pos, *kind, *force, err,
				tc.pos, tc.kind, tc.force)
		}
	}
}
