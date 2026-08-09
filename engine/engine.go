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
// is a legal outcome (e.g. an unsupported spec version); the caller decides what
// is fatal.
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
// an Engine, for tests and embedders that need a custom compiler set. A
// register failure (a compiler reporting no formats, or two compilers claiming
// the same format) surfaces as a Go error rather than a panic. Calling
// NewWith with no compilers is legal: the resulting engine's Run always fails
// at detection, which is the seam TestEngine_RunNothingRegistered relies on to
// reach that branch.
func NewWith(fronts ...compilers.Compiler) (*Engine, error) {
	reg := compilers.NewRegistry()
	for _, front := range fronts {
		if err := reg.Register(front); err != nil {
			return nil, fmt.Errorf("engine: register compiler: %w", err)
		}
	}
	return &Engine{registry: reg}, nil
}

// Run executes the pipeline for the spec at specPath: read the file, ask the
// registered compilers which of them recognizes it, dispatch to that one, and —
// unless disabled — append the validate pass's diagnostics. The Go error return
// is reserved for I/O and programmer errors; spec problems surface as
// diagnostics in the Result.
func (e *Engine) Run(ctx context.Context, specPath string, opts RunOptions) (*Result, error) {
	data, err := os.ReadFile(specPath)
	if err != nil {
		return nil, fmt.Errorf("engine: read spec %q: %w", specPath, err)
	}
	source := compilers.Source{Path: specPath, Data: data}

	front, format, ok := e.registry.Detect(source)
	if !ok {
		return nil, unsupportedFormat(specPath, format)
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
	if !opts.SkipValidate && doc != nil {
		// Land the pass diagnostics in the document too, so the persisted IR JSON
		// (golden snapshots, IR diff, caches, emitters) carries them and does not
		// silently lose error-level validation findings.
		doc.Diagnostics = append(doc.Diagnostics, pass.Validate(doc)...)
		diags = doc.Diagnostics
	}
	return &Result{Document: doc, Diagnostics: diags, Format: format}, nil
}

// unsupportedFormat reports a source no registered compiler will take. The two
// cases are worth telling apart: a zero format means nothing recognized the
// bytes at all, while a named one means a compiler read the source and named a
// format this build does not carry — a Swagger 2.0 document, say, which is a
// spec morphic understands the shape of and does not yet compile.
func unsupportedFormat(specPath string, format compilers.SourceFormat) error {
	if format.Name == "" {
		return fmt.Errorf("engine: no compiler recognizes %q as a supported spec format", specPath)
	}
	return fmt.Errorf("engine: no compiler registered for format %s (%q)", format, specPath)
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
