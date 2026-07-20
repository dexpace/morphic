package frontend

import (
	"context"
	"fmt"
	"sort"

	"github.com/dexpace/morphic/ir"
)

// SourceFormat identifies one spec dialect a frontend accepts.
type SourceFormat struct {
	Name    string // "openapi", "swagger", "typespec", "smithy", ...
	Version string // "3.0", "3.1", "2.0", ...
}

// String renders the canonical "name@version" form used in diagnostics and
// registry errors.
func (f SourceFormat) String() string { return f.Name + "@" + f.Version }

// Source is one pre-read input document. Frontends perform no file I/O; the
// caller loads bytes so parsing stays pure and reentrant.
type Source struct {
	Path string
	Data []byte
}

// Options carries per-parse configuration. FormatOptions is the
// frontend-specific options value; each frontend documents the concrete type
// it accepts and treats nil as defaults.
type Options struct {
	FormatOptions any
}

// Frontend lowers source documents into the IR. Implementations must be pure:
// no package-level mutable state, no writes to stderr; spec problems are
// returned as ir.Diagnostic values and the error return is reserved for
// I/O-level and programmer errors.
type Frontend interface {
	Formats() []SourceFormat
	Parse(ctx context.Context, sources []Source, opts Options) (*ir.Document, []ir.Diagnostic, error)
}

// Registry maps source formats to frontends. It is a plain instance — there is
// no package-level default and no init()-time self-registration; the engine
// composes its registry explicitly.
type Registry struct {
	byFormat map[SourceFormat]Frontend
}

// NewRegistry returns an empty registry.
func NewRegistry() *Registry {
	return &Registry{byFormat: make(map[SourceFormat]Frontend)}
}

// Register adds f under every format it reports. It fails if any format is
// already claimed; on failure nothing is registered.
func (r *Registry) Register(f Frontend) error {
	formats := f.Formats()
	if len(formats) == 0 {
		return fmt.Errorf("frontend: register: frontend reports no formats")
	}
	for _, format := range formats {
		if _, taken := r.byFormat[format]; taken {
			return fmt.Errorf("frontend: register: format %s already registered", format)
		}
	}
	for _, format := range formats {
		r.byFormat[format] = f
	}
	return nil
}

// Lookup returns the frontend registered for format.
func (r *Registry) Lookup(format SourceFormat) (Frontend, bool) {
	f, ok := r.byFormat[format]
	return f, ok
}

// Formats lists every registered format, sorted for stable display.
func (r *Registry) Formats() []SourceFormat {
	out := make([]SourceFormat, 0, len(r.byFormat))
	for f := range r.byFormat {
		out = append(out, f)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].String() < out[j].String() })
	return out
}
