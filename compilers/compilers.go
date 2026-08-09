package compilers

import (
	"context"
	"errors"
	"fmt"
	"reflect"

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
type Compiler interface {
	Formats() []SourceFormat
	Compile(ctx context.Context, sources []Source, opts Options) (*ir.Document, []ir.Diagnostic, error)
}

// Registry maps source formats to compilers. It is a plain instance — there is
// no package-level default and no init()-time self-registration; the engine
// composes its registry explicitly. The zero value is a usable empty registry.
type Registry struct {
	byFormat map[SourceFormat]Compiler
}

// NewRegistry returns an empty registry.
func NewRegistry() *Registry {
	return &Registry{byFormat: make(map[SourceFormat]Compiler)}
}

// Register adds c under every format it reports. It rejects a nil compiler and
// a compiler reporting no formats, and it fails if any format is already
// claimed; on failure nothing is registered.
//
// Rejecting nil is what keeps a caller's programmer error a Go error instead of
// a segmentation fault raised inside this package, which is why the check comes
// before the only call this method makes on c.
func (r *Registry) Register(c Compiler) error {
	if isNilCompiler(c) {
		return errors.New("compilers: register: nil compiler")
	}
	formats := c.Formats()
	if len(formats) == 0 {
		return errors.New("compilers: register: compiler reports no formats")
	}
	for _, format := range formats {
		if _, taken := r.byFormat[format]; taken {
			return fmt.Errorf("compilers: register: format %s already registered", format)
		}
	}
	if r.byFormat == nil {
		r.byFormat = make(map[SourceFormat]Compiler, len(formats))
	}
	for _, format := range formats {
		r.byFormat[format] = c
	}
	return nil
}

// isNilCompiler reports whether c is unsafe to call: an untyped nil interface or
// a typed nil pointer stored in one.
//
// The two spellings are one bug — a typed nil passes c == nil and panics on the
// first method call that touches the receiver — so screening only the first
// leaves half the hole open. ir.IsNilTypeDef screens ir.TypeDef the same way.
func isNilCompiler(c Compiler) bool {
	if c == nil {
		return true
	}
	rv := reflect.ValueOf(c)
	return rv.Kind() == reflect.Pointer && rv.IsNil()
}

// Lookup returns the compiler registered for format.
func (r *Registry) Lookup(format SourceFormat) (Compiler, bool) {
	c, ok := r.byFormat[format]
	return c, ok
}
