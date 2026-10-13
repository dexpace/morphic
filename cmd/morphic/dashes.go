package main

import (
	"errors"
	"fmt"
	"io"
	"strings"

	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
)

// singleDashLongFlags refuses a long flag spelled with one dash. Go's flag
// package, which morphic parsed with before cobra, accepted that spelling; pflag
// reads it as shorthands, so on compile "-opt=k=v" is "-o pt=k=v": the option
// dropped, the document written to a file named "pt=k=v", and exit 0.
//
// It checks the arguments of the command cobra will run, read as pflag will read
// them: a flag's value is not a flag, and nothing after a "--" is one. A command
// not found here is cobra's to resolve; help and completion join the tree only
// as it executes.
func singleDashLongFlags(root *cobra.Command, args []string) error {
	target, rest, found := findCommand(root, args)
	if !found {
		return nil // cobra resolves the command again, and reports what it cannot find
	}
	// cobra defines the help flag before it parses; defining it here first lets
	// "-help" be read as the flag it names.
	target.InitDefaultHelpFlag()

	fs := target.Flags()
	probe := pflag.NewFlagSet(target.Name(), pflag.ContinueOnError)
	probe.SetOutput(io.Discard)
	probe.AddFlagSet(fs)

	for i := 0; i < len(rest); i++ {
		if name, ok := oneDashName(rest[i]); ok && fs.Lookup(name) != nil {
			return misuse(target, fmt.Sprintf("long flags take two dashes: write %q, not %q", "-"+rest[i], rest[i]))
		}
		switch readAlone(probe, rest[i]) {
		case wantsValue:
			i++
		case endsFlags, refused:
			return nil
		case standsAlone:
		}
	}
	return nil
}

// findCommand returns the command cobra will run for args and the arguments
// left for it, or false and nothing when cobra finds none. Cobra reports that
// itself when it runs, so only whether a command was found matters here.
func findCommand(root *cobra.Command, args []string) (*cobra.Command, []string, bool) {
	target, rest, err := root.Find(args)
	if err != nil {
		return nil, nil, false
	}
	return target, rest, true
}

// oneDashName returns the name tok spells after a single dash, up to any "=",
// or false when tok does not start with exactly one dash.
func oneDashName(tok string) (string, bool) {
	if !strings.HasPrefix(tok, "-") || strings.HasPrefix(tok, "--") {
		return "", false
	}
	name, _, _ := strings.Cut(tok[1:], "=")
	return name, true
}

// reading is how pflag reads one argument on its own.
type reading int

const (
	standsAlone reading = iota // an operand, or flags with every value they need
	wantsValue                 // a flag whose value is the next argument
	endsFlags                  // the "--" after which every argument is an operand
	refused                    // an argument the parse fails on
)

// readAlone asks pflag how it reads tok, so that which argument is a flag's
// value is pflag's own answer rather than a copy of its grammar. probe holds
// the command's flags and is parsed with values discarded, so neither the flags
// nor the command's own flag set change.
func readAlone(probe *pflag.FlagSet, tok string) reading {
	err := probe.ParseAll([]string{tok}, func(*pflag.Flag, string) error { return nil })

	if _, ok := errors.AsType[*pflag.ValueRequiredError](err); ok {
		return wantsValue
	}
	switch {
	case err != nil:
		return refused
	case probe.ArgsLenAtDash() >= 0:
		return endsFlags
	default:
		return standsAlone
	}
}
