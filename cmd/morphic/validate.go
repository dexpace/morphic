package main

import (
	"github.com/spf13/cobra"
)

// validateLong is validate's description, wrapped by hand — see compileLong.
const validateLong = `Check an API spec and write its diagnostics to stderr, writing no IR anywhere.
validate is compile's pipeline with the document dropped instead of marshalled,
for a gate that wants the diagnostics and the exit code and would throw the
document away.

The diagnostics and the exit code are compile's: 0 when nothing reaches
--fail-on, 1 when something does or the spec lowered to no document at all, 2
for a misuse or an I/O error.

--skip-validate and --disable-pass validate are interchangeable. Both name the
IR pass called validate, not this command: they drop that pass's diagnostics and
keep the compiler's.

--opt is compile's, and takes the same settings: a spec that needs one to
compile needs it here too, or what is checked is not what would be built.`

// validateExample is validate's examples — see compileExample.
const validateExample = `  # diagnostics and exit code only
  morphic validate openapi.yaml

  # a stricter gate: exit 1 on a warning too
  morphic validate openapi.yaml --fail-on warning`

// newValidateCmd builds the validate subcommand. It defines the flags every
// spec-taking command shares and nothing else: --output and --explain are both
// ways of emitting a document, and validate emits none.
func newValidateCmd() *cobra.Command {
	var opts specOptions
	cmd := &cobra.Command{
		Use:     "validate <spec-file>",
		Short:   "check an API spec and report its diagnostics, writing no IR",
		Long:    withFormats(validateLong),
		Example: validateExample,
		Args:    specArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			return validateSpec(cmd, args[0], opts)
		},
		ValidArgsFunction: completeSpecFile,
	}

	bindSpecFlags(cmd.Flags(), &opts)
	return cmd
}

// validateSpec runs the pipeline over specPath for its diagnostics alone and
// returns the error that ends the run with its exit code.
//
// It writes nothing to stdout because it has nothing to put there: dropping the
// document is the whole of the difference from compile, so a caller redirecting
// stdout gets an empty file rather than a document it asked not to be given.
// Whether there was a document to drop changes nothing here — a spec that
// lowered to none already has its exit code.
func validateSpec(cmd *cobra.Command, specPath string, opts specOptions) error {
	_, code, err := runPipeline(cmd.Context(), specPath, opts, cmd.ErrOrStderr())
	if err != nil {
		return err
	}
	return exitWith(code)
}
