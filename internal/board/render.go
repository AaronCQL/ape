package board

import (
	"fmt"
	"strings"
	"unicode/utf8"

	"github.com/AaronCQL/ape/internal/task"
)

// width is the most columns (runes) an Ask or DONE line takes; longer ones are cut, ending "…".
const width = 120

var colNames = [...]string{Owner: "OWNER", Doing: "DOING", New: "NEW", Blocked: "BLOCKED", Ready: "READY"}

// Render returns the board text exactly as SPEC.md specifies, ending in a newline.
func Render(b Board) string {
	var n [len(colNames)]int
	for _, r := range b.Rows {
		n[r.Col]++
	}
	var sb strings.Builder
	fmt.Fprintf(&sb, "ape: %d tasks · %d owner · %d doing · %d new · %d blocked · %d ready · %d done 24h\n",
		len(b.Rows), n[Owner], n[Doing], n[New], n[Blocked], n[Ready], len(b.Done))

	for col, name := range colNames {
		if n[col] > 0 {
			sb.WriteString(name + "\n")
		}
		for _, r := range b.Rows {
			if int(r.Col) != col {
				continue
			}
			pri := "  "
			if p := get(r.Task, "pri"); p != "" {
				pri = "p" + p
			}
			fmt.Fprintf(&sb, "  %-4s %-5s %s  %s", task.ID(r.Task.Num), get(r.Task, "kind"), pri, r.Task.Title)
			if len(r.Marks) > 0 {
				sb.WriteString("   [" + strings.Join(r.Marks, "; ") + "]")
			}
			sb.WriteString("\n")
			if r.Col == Owner && r.Task.Ask != "" {
				sb.WriteString(fit("       ? "+r.Task.Ask) + "\n")
			}
		}
	}
	section(&sb, "DONE 24h", b.Done, true)
	section(&sb, "CHECK", b.Check, false) // never cut: its lines hold paths and commands to run
	return sb.String()
}

func section(sb *strings.Builder, name string, lines []string, cut bool) {
	if len(lines) == 0 {
		return
	}
	sb.WriteString(name + "\n")
	for _, l := range lines {
		if l = "  " + l; cut {
			l = fit(l)
		}
		sb.WriteString(l + "\n")
	}
}

// fit cuts line to width runes, the last one "…", dropping spaces before it.
func fit(line string) string {
	if utf8.RuneCountInString(line) <= width {
		return line
	}
	r := []rune(line)[:width-1]
	return strings.TrimRight(string(r), " ") + "…"
}
