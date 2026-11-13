// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package main

import (
	"errors"
	"fmt"
	"io"
	"strings"

	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
)

// misreadDashes refuses a flag spelled with one dash that pflag would misread.
// pflag reads a long flag so spelled as shorthands, so on compile "-opt=k=v" is
// "-o pt=k=v": the option dropped, the document written to a file named
// "pt=k=v", and exit 0. It also drops, unread, what starts "test." where a
// shorthand could, which it keeps for go test's own flags.
//
// It checks the arguments of the command cobra will run, read as pflag will read
// them: a flag's value is not a flag, and nothing after a "--" is one. A command
// not found here is cobra's to resolve.
func misreadDashes(root *cobra.Command, args []string) error {
	target, rest, found := findCommand(root, args)
	if !found {
		return nil // cobra resolves the command again, and reports what it cannot find
	}

	fs := target.Flags()
	probe := probeFor(target)
	for i := 0; i < len(rest); i++ {
		name, oneDash := oneDashName(rest[i])
		if oneDash && fs.Lookup(name) != nil {
			return misuse(target, fmt.Sprintf("long flags take two dashes: write %q, not %q", "-"+rest[i], rest[i]))
		}
		if oneDash && dropsAsGoTestFlag(fs, rest[i][1:]) {
			return misuse(target, fmt.Sprintf("unknown shorthand flag: 't' in %s", rest[i]))
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

// probeFor returns a flag set holding c's flags, help among them, for readAlone
// to parse with. cobra defines the help flag only as it parses; defining it
// here first lets "-h" be read as the switch it is, and "-help" as the flag it
// names.
func probeFor(c *cobra.Command) *pflag.FlagSet {
	c.InitDefaultHelpFlag()
	probe := pflag.NewFlagSet(c.Name(), pflag.ContinueOnError)
	probe.SetOutput(io.Discard)
	probe.AddFlagSet(c.Flags())
	return probe
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

// goTestPrefix starts what pflag drops where a shorthand could start: go test's
// own flags, such as -test.v, which it leaves for the flag package to read.
const goTestPrefix = "test."

// dropsAsGoTestFlag reports whether pflag, reading shorts as shorthands of fs,
// as it reads what follows one dash, drops what is left of them: goTestPrefix
// starts them, or follows a run of shorthands that take no value. A shorthand
// that takes one reads the rest as its value, so "-otest.json" is -o's file.
func dropsAsGoTestFlag(fs *pflag.FlagSet, shorts string) bool {
	for ; shorts != ""; shorts = shorts[1:] {
		if strings.HasPrefix(shorts, goTestPrefix) {
			return true
		}
		flag := fs.ShorthandLookup(shorts[:1])
		if flag == nil || flag.NoOptDefVal == "" {
			return false
		}
	}
	return false
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
// nor the command's own flag set change. Once a "--" is read, probe reports
// every later argument as ending the flags, so a reading stops there.
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
