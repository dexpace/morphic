package engine

import (
	"context"
	"fmt"
	"os"

	"github.com/dexpace/morphic/frontend"
	"github.com/dexpace/morphic/frontend/openapi"
	"github.com/dexpace/morphic/ir"
	"github.com/dexpace/morphic/pass"
)

// RunOptions configures a single pipeline run. FormatOptions is forwarded
// verbatim to the frontend as frontend.Options.FormatOptions.
type RunOptions struct {
	FormatOptions any  `json:"formatOptions,omitempty"`
	SkipValidate  bool `json:"skipValidate,omitempty"`
}

// Result is the outcome of a pipeline run. A nil Document alongside diagnostics
// is a legal outcome (e.g. an unsupported spec version); the caller decides what
// is fatal.
type Result struct {
	Document    *ir.Document          `json:"document,omitempty"`
	Diagnostics []ir.Diagnostic       `json:"diagnostics,omitempty"`
	Format      frontend.SourceFormat `json:"format"`
}

// Engine orchestrates the sniff → frontend → passes pipeline over a registry of
// frontends.
type Engine struct {
	registry *frontend.Registry
}

// New composes the default engine: a registry with every built-in frontend
// registered. Future frontends are added here and only here.
func New() (*Engine, error) {
	reg := frontend.NewRegistry()
	if err := reg.Register(openapi.New()); err != nil {
		return nil, fmt.Errorf("engine: register openapi frontend: %w", err)
	}
	return NewWithRegistry(reg), nil
}

// NewWithRegistry builds an engine over a caller-supplied registry, for tests
// and embedders that need a custom frontend set.
func NewWithRegistry(reg *frontend.Registry) *Engine {
	return &Engine{registry: reg}
}

// Run executes the pipeline for the spec at specPath: read the file, sniff its
// format, dispatch to the matching frontend, and — unless disabled — append the
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
		return nil, fmt.Errorf("engine: no frontend registered for format %s", format)
	}
	doc, diags, err := front.Parse(ctx,
		[]frontend.Source{{Path: specPath, Data: data}},
		frontend.Options{FormatOptions: opts.FormatOptions})
	if err != nil {
		return nil, fmt.Errorf("engine: parse %q: %w", specPath, err)
	}
	if !opts.SkipValidate && doc != nil {
		// Land the pass diagnostics in the document too, so the persisted IR JSON
		// (golden snapshots, IR diff, caches, backends) carries them and does not
		// silently lose error-level validation findings.
		doc.Diagnostics = append(doc.Diagnostics, pass.Validate(doc)...)
		diags = doc.Diagnostics
	}
	return &Result{Document: doc, Diagnostics: diags, Format: format}, nil
}
