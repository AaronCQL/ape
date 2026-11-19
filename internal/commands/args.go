package commands

import (
	"flag"
	"io"
	"strings"
)

// parseArgs parses args with fs and returns the positionals. Flags may come after positionals
// (`ape new x --kind bug`); "--" ends flags. Only an argument naming one of fs's flags (or -h, --help) is a
// flag, so a summary like "-20% p95" is a positional. A bad flag value is an ExitUsage error naming usage.
func parseArgs(fs *flag.FlagSet, args []string, usage string) ([]string, error) {
	fs.SetOutput(io.Discard)
	var flags, pos []string
	for i := 0; i < len(args); i++ {
		a := args[i]
		f, isFlag := lookupFlag(fs, a)
		switch {
		case a == "--":
			pos = append(pos, args[i+1:]...)
			i = len(args)
		case !isFlag:
			pos = append(pos, a)
		default:
			flags = append(flags, a)
			// A value flag (-k v, not -k=v) takes the next arg as its value, wherever it stands.
			if f != nil && !strings.Contains(a, "=") && !isBoolFlag(f) && i+1 < len(args) {
				i++
				flags = append(flags, args[i])
			}
		}
	}
	if err := fs.Parse(flags); err != nil {
		return nil, usageErr(usage, "%v", err)
	}
	return append(pos, fs.Args()...), nil
}

// lookupFlag reports whether a is a flag: -name, --name, -name=v or --name=v for one of fs's flags, or -h/--help
// (f is nil for those).
func lookupFlag(fs *flag.FlagSet, a string) (f *flag.Flag, ok bool) {
	if a == "-h" || a == "--help" {
		return nil, true
	}
	name, found := strings.CutPrefix(a, "--")
	if !found {
		if name, found = strings.CutPrefix(a, "-"); !found {
			return nil, false
		}
	}
	name, _, _ = strings.Cut(name, "=")
	f = fs.Lookup(name)
	return f, f != nil
}

func isBoolFlag(f *flag.Flag) bool {
	b, ok := f.Value.(interface{ IsBoolFlag() bool })
	return ok && b.IsBoolFlag()
}

// usageErr is an ExitUsage error: what's wrong, then the right form.
func usageErr(usage, format string, a ...any) error {
	return fail(ExitUsage, format+"; usage: "+strings.ReplaceAll(usage, "%", "%%"), a...)
}

// oneLine collapses whitespace (newlines included) to single spaces, as a header value or commit subject needs.
func oneLine(s string) string { return strings.Join(strings.Fields(s), " ") }
