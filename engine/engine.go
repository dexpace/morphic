package engine

import (
	"context"
	"errors"
	"fmt"
	"os"

	"github.com/dexpace/morphic/compilers"
	"github.com/dexpace/morphic/compilers/openapi"
	"github.com/dexpace/morphic/ir"
	"github.com/dexpace/morphic/pass"
)

// RunOptions configures a single pipeline run.
//
// Compiler options arrive by one of two channels. FormatOptions is the
// programmatic one: a value of the compiler's own options type, forwarded
// verbatim as compilers.Options.FormatOptions by a caller that imports the
// compiler. CompilerOptions is the textual one, for a caller — the CLI — that
// does not: the detected compiler decodes the settings into its own options
// type itself.
type RunOptions struct {
	FormatOptions   any               `json:"formatOptions,omitempty"`
	CompilerOptions map[string]string `json:"compilerOptions,omitempty"`
	SkipValidate    bool              `json:"skipValidate,omitempty"`
}

// errOptionChannels reports both option channels set at once. Which one wins
// would be a precedence rule no caller can see the effect of, and a run
// configured two ways is a mistake in the caller rather than a case to resolve.
var errOptionChannels = errors.New(
	"engine: set FormatOptions or CompilerOptions, not both")

// Result is the outcome of a pipeline run. A nil Document alongside diagnostics
// is a legal outcome and the shape every refusal takes — a source no compiler
// claims, or one a compiler declined to lower; the caller decides what is fatal.
//
// Diagnostics is the whole list for the run. When Document is non-nil it holds
// the same values, so a caller reading either channel sees every finding.
type Result struct {
	Document    *ir.Document           `json:"document,omitempty"`
	Diagnostics []ir.Diagnostic        `json:"diagnostics,omitempty"`
	Format      compilers.SourceFormat `json:"format"`
}

// Engine orchestrates the detect → compiler → passes pipeline over a registry
// of compilers.
type Engine struct {
	registry *compilers.Registry
}

// New composes the default engine: a registry with every built-in compiler
// registered. Future compilers are added here and only here.
func New() (*Engine, error) {
	return NewWith(openapi.New())
}

// NewWith registers the given compilers into a fresh registry and wraps it in
// an Engine, for tests and embedders that need a custom compiler set. A nil
// compiler and a register failure (a compiler reporting no formats, or two
// compilers claiming the same format) alike surface as a Go error rather than a
// panic, and the error names the argument position so a caller passing several
// compilers can tell which one was rejected. Calling NewWith with no compilers
// is legal: the resulting engine's Run always fails at detection, with nobody
// to ask and so nothing to report but that the format went unrecognized, which
// is the seam TestEngine_RunNothingRegistered relies on to reach that branch.
func NewWith(fronts ...compilers.Compiler) (*Engine, error) {
	reg := compilers.NewRegistry()
	for i, front := range fronts {
		if err := reg.Register(front); err != nil {
			return nil, fmt.Errorf("engine: register compiler %d: %w", i, err)
		}
	}
	return &Engine{registry: reg}, nil
}

// Run executes the pipeline for the spec at specPath: read the file, ask the
// registered compilers which of them recognizes it, dispatch to that one, and —
// unless disabled — append the validate pass's diagnostics.
//
// The Go error return is reserved for I/O and programmer errors: the file could
// not be read, or a compiler failed in a way its own contract calls an error.
// Everything wrong with the spec itself comes back as a diagnostic in the
// Result, a source no compiler can lower included. A caller that treats a Go
// error as "the pipeline was invoked wrongly" therefore stays correct, which is
// what lets the CLI keep its usage exit code for actual misuse.
func (e *Engine) Run(ctx context.Context, specPath string, opts RunOptions) (*Result, error) {
	data, err := os.ReadFile(specPath)
	if err != nil {
		return nil, fmt.Errorf("engine: read spec %q: %w", specPath, err)
	}
	source := compilers.Source{Path: specPath, Data: data}

	front, format, declined, ok := e.registry.Detect(source)
	if !ok {
		return &Result{Format: format, Diagnostics: undetected(format, declined)}, nil
	}
	formatOpts, err := formatOptions(front, opts)
	if err != nil {
		return nil, fmt.Errorf("engine: options for %q: %w", specPath, err)
	}
	doc, diags, err := front.Compile(ctx, []compilers.Source{source},
		compilers.Options{FormatOptions: formatOpts})
	if err != nil {
		return nil, fmt.Errorf("engine: parse %q: %w", specPath, err)
	}
	if doc == nil {
		return &Result{Diagnostics: diags, Format: format}, nil
	}
	if !opts.SkipValidate {
		diags = append(diags, pass.Validate(doc)...)
	}
	// Both channels end up carrying the whole list: the Result is what a caller
	// gates on, and the document is what gets persisted (golden snapshots, IR
	// diff, caches, emitters). Merging rather than picking one is what keeps a
	// finding its compiler put on only one of them — see mergeDiagnostics.
	doc.Diagnostics = mergeDiagnostics(doc.Diagnostics, diags)
	return &Result{Document: doc, Diagnostics: doc.Diagnostics, Format: format}, nil
}

// undetected reports a source no registered compiler will take. None of the
// three cases is an I/O failure or a programmer error, so none may leave Run as
// a Go error: a caller that maps Go errors to "you invoked me wrong" — which the
// CLI does — would report a spec it read as a misuse of itself.
//
// A named format means a compiler read the source and named one this build does
// not carry: a Swagger 2.0 document, say, whose shape morphic understands and
// does not yet compile. Otherwise nothing recognized the bytes, and the only
// account of why is whatever the compilers that declined chose to give —
// preferred over the engine's own, because the engine parses nothing and can
// say no more than that nobody claimed it.
func undetected(format compilers.SourceFormat, declined []ir.Diagnostic) []ir.Diagnostic {
	if format.Name != "" {
		return []ir.Diagnostic{specProblem(codeNoCompilerForFormat,
			"no compiler registered for format %s", format)}
	}
	if len(declined) > 0 {
		return declined
	}
	return []ir.Diagnostic{specProblem(codeUnrecognizedFormat, "unrecognized spec format")}
}

// formatOptions resolves the compiler options for one run, decoding the textual
// channel through the compiler that will read them. os.ReadFile is what makes a
// path-valued setting work: the engine already reads the spec, so loading a file
// a setting names keeps the I/O on this side of the contract and the compiler
// pure.
func formatOptions(front compilers.Compiler, opts RunOptions) (any, error) {
	if len(opts.CompilerOptions) == 0 {
		return opts.FormatOptions, nil
	}
	if opts.FormatOptions != nil {
		return nil, errOptionChannels
	}
	decoded, err := front.DecodeOptions(compilers.OptionSet{
		Settings: opts.CompilerOptions,
		ReadFile: os.ReadFile,
	})
	if err != nil {
		return nil, fmt.Errorf("decode: %w", err)
	}
	return decoded, nil
}
