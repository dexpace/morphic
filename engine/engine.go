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
// is a legal outcome (e.g. an unsupported spec version); the caller decides what
// is fatal.
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
// validate pass's diagnostics. The Go error return is reserved for I/O and
// programmer errors; spec problems surface as diagnostics in the Result.
func (e *Engine) Run(ctx context.Context, specPath string, opts RunOptions) (*Result, error) {
	data, err := os.ReadFile(specPath)
	if err != nil {
		return nil, fmt.Errorf("engine: read spec %q: %w", specPath, err)
	}
	format, err := Sniff(data)
	if err != nil {
		return nil, fmt.Errorf("engine: sniff %q: %w", specPath, err)
	}
	front, ok := e.registry.Lookup(format)
	if !ok {
		return nil, fmt.Errorf("engine: no compiler registered for format %s", format)
	}
	doc, diags, err := front.Compile(ctx,
		[]compilers.Source{{Path: specPath, Data: data}},
		compilers.Options{FormatOptions: opts.FormatOptions})
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
