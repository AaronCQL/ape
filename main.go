// Command ape is the CLI of APE, Agentic Plaintext Engineering. See framework/_FRAMEWORK.md.
package main

import (
	"fmt"
	"os"

	"github.com/AaronCQL/ape/framework"
	"github.com/AaronCQL/ape/internal/commands"
	"github.com/AaronCQL/ape/internal/locate"
)

const usage = `ape: a task board + project memory for AI agents, in ape/ (markdown + git).
https://github.com/AaronCQL/ape
usage:
  ape                          board
  ape new <slug> [--kind k] [--from who@sha] [--title "…"]
  ape set <id> k=v...
  ape start <id>
  ape pause <id>
  ape done <id> "<summary>"
  ape drop <id> "<reason>" [--force]
  ape commit -m "<msg>" <paths…>
  ape init
  ape version
details: ape/_FRAMEWORK.md (ape init writes it)`

func main() { os.Exit(run(os.Args[1:])) }

func run(args []string) int {
	sub, rest := "", args
	if len(args) > 0 {
		sub, rest = args[0], args[1:]
	}
	cmds := map[string]func(commands.Env, []string) error{
		"": commands.Board, "new": commands.New, "set": commands.Set, "start": commands.Start,
		"pause": commands.Pause, "done": commands.Done, "drop": commands.Drop, "init": commands.Init,
		"commit": commands.Commit,
	}
	switch sub {
	case "-h", "--help", "help":
		fmt.Println(usage)
		return 0
	case "version", "--version":
		fmt.Println("ape", framework.Version)
		return 0
	}
	cmd, ok := cmds[sub]
	if !ok {
		return fail(fmt.Errorf("unknown command %q; run 'ape help'", sub))
	}
	cwd, err := os.Getwd()
	if err != nil {
		return fail(err)
	}
	env := commands.Env{Cwd: cwd, Out: os.Stdout}
	if sub != "init" {
		if env.ApeDir, env.MainRoot, err = locate.Find(cwd); err != nil {
			return fail(&commands.Error{Code: commands.ExitNotFound,
				Msg: "no ape/ found here or above; run 'ape init' in the repo root"})
		}
	}
	if err := cmd(env, rest); err != nil {
		return fail(err)
	}
	return 0
}

// fail prints err as one line on stderr and returns its exit code (commands.Report).
func fail(err error) int {
	msg, code := commands.Report(err)
	fmt.Fprintln(os.Stderr, "ape:", msg)
	return code
}
