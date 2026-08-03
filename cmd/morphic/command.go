package main

import "io"

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
	// run executes the command with the subcommand word already removed from
	// args, and returns the process exit code.
	run func(args []string, stdout, stderr io.Writer) int
}

// commands returns the subcommand table. Adding a subcommand means adding one
// entry.
//
// It is a function rather than a var so that a command's own code may reach
// back into the table — writeRootHelp already does, and any error path that
// wants to list the commands will too. As a var it sits in the initialization
// graph, and the first such reference is an initialization cycle
// (commands → newCompileCommand → runCompile → writeRootHelp → commands) that
// stops the package compiling. Functions may freely refer to each other.
func commands() []command { return []command{newCompileCommand()} }

// lookup resolves a subcommand by name.
func lookup(name string) (command, bool) {
	for _, c := range commands() {
		if c.name == name {
			return c, true
		}
	}
	return command{}, false
}
