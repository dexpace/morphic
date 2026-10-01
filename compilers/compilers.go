package compilers

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"slices"
	"strings"

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
	// Parsed is what the compiler's own Detect already made of Data, so Compile
	// need not parse it again. It is never required: nil, or a value of a type
	// the compiler does not recognize, means Compile parses Data itself, so a
	// compiler must type-assert it with comma-ok.
	//
	// A Parsed belongs to one Compile. It is live state the compiler writes
	// through, so a Source carrying one may not be compiled twice or shared
	// between concurrent Compile calls. If Data changes under it, the compiler
	// must read the bytes afresh. Registry.Detect drops a Parsed whose producer
	// will not consume it.
	Parsed any
}

// Recognition is what one compiler made of a source it recognized: the format
// it names, and whatever it parsed while deciding. The parse is carried so it
// can be handed to the same compiler's Compile rather than repeated there; see
// Source.Parsed for what a consumer may assume about it.
type Recognition struct {
	// Format is the dialect the source declares. An ok recognition names one.
	Format SourceFormat
	// Parsed is the value to put in Source.Parsed before compiling this source,
	// or nil when the compiler parsed nothing worth keeping — it read the bytes
	// some other way, or it declined.
	Parsed any
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
// no package-level mutable state, no writes to stderr. Spec problems are
// ir.Diagnostic values; errors are for I/O and programmer faults.
//
// One Compiler value must accept overlapping Compile calls, except over one
// Source carrying a Parsed (see Source.Parsed).
//
// Detect and DecodeOptions are methods, not optional interfaces: a compiler
// answering neither would be registered yet unreachable.
//
// Compile may also store its findings on the Document, for persisted IR JSON; a
// compiler doing both fills them alike, but a caller unions the two, since
// neither is guaranteed to hold the other.
type Compiler interface {
	Formats() []SourceFormat
	// Detect reports the format src declares and whether this compiler
	// recognizes it. Recognition is not support: it may name a format it does
	// not serve. An ok answer must name a format, or Registry.Detect ignores
	// it, diags included, and asks the next compiler.
	//
	// opts carries the caller's bounds for reading src, which recognition
	// honors. FormatOptions of a type this compiler does not take mean its
	// defaults; only Compile rejects them.
	//
	// diags is read only when ok is false. Declining another format's bytes is
	// silent; diags is for this compiler's own source when it is unreadable or
	// opts forbid reading it.
	Detect(src Source, opts Options) (rec Recognition, diags []ir.Diagnostic, ok bool)
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
//
// Concurrent Lookup is safe once registration is complete. Register is not: it
// writes an unsynchronized map, so every Register must happen before the first
// concurrent Lookup. Compose a Registry fully before publishing it, the way
// engine.NewWith registers into a fresh one and only then wraps it.
type Registry struct {
	// byFormat finds a compiler by format. ordered is registration order, the one
	// order a caller controls and the order Detect asks in; ranging byFormat would
	// resolve two claimants of a source differently per run.
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

// Configure resolves the options one compiler would compile a source with. It
// is asked per compiler because what configures a run is, in general, one
// compiler's vocabulary — textual settings only that compiler can decode — and
// which compiler a source belongs to is what detection is still finding out.
type Configure func(c Compiler) (Options, error)

// Detection is what Registry.Detect found for one source.
type Detection struct {
	// Compiler is the compiler registered for the format the source was
	// recognized as, or nil when none is.
	Compiler Compiler
	// Recognition is what recognizing the source found. Its Format is the zero
	// format when nothing recognized it; see Registry.Detect.
	Recognition Recognition
	// Options is what Compiler compiles the source with: the answer Configure
	// gave for it, which the caller hands on rather than resolving again.
	Options Options
	// Declined collects what the compilers that declined the source had to say,
	// in the order they were asked, and is meaningful only when none took it.
	Declined []ir.Diagnostic
}

// Detect asks each registered compiler, in registration order, to recognize src
// under the options configure gives it. The first format named decides;
// Detection.Compiler is its owner, and a recognizer that does not own it loses
// its parse while the owner is configured separately.
//
// The bool reports whether a compiler takes src. Unrecognized src, empty Data
// included, has the zero format, unlike src recognized but unsupported; a nil
// configure means zero Options for all.
//
// Undecodable options are an error only for the compiler that takes src. ctx is
// checked before each compiler is asked, and once canceled it is the error.
func (r *Registry) Detect(ctx context.Context, src Source, configure Configure) (Detection, bool, error) {
	if len(src.Data) == 0 {
		return Detection{}, false, nil
	}
	if configure == nil {
		configure = func(Compiler) (Options, error) { return Options{}, nil }
	}

	var declined []ir.Diagnostic
	for _, c := range r.ordered {
		if err := ctx.Err(); err != nil {
			return Detection{}, false, err
		}
		opts, optsErr := configure(c)
		if optsErr != nil {
			opts = Options{}
		}
		rec, diags, ok := c.Detect(src, opts)
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
		if rec.Format == (SourceFormat{}) {
			continue
		}
		return r.claim(c, rec, opts, optsErr, configure)
	}
	return Detection{Declined: declined}, false, nil
}

// claim resolves a recognition into the compiler that will compile the source
// and the options it compiles with. c is the compiler that recognized it, and
// opts and optsErr are what configuring c gave.
func (r *Registry) claim(c Compiler, rec Recognition, opts Options, optsErr error, configure Configure) (Detection, bool, error) {
	owns := slices.Contains(c.Formats(), rec.Format)
	if !owns {
		// The recognizer is not the owner, so its parse is not the owner's.
		// Asking which formats it registered answers that without comparing two
		// Compiler values, which panics outright when a compiler's type is not
		// comparable.
		rec.Parsed = nil
	}
	owner, registered := r.Lookup(rec.Format)
	if !registered {
		// Nothing will compile the source, so no options are wanted for it.
		return Detection{Recognition: rec}, false, nil
	}
	if !owns {
		// Nor are its options the owner's.
		opts, optsErr = configure(owner)
	}
	if optsErr != nil {
		return Detection{}, false, fmt.Errorf("compilers: options for %s: %w", rec.Format, optsErr)
	}
	return Detection{Compiler: owner, Recognition: rec, Options: opts}, true, nil
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

// Formats returns every format some registered compiler serves, sorted by name
// and then by version as a string, so the set reads the same from run to run.
//
// String order matches numeric order only while minor versions stay
// single-digit: 3.10 sorts ahead of 3.2. That is left unfixed because the order
// is read, never compared against, and a version comparator guessing at every
// format's scheme is a larger thing to get wrong.
func (r *Registry) Formats() []SourceFormat {
	formats := make([]SourceFormat, 0, len(r.byFormat))
	for format := range r.byFormat {
		formats = append(formats, format)
	}
	slices.SortFunc(formats, func(a, b SourceFormat) int {
		if byName := strings.Compare(a.Name, b.Name); byName != 0 {
			return byName
		}
		return strings.Compare(a.Version, b.Version)
	})
	return formats
}

// Lookup returns the compiler registered for format.
func (r *Registry) Lookup(format SourceFormat) (Compiler, bool) {
	c, ok := r.byFormat[format]
	return c, ok
}
