// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package main

import (
	"slices"

	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
)

// commandFirst returns args with the words naming the command to run moved to
// the front, the rest as they were: at each level, the word nextCommand finds,
// read by the flags of the command it names as pflag will read them. cobra
// reads them its own way, which takes a flag it does not know to need a value,
// so "--pretty compile spec.yaml" hands --pretty the word "compile". A
// completion request is read the same way, but for the word being completed,
// which stays last.
func commandFirst(root *cobra.Command, args []string) []string {
	if len(args) > 1 && isCompletionRequest(args[0]) {
		last := len(args) - 1
		return slices.Concat(args[:1], commandWordsFirst(root, args[1:last]), args[last:])
	}
	return commandWordsFirst(root, args)
}

// commandWordsFirst is commandFirst for a command line that is not a completion
// request.
func commandWordsFirst(root *cobra.Command, args []string) []string {
	target, path, rest := root, []string(nil), args
	for range args { // each turn takes one argument as a command's word
		sub, at, ok := nextCommand(target, rest, readAlone)
		if !ok {
			break
		}
		target, path = sub, append(path, rest[at])
		rest = slices.Delete(slices.Clone(rest), at, at+1)
	}
	return slices.Concat(path, rest)
}

// isCompletionRequest reports whether word is the hidden command a completion
// script calls, whose arguments are the command line being completed.
func isCompletionRequest(word string) bool {
	return word == cobra.ShellCompRequestCmd || word == cobra.ShellCompNoDescRequestCmd
}

// nextCommand returns the subcommand of c that args name, and where, or false
// when none does: the first argument naming one whose flags take every
// argument before it as a flag or a flag's value. Failing that, it is the first
// naming one whose flags refuse an argument before it, which pflag then
// reports, as how far a flag it does not know reaches is unknown. read is
// readAlone, which a test counts: args are read once for each subcommand.
func nextCommand(c *cobra.Command, args []string, read func(*pflag.FlagSet, string) reading) (*cobra.Command, int, bool) {
	var found, refused *cobra.Command
	foundAt, refusedAt := len(args), len(args)
	for _, sub := range c.Commands() {
		at, v := firstWord(sub, args, read)
		switch {
		case v == onlyFlags && at < foundAt:
			found, foundAt = sub, at
		case v == unknownFlag && at < refusedAt:
			refused, refusedAt = sub, at
		}
	}
	if found != nil {
		return found, foundAt, true
	}
	return refused, refusedAt, refused != nil
}

// names reports whether word is c's name or one of its aliases.
func names(c *cobra.Command, word string) bool {
	return c.Name() == word || c.HasAlias(word)
}

// verdict is how a command reads the arguments ahead of a word naming it.
type verdict int

const (
	onlyFlags   verdict = iota // flags and their values, the word none of them
	unknownFlag                // an argument the command's flags refuse
	notCommand                 // no word naming it stands free of an operand, a "--" or a flag
)

// firstWord returns where c's word first stands in args, read by c's flags as
// pflag will read them, and how what comes before it reads. The reading is one
// pass, ending at the first argument that decides it: an operand or a "--",
// after which no word is c's, or a flag c refuses, after which the next word
// naming c is taken. A flag's value is never c's word.
func firstWord(c *cobra.Command, args []string, read func(*pflag.FlagSet, string) reading) (int, verdict) {
	probe := probeFor(c)
	for i := 0; i < len(args); i++ {
		if names(c, args[i]) {
			return i, onlyFlags
		}
		switch read(probe, args[i]) {
		case wantsValue:
			i++
		case endsFlags:
			return 0, notCommand
		case refused:
			return nextNaming(c, args, i+1)
		case standsAlone:
			if isOperand(args[i]) {
				return 0, notCommand
			}
		}
	}
	return 0, notCommand
}

// nextNaming returns where the first word naming c stands in args at or after
// from, read past a flag c refuses, or notCommand when none does.
func nextNaming(c *cobra.Command, args []string, from int) (int, verdict) {
	for i := from; i < len(args); i++ {
		if names(c, args[i]) {
			return i, unknownFlag
		}
	}
	return 0, notCommand
}

// isOperand reports whether pflag reads tok, standing alone, as an operand
// rather than a flag: an empty argument, a lone dash, or one that does not
// start with a dash.
func isOperand(tok string) bool {
	return len(tok) < 2 || tok[0] != '-'
}
