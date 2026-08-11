package main

import (
	"errors"
	"flag"
	"io"
)

// command describes one morphic subcommand: how it is invoked, how it is
// documented, and how it runs. Dispatch and help rendering both read this
// table, so a new subcommand becomes reachable and documented in one edit.
type command struct {
	// name is the word typed after "morphic".
	name string
	// summary is the one-line description shown in the root command list.
	summary string
	// usage is the invocation synopsis, e.g. "morphic compile <spec-file> [flags]".
	usage string
	// description is the paragraph shown above the flag table in command help.
	description string
	// printFlags writes this command's flag table to w. It renders from the same
	// constructor that parsing binds, so the documented flags cannot drift from
	// the flags Parse accepts.
	//
	// It hands out the rendered text rather than the *flag.FlagSet because help
	// needs nothing more: a FlagSet in a caller's hands can be Parsed, and that
	// writes into an options struct nobody is holding, losing the values with no
	// error to notice.
	printFlags func(w io.Writer)
	// bind parses args — the subcommand word already removed — into this
	// command's own options, and returns the work they ask for. Anything wrong
	// with args comes back as an error, a help request included, since dispatch
	// answers those for every command in one place.
	bind func(args []string) (work, error)
}

// work is a bound command: its arguments are parsed and its options are held by
// the closure, so all that is left is to do the job and report an exit code.
type work func(stdout, stderr io.Writer) int

// commands returns the subcommand table. Adding a subcommand means adding one
// entry.
//
// It is a function rather than a var to keep the table out of the
// initialization graph. A command's own code reaching back into the table is
// ordinary — writeRootHelp already does, and any error path that wants to list
// the commands will too — and as a var the first such reference from a
// constructor is an initialization cycle that stops the package compiling.
// Functions may freely refer to each other.
func commands() []command { return []command{newCompileCommand(), newValidateCommand()} }

// dispatch runs c against args and returns the process exit code, answering the
// two things every command answers the same way: a help request prints c's help
// to stdout and exits 0, and any other misuse prints one reason line and c's
// usage pointer to stderr and exits 2.
//
// Both belong here rather than in c's own body because both are replies to
// arguments rather than work done, and a body that owned them could get them
// wrong in silence. The flag package reports a help request as an ordinary
// error from Parse, so a command that does the obvious thing with that error
// answers -h with "flag: help requested" on stderr and exit 2 — a divergence
// from its neighbour that no golden records and no per-command test asks about.
func dispatch(c command, args []string, stdout, stderr io.Writer) int {
	todo, err := c.bind(args)
	if errors.Is(err, flag.ErrHelp) {
		writeCommandHelp(stdout, c)
		return 0
	}
	if err != nil {
		return commandUsageError(stderr, c, err.Error())
	}
	return todo(stdout, stderr)
}

// lookup resolves a subcommand by name.
func lookup(name string) (command, bool) {
	for _, c := range commands() {
		if c.name == name {
			return c, true
		}
	}
	return command{}, false
}
