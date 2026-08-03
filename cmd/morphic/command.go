package main

import (
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
	// flagSet returns a fresh FlagSet with this command's flags defined. Help
	// rendering and argument parsing share it, so the flag table printed by
	// PrintDefaults cannot drift from what Parse accepts.
	flagSet func() *flag.FlagSet
	// run executes the command with the subcommand word already removed from
	// args, and returns the process exit code.
	run func(args []string, stdout, stderr io.Writer) int
}

// commands is the subcommand table. Adding a subcommand means adding one entry.
var commands = []command{compileCommand}

// lookup resolves a subcommand by name.
func lookup(name string) (command, bool) {
	for _, c := range commands {
		if c.name == name {
			return c, true
		}
	}
	return command{}, false
}
