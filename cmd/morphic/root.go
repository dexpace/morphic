package main

import (
	"runtime/debug"

	"charm.land/fang/v2"
	"github.com/spf13/cobra"
)

// rootDescription is the one-paragraph summary shown in root help.
const rootDescription = "morphic lowers an API spec into Morphic IR."

// newRootCmd builds the morphic command tree. It is a function, not a
// package-level var, so each run gets a tree of its own with no shared state:
// the flags a command binds are written into variables its closure holds.
func newRootCmd() *cobra.Command {
	root := &cobra.Command{
		Use:   "morphic",
		Short: "lower an API spec into Morphic IR",
		Long:  rootDescription,
	}
	root.SetVersionTemplate(versionTemplate)
	root.SetHelpCommand(newHelpCmd())
	root.SetFlagErrorFunc(asMisuse)
	root.AddCommand(newCompileCmd(), newValidateCmd())

	// cobra resolves the command before it defines -h, and an undefined short
	// flag is assumed to take a value: "morphic -h compile" would read compile
	// as -h's argument and show root help. Defining it first makes -h a switch.
	root.InitDefaultHelpFlag()
	return root
}

// fangOptions returns how fang decorates the tree: morphic reports its own
// errors, in the shape errorHandler gives them, and names its own version.
func fangOptions() []fang.Option {
	info, _ := debug.ReadBuildInfo()
	return []fang.Option{
		fang.WithErrorHandler(errorHandler),
		fang.WithVersion(moduleVersion(info)),
	}
}

// newHelpCmd builds the help command. cobra's own answers an unknown topic
// with exit 0, which would make "morphic help compil" look like success to a
// script, so this one reports it as misuse.
func newHelpCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "help [command]",
		Short: "show help for a command",
		Args:  helpArgs,
		RunE:  runHelp,

		ValidArgsFunction: completeCommand,
	}
}

// completeCommand offers the commands help can describe, for the one topic it
// takes.
func completeCommand(cmd *cobra.Command, args []string, _ string) ([]cobra.Completion, cobra.ShellCompDirective) {
	if len(args) > 0 {
		return nil, cobra.ShellCompDirectiveNoFileComp
	}

	var names []cobra.Completion
	for _, c := range cmd.Root().Commands() {
		if c.IsAvailableCommand() {
			names = append(names, cobra.CompletionWithDesc(c.Name(), c.Short))
		}
	}
	return names, cobra.ShellCompDirectiveNoFileComp
}

// helpArgs allows help to name at most one command.
func helpArgs(cmd *cobra.Command, args []string) error {
	if len(args) > 1 {
		return misuse(cmd.Root(), "help accepts at most one command")
	}
	return nil
}

// runHelp prints the help of the command args names, or of morphic itself when
// it names none. Find reports the unknown name. The target's help flag is
// defined first, as cobra's own help command does, so that "help compile" lists
// the same flags "compile --help" does.
func runHelp(cmd *cobra.Command, args []string) error {
	target, _, err := cmd.Root().Find(args)
	if err != nil {
		return asMisuse(cmd.Root(), err)
	}
	target.InitDefaultHelpFlag()
	return target.Help()
}
