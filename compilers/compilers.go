package compilers

import (
	"context"
	"fmt"

	"github.com/dexpace/morphic/ir"
)

// SourceFormat identifies one spec dialect a compiler accepts.
type SourceFormat struct {
	Name    string // "openapi", "swagger", "typespec", "smithy", ...
	Version string // "3.0", "3.1", "2.0", ...
}

// String renders the canonical "name@version" form used in diagnostics and
// registry errors.
func (f SourceFormat) String() string { return f.Name + "@" + f.Version }

// Source is one pre-read input document. Compilers perform no file I/O; the
// caller loads bytes so compilation stays pure and reentrant.
type Source struct {
	Path string
	Data []byte
}

// Options carries per-compile configuration. FormatOptions is the
// compiler-specific options value; each compiler documents the concrete type
// it accepts and treats nil as defaults.
type Options struct {
	FormatOptions any
}

// Compiler lowers source documents into the IR. Implementations must be pure:
// no package-level mutable state, no writes to stderr; spec problems are
// returned as ir.Diagnostic values and the error return is reserved for
// I/O-level and programmer errors.
//
// Purity is also the concurrency contract. One Compiler value must accept
// overlapping Compile calls, which is what lets a caller share a single engine
// across goroutines; a Compiler that memoizes into package state would break
// that caller's guarantee without changing this signature.
type Compiler interface {
	Formats() []SourceFormat
	Compile(ctx context.Context, sources []Source, opts Options) (*ir.Document, []ir.Diagnostic, error)
}

// Registry maps source formats to compilers. It is a plain instance — there is
// no package-level default and no init()-time self-registration; the engine
// composes its registry explicitly.
//
// Concurrent Lookup is safe once registration is complete. Register is not: it
// writes an unsynchronized map, so every Register must happen before the first
// concurrent Lookup. Compose a Registry fully before publishing it, the way
// engine.NewWith registers into a fresh one and only then wraps it.
type Registry struct {
	byFormat map[SourceFormat]Compiler
}

// NewRegistry returns an empty registry.
func NewRegistry() *Registry {
	return &Registry{byFormat: make(map[SourceFormat]Compiler)}
}

// Register adds c under every format it reports. It fails if any format is
// already claimed; on failure nothing is registered.
func (r *Registry) Register(c Compiler) error {
	formats := c.Formats()
	if len(formats) == 0 {
		return fmt.Errorf("compilers: register: compiler reports no formats")
	}
	for _, format := range formats {
		if _, taken := r.byFormat[format]; taken {
			return fmt.Errorf("compilers: register: format %s already registered", format)
		}
	}
	for _, format := range formats {
		r.byFormat[format] = c
	}
	return nil
}

// Lookup returns the compiler registered for format.
func (r *Registry) Lookup(format SourceFormat) (Compiler, bool) {
	c, ok := r.byFormat[format]
	return c, ok
}
