// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package main

import (
	"slices"

	"github.com/spf13/cobra"
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
		sub, at, ok := nextCommand(target, rest)
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
// reports, as how far a flag it does not know reaches is unknown.
func nextCommand(c *cobra.Command, args []string) (*cobra.Command, int, bool) {
	refusedAt := -1
	for at, word := range args {
		sub := subcommand(c, word)
		if sub == nil {
			continue
		}
		switch readBefore(sub, args[:at]) {
		case onlyFlags:
			return sub, at, true
		case unknownFlag:
			if refusedAt < 0 {
				refusedAt = at
			}
		case notCommand:
		}
	}
	if refusedAt < 0 {
		return nil, 0, false
	}
	return subcommand(c, args[refusedAt]), refusedAt, true
}

// subcommand returns c's subcommand named or aliased word, or nil.
func subcommand(c *cobra.Command, word string) *cobra.Command {
	for _, sub := range c.Commands() {
		if sub.Name() == word || sub.HasAlias(word) {
			return sub
		}
	}
	return nil
}

// verdict is how a command reads the arguments ahead of a word naming it.
type verdict int

const (
	onlyFlags   verdict = iota // flags and their values, the word none of them
	unknownFlag                // an argument the command's flags refuse
	notCommand                 // an operand, a "--", or a flag taking the word as its value
)

// readBefore returns how c, parsing before and then the word after it, reads
// them, stopping at the first argument its flags refuse.
func readBefore(c *cobra.Command, before []string) verdict {
	probe := probeFor(c)
	for i := 0; i < len(before); i++ {
		switch readAlone(probe, before[i]) {
		case wantsValue:
			if i+1 == len(before) {
				return notCommand // the word after before is this flag's value
			}
			i++
		case endsFlags:
			return notCommand
		case refused:
			return unknownFlag
		case standsAlone:
			if isOperand(before[i]) {
				return notCommand
			}
		}
	}
	return onlyFlags
}

// isOperand reports whether pflag reads tok, standing alone, as an operand
// rather than a flag: an empty argument, a lone dash, or one that does not
// start with a dash.
func isOperand(tok string) bool {
	return len(tok) < 2 || tok[0] != '-'
}
