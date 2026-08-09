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
type Compiler interface {
	Formats() []SourceFormat
	// Detect reports the format src declares, and whether this compiler
	// recognizes it at all. Recognition is not support: a compiler may name a
	// format it does not serve — a version it has yet to implement — so that the
	// caller can say so rather than report the source as unrecognized. It must
	// not report a parse failure as anything but "not mine"; the bytes of another
	// format are ordinary input here, not an error.
	Detect(src Source) (SourceFormat, bool)
	// DecodeOptions turns textual settings into the value this compiler expects
	// in Options.FormatOptions. An empty set yields defaults. An unknown key, an
	// unusable value, or a file that cannot be read is an error — a setting that
	// is silently ignored leaves the caller believing they configured something.
	DecodeOptions(set OptionSet) (any, error)
	Compile(ctx context.Context, sources []Source, opts Options) (*ir.Document, []ir.Diagnostic, error)
}

// Registry maps source formats to compilers. It is a plain instance — there is
// no package-level default and no init()-time self-registration; the engine
// composes its registry explicitly.
type Registry struct {
	byFormat map[SourceFormat]Compiler
	ordered  []Compiler
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
	r.ordered = append(r.ordered, c)
	return nil
}

// Detect asks each registered compiler in turn to recognize src, and returns
// the first that does: the compiler registered for the format it named, that
// format, and whether such a compiler exists. Nothing recognizing src is
// reported as the zero format, which is what tells "no compiler takes these
// bytes" from "this format is recognized but unsupported".
//
// The order is registration order, because it is the only order a caller
// controls — the variadic that composes the registry fixes it — and a map's
// would make two compilers that both claim a source resolve differently from
// run to run.
func (r *Registry) Detect(src Source) (Compiler, SourceFormat, bool) {
	if len(src.Data) == 0 {
		return nil, SourceFormat{}, false
	}
	for _, c := range r.ordered {
		format, ok := c.Detect(src)
		if !ok {
			continue
		}
		owner, registered := r.byFormat[format]
		return owner, format, registered
	}
	return nil, SourceFormat{}, false
}

// Lookup returns the compiler registered for format.
func (r *Registry) Lookup(format SourceFormat) (Compiler, bool) {
	c, ok := r.byFormat[format]
	return c, ok
}
