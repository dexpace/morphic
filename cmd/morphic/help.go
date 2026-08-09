package main

import (
	"fmt"
	"io"
)

// rootDescription is the one-paragraph summary shown in root help.
const rootDescription = "morphic lowers an API spec into Morphic IR."

// isHelpFlag reports whether arg asks for help rather than naming work to do.
func isHelpFlag(arg string) bool {
	return arg == "-h" || arg == "--help" || arg == "-help"
}

// writeRootHelp writes the top-level help text to w: the synopsis, what morphic
// is, the command list built from the command table, and how to go deeper.
func writeRootHelp(w io.Writer) {
	emitf(w, "usage:\n  morphic <command> [flags]\n\n%s\n\ncommands:\n", rootDescription)
	for _, c := range commands() {
		emitf(w, "  %-9s %s\n", c.name, c.summary)
	}
	emitf(w, "\nrun \"morphic help <command>\" for command details.\n")
}

// writeCommandHelp writes c's full help text to w: synopsis, description, and
// c's own flag table.
func writeCommandHelp(w io.Writer, c command) {
	emitf(w, "usage:\n  %s\n\n%s\n\nflags:\n", c.usage, c.description)
	c.printFlags(w)
}

// rootUsageError reports a misuse of morphic itself: one reason line, the root
// help, exit 2. It never touches stdout.
//
// reason is a finished string, not a format — see compileUsageError for why.
func rootUsageError(stderr io.Writer, reason string) int {
	emitf(stderr, "morphic: %s\n", reason)
	writeRootHelp(stderr)
	return 2
}

// writeCommandUsage writes the short pointer shown after a misuse of c: the
// synopsis and where the details live, never the whole flag table.
func writeCommandUsage(w io.Writer, c command) {
	emitf(w, "usage:\n  %s\nrun \"morphic help %s\" for details.\n", c.usage, c.name)
}

// filterHelpTokens returns args with every help-flag token removed. help
// takes only a bare positional command name and defines no flags of its own,
// so a help-flag token can never be a legitimate value for it — stripping
// these tokens first lets the argument-count and lookup logic in runHelp run
// on whatever command name, if any, remains. This filtering approach is safe
// here specifically because help has no flags; runCompile must keep detecting
// help via errors.Is(err, flag.ErrHelp) instead of pre-scanning argv.
//
// A "--" stops the filtering, since past it a help-flag token is a command name
// like any other: "morphic help -- --help" reports an unknown command called
// "--help" rather than dropping the token and being left with the marker.
func filterHelpTokens(args []string) []string {
	names := make([]string, 0, len(args))
	for i, arg := range args {
		if arg == flagTerminator {
			return append(names, args[i+1:]...)
		}
		if isHelpFlag(arg) {
			continue
		}
		names = append(names, arg)
	}
	return names
}

// runHelp implements every root-level request for help, whether spelled as the
// `help` subcommand or as a leading help flag: no command name prints root
// help, one name prints that command's help, and anything else is misuse.
// Help-flag tokens (`-h`, `--help`, `-help`) are stripped from args before the
// argument-count check runs, so `help compile --help` prints compile help
// rather than being conflated with `help --help`, and `help bogus --help` is
// still rejected as misuse rather than silently returning root help — a help
// flag no longer masks a mistyped or extra command name.
func runHelp(args []string, stdout, stderr io.Writer) int {
	names := filterHelpTokens(args)
	if len(names) == 0 {
		writeRootHelp(stdout)
		return 0
	}
	if len(names) > 1 {
		return rootUsageError(stderr, "help accepts at most one command")
	}

	c, ok := lookup(names[0])
	if !ok {
		return rootUsageError(stderr, fmt.Sprintf("unknown command %q", names[0]))
	}

	writeCommandHelp(stdout, c)
	return 0
}
