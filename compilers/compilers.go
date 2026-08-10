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

// OptionSet is one compile's configuration as text: settings named in the
// compiler's own option vocabulary, which DecodeOptions turns into that
// compiler's FormatOptions value.
//
// It exists so a caller can configure a compiler it does not import. The CLI
// collects key=value pairs without knowing which compiler will read them, and
// the compiler names and validates every key, so no layer above holds a list of
// another format's options.
type OptionSet struct {
	// Settings maps an option name to its textual value. A nil or empty map asks
	// for defaults.
	Settings map[string]string
	// ReadFile loads a file a setting names, e.g. an overlay document. A
	// compiler does no file I/O of its own — Source is the whole of its input,
	// which is what keeps compilation pure and reentrant — so the caller supplies
	// the reader and the read stays the caller's. A nil ReadFile means no setting
	// may name a file.
	ReadFile func(name string) ([]byte, error)
}

// Compiler lowers source documents into the IR. Implementations must be pure:
// no package-level mutable state, no writes to stderr; spec problems are
// returned as ir.Diagnostic values and the error return is reserved for
// I/O-level and programmer errors.
//
// Detect and DecodeOptions are what keep the layers above format-agnostic: a
// compiler says what its own input looks like and what its own options are
// called, so registering one is the whole of adding a format. Both are required
// rather than optional interfaces on purpose — a compiler that answered neither
// would be registered and unreachable, which is a hole no caller can see.
//
// A compiler reports through the returned slice. It may also store the same
// findings on the Document it returns — that copy is what the persisted IR JSON
// carries — but nothing obliges it to, and one that fills both must fill them
// alike. Neither list is guaranteed to hold the other, so a caller holding both
// unions them, as the engine does, rather than take one for the whole set.
type Compiler interface {
	Formats() []SourceFormat
	// Detect reports the format src declares, and whether this compiler
	// recognizes it at all. Recognition is not support: a compiler may name a
	// format it does not serve — a version it has yet to implement — so that the
	// caller can say so rather than report the source as unrecognized.
	//
	// An ok answer must name a format. Recognizing a source is knowing what it
	// is, so the zero format with ok true is no answer, and Registry.Detect
	// passes over a compiler that gives one rather than let it end the search.
	//
	// diags is what this compiler can say about a source it declines, and is read
	// only when ok is false. Bytes of another format are ordinary input here, so
	// declining them is silent: a compiler that reported every source it did not
	// take would bury the one report that matters under one per registered
	// format. It is for the narrower case where the source is recognizably this
	// compiler's own and cannot be read — a malformed document in its own
	// serialization — which no other compiler is in a position to say, and which
	// the caller would otherwise have to report as unrecognized.
	Detect(src Source) (format SourceFormat, diags []ir.Diagnostic, ok bool)
	// DecodeOptions turns textual settings into the value this compiler expects
	// in Options.FormatOptions. An empty set yields defaults. An unknown key, an
	// unusable value, or a file that cannot be read is an error — a setting that
	// is silently ignored leaves the caller believing they configured something.
	DecodeOptions(set OptionSet) (any, error)
	Compile(ctx context.Context, sources []Source, opts Options) (*ir.Document, []ir.Diagnostic, error)
}

// Registry maps source formats to compilers. It is a plain instance — there is
// no package-level default and no init()-time self-registration; the engine
// composes its registry explicitly. The zero value is a usable empty registry.
type Registry struct {
	byFormat map[SourceFormat]Compiler
	ordered  []Compiler
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
	r.ordered = append(r.ordered, c)
	return nil
}

// Detect asks each registered compiler in turn to recognize src, and returns
// the first that does: the compiler registered for the format it named, that
// format, and whether such a compiler exists. Nothing recognizing src is
// reported as the zero format, which is what tells "no compiler takes these
// bytes" from "this format is recognized but unsupported".
//
// diags collects what the declining compilers had to say, in the order they
// were asked, and is meaningful only when no compiler took src. A compiler that
// recognizes src ends the search, so nothing after it is asked and nothing it
// might have said is collected — the answer to "who takes this" makes any
// account of why others did not moot.
//
// The order is registration order, because it is the only order a caller
// controls — the variadic that composes the registry fixes it — and a map's
// would make two compilers that both claim a source resolve differently from
// run to run.
func (r *Registry) Detect(src Source) (Compiler, SourceFormat, []ir.Diagnostic, bool) {
	if len(src.Data) == 0 {
		return nil, SourceFormat{}, nil, false
	}

	var declined []ir.Diagnostic
	for _, c := range r.ordered {
		format, diags, ok := c.Detect(src)
		if !ok {
			declined = append(declined, diags...)
			continue
		}
		// A compiler that recognizes a source names the format it recognized. One
		// that claims a source and names nothing has answered no question, and
		// letting it end the search would hide every compiler registered after it
		// — a source another compiler would have taken becomes unrecognized, with
		// nothing to say which compiler swallowed it. Its diags are not collected:
		// the contract reads them only when a compiler declines, and this one did
		// not say it declined.
		if format == (SourceFormat{}) {
			continue
		}
		owner, registered := r.Lookup(format)
		return owner, format, nil, registered
	}
	return nil, SourceFormat{}, declined, false
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
