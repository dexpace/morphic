package main

import (
	"context"
	"fmt"
	"io"
	"slices"

	"github.com/spf13/cobra"
	"github.com/spf13/pflag"

	"github.com/dexpace/morphic/engine"
	"github.com/dexpace/morphic/engine/output"
	"github.com/dexpace/morphic/engine/report"
	"github.com/dexpace/morphic/ir"
)

// newEngine constructs the pipeline engine. It is a package var so tests can
// inject a construction failure — the real engine.New registers exactly one
// compiler into a fresh registry, so neither of Register's failure modes (no
// formats reported, a format collision) can actually fire — and a stub engine
// that returns a nil Document.
var newEngine = func() (*engine.Engine, error) { return engine.New() }

// compileLong is compile's description. cobra prints it as written, so it is
// wrapped by hand.
const compileLong = `Lower an API spec into Morphic IR JSON on stdout, and write diagnostics to
stderr. Output to stdout is indented for reading; a file written with --output
is compact unless --pretty asks for the indented form.

--explain reports what compiling produced at one source coordinate — the type
node interned there, the coordinates interned beneath it, and the diagnostics
stamped at it — instead of writing the document. The coordinate is a JSON
Pointer, so '' is the whole document. It writes no file, so it cannot be
combined with --output.

--disable-pass turns off one IR pass by name, and may be repeated. A name no
pass carries is refused (exit 2) with the names that exist. --skip-validate is
--disable-pass validate under its older name.

--verify holds the document the compiler returns, and each pass's, to the IR's
structural invariants, and each pass to leaving its input as it found it. The
first stage to break one ends the run (exit 2) with every invariant it broke.
It is for developing a compiler or a pass, and slows the run.

--opt passes a setting to the compiler the spec selects, which names and
validates its own options; morphic itself knows none of them. The OpenAPI
compiler's are listed in the README.

A -- argument ends flag parsing: every argument after it is an operand, even one
that begins with a dash, which is how a spec file named like a flag is passed.

Exit status is 0 when nothing reaches --fail-on, 1 when something does or the
spec lowered to no document at all, and 2 for a misuse, an I/O error, a
compiler or pass that broke its contract, or a run a signal stopped.

SIGINT, SIGTERM or SIGHUP stops the run. A document --output was writing is
abandoned and its temporary file removed, so the file keeps its previous
content unless the document was already being moved into place. Another
signal while the run is stopping changes nothing.`

// compileExample is compile's examples, indented as cobra's help indents a
// section.
const compileExample = `  # IR JSON to stdout
  morphic compile openapi.yaml

  # ...or to a file
  morphic compile openapi.yaml -o api.ir.json

  # what compiling produced at one source coordinate
  morphic compile openapi.yaml --explain /components/schemas/Pet

  # pass settings to the compiler the spec selects
  morphic compile openapi.yaml --opt grouping=path-prefix --opt overlay=patch.yaml`

// newCompileCmd builds the compile subcommand.
func newCompileCmd() *cobra.Command {
	var opts compileOptions
	cmd := &cobra.Command{
		Use:     "compile <spec-file>",
		Short:   "lower an API spec into Morphic IR JSON",
		Long:    withFormats(compileLong),
		Example: compileExample,
		Args:    compileArgs(&opts),
		RunE: func(cmd *cobra.Command, args []string) error {
			return compileSpec(cmd, args[0], opts)
		},
		ValidArgsFunction: completeSpecFile,
	}

	flags := cmd.Flags()
	bindSpecFlags(flags, &opts.specOptions)
	flags.StringVarP(&opts.outPath, "output", "o", "", "write IR JSON to this file instead of stdout")
	flags.Lookup("output").Annotations = map[string][]string{cobra.BashCompFilenameExt: {"json"}}
	flags.Var(&opts.explain, "explain",
		"report what compiling produced at this pointer instead of the IR; '' is the whole document")
	flags.BoolVar(&opts.pretty, "pretty", false,
		"indent the IR JSON --output writes; stdout is indented either way")
	return cmd
}

// specOptions holds what the shared pipeline runner reads, parsed by the flags
// every spec-taking command defines. validate is compile's pipeline with the
// document dropped, so the two configure that pipeline the same way: a spec that
// needs an option to compile needs it to be validated as it will be compiled.
type specOptions struct {
	failOn       failOnFlag
	skipValidate bool
	verify       bool
	disable      passFlag
	settings     settingFlag
}

// disabledPasses is the pass names the flags turn off: --disable-pass's, and
// validate when --skip-validate, its alias, was given. Names pass through as
// typed, repeats included: which exist is the engine's to say, and it reads a
// repeat as one.
func (o specOptions) disabledPasses() []string {
	if !o.skipValidate {
		return o.disable
	}
	return slices.Concat(o.disable, []string{engine.ValidatePass})
}

// compileOptions holds the values compile's flags parse into.
type compileOptions struct {
	specOptions
	outPath string
	explain pointerFlag
	pretty  bool
}

// bindSpecFlags registers the flags every spec-taking command shares onto fs.
// Sharing the registration is what keeps their spellings, defaults and help
// text identical across commands rather than identical-looking.
func bindSpecFlags(fs *pflag.FlagSet, opts *specOptions) {
	opts.settings = settingFlag{}
	opts.failOn = failOnFlag(engine.FailOnError)

	fs.Var(&opts.failOn, "fail-on",
		"exit 1 on a diagnostic at or above this severity: error, warning or info; none exits 1 only when the spec lowered to no document")
	fs.BoolVar(&opts.skipValidate, "skip-validate", false,
		"alias for --disable-pass validate")
	fs.Var(&opts.disable, "disable-pass",
		"turn off the IR pass with this name (repeatable)")
	fs.BoolVar(&opts.verify, "verify", false,
		"check the IR each stage returns against the IR's invariants, and that no pass changes its input; for developing a stage, and slow")
	fs.Var(opts.settings, "opt",
		"set one key=value option on the compiler the spec selects (repeatable)")
}

// completeSpecFile offers spec files, by extension, for the one operand compile
// and validate take, and nothing for a second.
func completeSpecFile(_ *cobra.Command, args []string, _ string) ([]cobra.Completion, cobra.ShellCompDirective) {
	if len(args) > 0 {
		return nil, cobra.ShellCompDirectiveNoFileComp
	}
	return []cobra.Completion{"yaml", "yml", "json"}, cobra.ShellCompDirectiveFilterFileExt
}

// compileArgs checks compile's arguments: what every spec-taking command takes,
// then that --explain is not asked to write a file. --explain reports instead of
// writing the document, so --output would be accepted and ignored, with an exit
// status that says it worked.
func compileArgs(opts *compileOptions) cobra.PositionalArgs {
	return func(cmd *cobra.Command, args []string) error {
		if err := specArgs(cmd, args); err != nil {
			return err
		}
		if opts.explain.set && opts.outPath != "" {
			return misuse(cmd, "--explain writes no document, so --output has nothing to write")
		}
		return nil
	}
}

// specArgs checks what every spec-taking command takes once its flags are
// parsed: exactly one spec file. The command's own name is in the message, so
// a misuse names what was typed. No flag's value is checked here: --fail-on
// refuses a bad one as it is set, and which pass names exist is the engine's
// to say.
func specArgs(cmd *cobra.Command, args []string) error {
	if len(args) != 1 {
		return misuse(cmd, cmd.Name()+" requires exactly one spec file")
	}
	return nil
}

// runPipeline runs the engine over specPath under ctx and renders every
// diagnostic the run produced to stderr.
//
// A non-nil err ends the run with its exit code: 2 when the run could not
// happen, the engine not built, failed or cancelled, and a refused diagnostics
// write as writeFailureExit says, so no document follows findings nobody saw.
// Otherwise code is the exit code the engine's verdict calls for, and res
// holds the document, or is nil when the spec lowered to none.
func runPipeline(
	ctx context.Context, specPath string, opts specOptions, stderr io.Writer,
) (res *engine.Result, code int, err error) {
	eng, err := newEngine()
	if err != nil {
		return nil, 0, failure(err)
	}

	res, err = eng.Run(ctx, specPath, engine.RunOptions{
		CompilerOptions: opts.settings,
		DisablePasses:   opts.disabledPasses(),
		FailOn:          engine.FailOn(opts.failOn),
		Verify:          opts.verify,
	})
	if err != nil {
		return nil, 0, failure(err)
	}
	// The engine checks ctx only between its stages, so a run cancelled during
	// its last one comes back whole. Nothing of it is written.
	if err := ctx.Err(); err != nil {
		return nil, 0, failure(err)
	}

	if res.Failed {
		code = exitFindings
	}
	if err := report.WriteDiagnostics(stderr, res.Diagnostics, res.Sources); err != nil {
		return nil, 0, &exitError{code: writeFailureExit(code), err: fmt.Errorf("write diagnostics: %w", err)}
	}
	if res.Document == nil {
		return nil, code, nil
	}
	return res, code, nil
}

// compileSpec runs the pipeline over specPath, writes the IR document and its
// diagnostics, and returns the error that ends the run with its exit code.
func compileSpec(cmd *cobra.Command, specPath string, opts compileOptions) error {
	ctx := cmd.Context()
	res, code, err := runPipeline(ctx, specPath, opts.specOptions, cmd.ErrOrStderr())
	if err != nil {
		return err
	}
	if res == nil {
		return exitWith(code)
	}

	if opts.explain.set {
		e := report.Explain(res.Document, res.Diagnostics, res.Sources, opts.explain.pointer)
		if err := report.WriteExplanation(cmd.OutOrStdout(), e); err != nil {
			return &exitError{code: writeFailureExit(code), err: fmt.Errorf("write explanation: %w", err)}
		}
		return exitWith(code)
	}
	if err := writeCompiled(ctx, opts, cmd.OutOrStdout(), res.Document); err != nil {
		return &exitError{code: writeFailureExit(code), err: err}
	}
	return exitWith(code)
}

// writeFailureExit returns the exit code for a run whose diagnostics earned
// code and whose output then could not be written.
//
// A failed write does not overwrite a non-zero code. Whether a destination can
// be written is a property of the destination (/dev/null and a read-only
// directory both refuse output.ReplaceDocument's temporary file), not of the
// spec, so the verdict on the spec keeps the exit code and 2 is left for runs
// that failed for a reason outside the spec.
func writeFailureExit(code int) int {
	if code != 0 {
		return code
	}
	return exitFailure
}

// writeCompiled writes doc's IR JSON to opts.outPath, or to stdout when it is
// empty, until ctx is done. Stdout is indented because a person is reading it;
// a file is compact unless --pretty asks for the indented form.
func writeCompiled(ctx context.Context, opts compileOptions, stdout io.Writer, doc *ir.Document) error {
	if opts.outPath == "" {
		return output.WriteDocument(ctx, stdout, doc, output.Indented)
	}
	layout := output.Compact
	if opts.pretty {
		layout = output.Indented
	}
	return output.ReplaceDocument(ctx, opts.outPath, doc, layout)
}
