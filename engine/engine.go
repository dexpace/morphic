package engine

import (
	"context"
	"fmt"
	"os"

	"github.com/dexpace/morphic/compilers"
	"github.com/dexpace/morphic/compilers/openapi"
	"github.com/dexpace/morphic/ir"
	"github.com/dexpace/morphic/pass"
)

// RunOptions configures a single pipeline run. FormatOptions is forwarded
// verbatim to the compiler as compilers.Options.FormatOptions.
type RunOptions struct {
	FormatOptions any  `json:"formatOptions,omitempty"`
	SkipValidate  bool `json:"skipValidate,omitempty"`
}

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

// Engine orchestrates the sniff → compiler → passes pipeline over a registry of
// compilers.
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
// at the lookup step, which is the seam TestEngine_RunLookupMiss relies on to
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

// Run executes the pipeline for the spec at specPath: read the file, sniff its
// format, dispatch to the matching compiler, and — unless disabled — append the
// validate pass's diagnostics.
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
	format, problem, ok := Sniff(data)
	if !ok {
		return &Result{Diagnostics: []ir.Diagnostic{problem}}, nil
	}
	front, registered := e.registry.Lookup(format)
	if !registered {
		return &Result{Format: format, Diagnostics: []ir.Diagnostic{
			specProblem(codeNoCompilerForFormat, "no compiler registered for format %s", format),
		}}, nil
	}
	doc, diags, err := front.Compile(ctx,
		[]compilers.Source{{Path: specPath, Data: data}},
		compilers.Options{FormatOptions: opts.FormatOptions})
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
