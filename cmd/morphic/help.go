package main

import "io"

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
	for _, c := range commands {
		emitf(w, "  %-9s %s\n", c.name, c.summary)
	}
	emitf(w, "\nrun \"morphic help <command>\" for command details.\n")
}

// writeCommandHelp writes c's full help text to w: synopsis, description, and
// the flag table rendered from c's own FlagSet.
func writeCommandHelp(w io.Writer, c command) {
	emitf(w, "usage:\n  %s\n\n%s\n\nflags:\n", c.usage, c.description)
	fs := c.flagSet()
	fs.SetOutput(w)
	fs.PrintDefaults()
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
func filterHelpTokens(args []string) []string {
	names := make([]string, 0, len(args))
	for _, arg := range args {
		if isHelpFlag(arg) {
			continue
		}
		names = append(names, arg)
	}
	return names
}

// runHelp implements the `help` subcommand: bare `help` prints root help,
// `help <command>` prints that command's help, and anything else is misuse.
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
		emitf(stderr, "morphic: help accepts at most one command\n")
		writeRootHelp(stderr)
		return 2
	}

	c, ok := lookup(names[0])
	if !ok {
		emitf(stderr, "morphic: unknown command %q\n", names[0])
		writeRootHelp(stderr)
		return 2
	}

	writeCommandHelp(stdout, c)
	return 0
}
