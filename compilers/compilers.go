package compilers

import (
	"context"
	"errors"
	"fmt"
	"maps"
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
// caller loads bytes so compilation stays pure and reentrant, and a document a
// reference names reaches the compiler through Options.Reader.
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
//
// Reader reads the documents a source's references name. Nil reads none, so a
// reference leaving the sources is a spec problem the compiler reports, never
// a read it makes itself.
type Options struct {
	FormatOptions any
	Reader        DocumentReader
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
// One value must accept overlapping Compile calls, except over one Source
// carrying a Parsed. Every method is required: a compiler answering neither
// Detect nor DecodeOptions would be unreachable. Compile may also store its
// findings on the Document; a caller unions the two lists.
//
// The duties below are checked where a caller can: by Registry.Register and
// Registry.Detect, by Detection.Compile against a compile's inputs, and by
// irverify on the Document.
type Compiler interface {
	// Formats reports the dialects this compiler serves, at least one, each
	// naming a name and a version, the name free of the '@' String writes.
	Formats() []SourceFormat
	// Detect reports the format src declares and whether this compiler
	// recognizes it, which is not support: it may name a format it does not
	// serve. An ok answer names a format as Formats does: the one Compile reads.
	//
	// Detection honors the bounds opts sets for reading src. FormatOptions of
	// another compiler's type mean this one's defaults; only Compile rejects
	// them.
	//
	// diags is read only when ok is false. Declining another format's bytes is
	// silent; diags is for this compiler's own source, unreadable or one opts
	// forbid reading, holds an error saying why, and names src as Source 0, or
	// no source.
	Detect(src Source, opts Options) (rec Recognition, diags []ir.Diagnostic, ok bool)
	// DecodeOptions turns textual settings into the value this compiler expects
	// in Options.FormatOptions. An empty set yields defaults. An unknown key, an
	// unusable value, or a file that cannot be read is an error — a setting that
	// is silently ignored leaves the caller believing they configured something.
	DecodeOptions(set OptionSet) (any, error)
	// Compile lowers sources into a Document, or refuses them with a nil one, no
	// error and at least one error diagnostic saying why. A returned Document's
	// Sources begins with the table SourceTable gives for the same arguments,
	// each entry filled in, a source's naming the format Detect recognized and
	// the SHA-256 of its Data, and may go on to name each further document the
	// compile read, such as one a reference names. Every Provenance.Source it
	// reports other than ir.NoSource indexes that Document's Sources; a
	// refusal's diagnostics index SourceTable's table. The Document holds what
	// irverify checks; its Name may be empty.
	Compile(ctx context.Context, sources []Source, opts Options) (*ir.Document, []ir.Diagnostic, error)
	// SourceTable names the inputs its arguments name, in the order a compile's
	// provenance indexes them: the sources, then any input the options supply,
	// such as an overlay.
	//
	// It is the table a refusal's diagnostics index, and a returned Document's
	// Sources begins with it. It reads no input and fails on none, so an entry
	// holds only the Path it was given. For options a compile would reject, it
	// names the sources alone. It is required: a caller left to guess the
	// table can name only the sources, and would print an overlay's findings
	// without their file.
	SourceTable(sources []Source, opts Options) []ir.SourceInfo
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

// Register adds c under every format it reports. It rejects a nil compiler, and
// a compiler reporting no formats or one that names no dialect (an empty name or
// version, or a name holding '@') with an error wrapping ErrContract, and it
// fails if any format is already claimed; on failure nothing is registered.
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
		return fmt.Errorf("compilers: register: compiler reports no formats: %w", ErrContract)
	}
	for _, format := range formats {
		if !format.valid() {
			return fmt.Errorf("compilers: register: format %q names no dialect: %w", format, ErrContract)
		}
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
	// Source is the source detection was handed, and the one Compile compiles,
	// so what was recognized and what is compiled cannot be two inputs.
	Source Source
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
// under the options configure gives it, zero Options for a nil configure. The
// first format named decides: its owner is Detection.Compiler, configured for
// itself, and a recognizer not owning it loses its parse.
//
// The bool reports whether a compiler takes src. Unrecognized src, empty Data
// included, has the zero format, unlike src recognized but unsupported.
//
// It fails on a canceled ctx, checked before each compiler; on undecodable
// options for the compiler taking src; and, wrapping ErrContract, on a claim or
// a decline Compiler.Detect does not admit.
func (r *Registry) Detect(ctx context.Context, src Source, configure Configure) (Detection, bool, error) {
	if len(src.Data) == 0 {
		return Detection{Source: src}, false, nil
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
			if err := checkDecline(diags); err != nil {
				return Detection{}, false, fmt.Errorf("compilers: detect by %s: %w", served(c), err)
			}
			declined = append(declined, diags...)
			continue
		}
		// A claim that names no dialect answers no question, and passing over it
		// would hide which compiler swallowed a source the next one might take.
		if !rec.Format.valid() {
			return Detection{}, false, fmt.Errorf("compilers: detect by %s: claimed the source as %q: %w",
				served(c), rec.Format, ErrContract)
		}
		det, ok, err := r.claim(c, rec, opts, optsErr, configure)
		if err != nil {
			return Detection{}, false, err
		}
		det.Source = src
		return det, ok, nil
	}
	return Detection{Source: src, Declined: declined}, false, nil
}

// claim resolves a recognition into the compiler that will compile the source
// and the options it compiles with. c is the compiler that recognized it, and
// opts and optsErr are what configuring c gave. The caller records the source.
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
// An empty registry answers an empty list, never nil, so a caller that encodes
// the result writes [] rather than null.
//
// String order matches numeric order only while minor versions stay
// single-digit: 3.10 sorts ahead of 3.2. That is left unfixed because the order
// is read, never compared against, and a version comparator guessing at every
// format's scheme is a larger thing to get wrong.
func (r *Registry) Formats() []SourceFormat {
	formats := slices.AppendSeq(make([]SourceFormat, 0, len(r.byFormat)), maps.Keys(r.byFormat))
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
