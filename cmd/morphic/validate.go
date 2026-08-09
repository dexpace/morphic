package main

import (
	"flag"
	"io"
)

// newValidateCommand builds validate's command-table entry. It is a function,
// not a package-level var — see commands.
func newValidateCommand() command {
	return command{
		name:    "validate",
		summary: "check an API spec and report its diagnostics, writing no IR",
		usage:   "morphic validate <spec-file> [flags]",
		description: "Check an API spec (OpenAPI 3.x) and write its diagnostics to stderr, writing\n" +
			"no IR anywhere. validate is compile's pipeline with the document dropped\n" +
			"instead of marshalled, for a gate that wants the diagnostics and the exit\n" +
			"code and would throw the document away.\n\n" +
			"The diagnostics and the exit code are compile's: 0 when nothing reaches\n" +
			"--fail-on, 1 when something does or the spec lowered to no document at all,\n" +
			"2 for a misuse or an I/O error.\n\n" +
			"--skip-validate names the referential-integrity pass, not this command: it\n" +
			"drops that pass's diagnostics and keeps the compiler's.",
		printFlags: func(w io.Writer) {
			fs, _ := newValidateFlags()
			fs.SetOutput(w)
			fs.PrintDefaults()
		},
		bind: bindValidate,
	}
}

// newValidateFlags returns validate's FlagSet and the options its flags write
// into. It defines the flags every spec-taking command shares and nothing else:
// -o and --explain are both ways of emitting a document, and validate emits
// none. Directing Output to io.Discard silences the flag package the same way
// compile does — see newCompileFlags.
func newValidateFlags() (*flag.FlagSet, *specOptions) {
	fs := flag.NewFlagSet("validate", flag.ContinueOnError)
	fs.SetOutput(io.Discard)

	var opts specOptions
	bindSpecFlags(fs, &opts)

	return fs, &opts
}

// bindValidate parses validate's arguments and returns the check they ask for.
func bindValidate(args []string) (work, error) {
	fs, opts := newValidateFlags()

	specPath, err := bindSpec(fs, "validate", args, opts)
	if err != nil {
		return nil, err
	}

	return func(_, stderr io.Writer) int {
		return validateSpec(specPath, *opts, stderr)
	}, nil
}

// validateSpec runs the pipeline over specPath for its diagnostics alone and
// returns the process exit code.
//
// It takes no stdout because it has nothing to put there: dropping the document
// is the whole of the difference from compile, so a caller redirecting stdout
// gets an empty file rather than a document it asked not to be given. Whether
// there was a document to drop changes nothing here — a spec that lowered to
// none already has its exit code — which is why runPipeline's ok is ignored.
func validateSpec(specPath string, opts specOptions, stderr io.Writer) int {
	_, code, _ := runPipeline(specPath, opts, stderr)
	return code
}
