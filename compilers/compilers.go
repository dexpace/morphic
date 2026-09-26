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
	// Parsed is what a compiler's own Detect already made of Data, for its
	// Compile to use instead of reading the bytes a second time. Recognizing a
	// source and lowering it both begin by parsing it, and the two calls are
	// back to back over the same bytes, so without this every compile parses
	// its input twice.
	//
	// It is never required. A nil Parsed — what a caller assembling a Source by
	// hand leaves — means the compiler parses Data itself, and so does a value
	// of a type it does not recognize, which is the only thing it may assume
	// about one: the type is the producing compiler's own, and a compiler must
	// type-assert with comma-ok rather than trust what it is handed.
	//
	// A Parsed value belongs to one Compile. What a compiler leaves here is live
	// state it writes through while compiling, so a Source carrying one may not
	// be compiled twice or shared between concurrent Compile calls. Data may
	// change under it, on the other hand, and a compiler must notice: bytes that
	// no longer match the parse beside them are read afresh rather than lowered
	// from a tree that does not describe them. Registry.Detect produces one per
	// source, and drops it unless the compiler that produced it is the one that
	// will consume it.
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
// no package-level mutable state, no writes to stderr; spec problems are
// returned as ir.Diagnostic values and the error return is reserved for
// I/O-level and programmer errors.
//
// Purity is also the concurrency contract. One Compiler value must accept
// overlapping Compile calls, which is what lets a caller share a single engine
// across goroutines; a Compiler that memoizes into package state would break
// that caller's guarantee without changing this signature.
//
// The guarantee is the compiler's, not the argument's. Two Compile calls may
// run at once over Sources holding the same bytes; they may not run at once
// over one Source carrying a Parsed, nor may one such Source be compiled twice,
// because a Parsed is the compiler's working state and a compile writes through
// it. Source.Parsed says so, and nothing here can enforce it: a Source is the
// caller's value, and the type system cannot tell one that has been compiled
// from one that has not.
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
	// opts is what this compiler would compile src with, so that recognizing a
	// source is held to the bounds the caller set for reading it rather than to
	// a ceiling of the compiler's choosing: the read that recognition makes may
	// be the most expensive read of the source there is. A FormatOptions value
	// of a type this compiler does not take configures some other compiler, and
	// here means this one's defaults; Compile is where such a value is an error,
	// since only there is it certain the caller meant it for this compiler.
	//
	// A compiler that parsed src to recognize it may return that parse in
	// Recognition.Parsed, and read it back from Source.Parsed in Compile. It is
	// an optimization and never a protocol: a compiler must compile a source
	// whose Parsed is nil or another compiler's, since a caller calling Compile
	// directly never went through Detect at all.
	//
	// diags is what this compiler can say about a source it declines, and is read
	// only when ok is false. Bytes of another format are ordinary input here, so
	// declining them is silent: a compiler that reported every source it did not
	// take would bury the one report that matters under one per registered
	// format. It is for the narrower cases where the source is recognizably this
	// compiler's own and cannot be read — a malformed document in its own
	// serialization — or where opts forbid reading it at all, a source past the
	// caller's size budget. No other compiler is in a position to say either,
	// and the caller would otherwise have to report the source as unrecognized.
	// Such a diagnostic names src as Source 0: the one source detection was
	// handed is the whole of the table it can index.
	Detect(src Source, opts Options) (rec Recognition, diags []ir.Diagnostic, ok bool)
	// DecodeOptions turns textual settings into the value this compiler expects
	// in Options.FormatOptions. An empty set yields defaults. An unknown key, an
	// unusable value, or a file that cannot be read is an error — a setting that
	// is silently ignored leaves the caller believing they configured something.
	DecodeOptions(set OptionSet) (any, error)
	// Compile lowers sources into a Document, or refuses them by returning a nil
	// one. Every Provenance.Source it reports, on the Document or in the
	// returned diagnostics, indexes the table SourceTable gives for the same
	// arguments, and a returned Document's Sources names those inputs in that
	// order.
	Compile(ctx context.Context, sources []Source, opts Options) (*ir.Document, []ir.Diagnostic, error)
	// SourceTable names the inputs a compile of sources under opts reads, in the
	// order its provenance indexes them: the sources, then any input the
	// options supply, such as an overlay.
	//
	// It exists for the compile that returns no Document. A refusal's
	// diagnostics still index a table, and without this one a caller has
	// nothing to resolve them against and cannot say which file a finding is
	// in. It reads no input and fails on none, so an entry carries the Path it
	// was given and nothing a read would have filled in. For options a compile
	// would reject, it names the sources alone.
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

// Detect asks each registered compiler in turn to recognize src, under the
// options configure resolves for it, and returns the first that does: the
// compiler registered for the format it named, what that recognition found, the
// options it compiles with, and whether such a compiler exists. Nothing
// recognizing src is reported as the zero format, which is what tells "no
// compiler takes these bytes" from "this format is recognized but unsupported".
// A nil configure gives every compiler the zero Options, its defaults.
//
// Options a compiler cannot be configured with are an error only when that
// compiler is the one taking src. Until then they may well be another's: a
// setting in one compiler's vocabulary fails to decode in every other, and a
// compiler that the settings do not describe is configured by its defaults,
// which is what it recognizes under. A compiler that takes src with options it
// could not decode ends the search with the error, because those are the
// options it would have been asked to compile with.
//
// The parse a recognition carries survives only when the compiler that produced
// it is the one registered for the format it named. A compiler may recognize a
// format another serves — an OpenAPI compiler names Swagger so the caller hears
// "unsupported" rather than "unreadable" — and one compiler's parse is not
// another's to read, whatever its type happens to be. The owner is configured
// in its own right then, since the recognizer's options are not its.
//
// A compiler that recognizes src ends the search, so nothing after it is asked
// and nothing it might have said is collected — the answer to "who takes this"
// makes any account of why others did not moot.
//
// The order is registration order, because it is the only order a caller
// controls — the variadic that composes the registry fixes it — and a map's
// would make two compilers that both claim a source resolve differently from
// run to run.
//
// ctx is consulted before each compiler is asked, since recognizing a source may
// mean parsing it, and a canceled ctx is returned as the error.
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

// Formats returns every format some registered compiler serves.
//
// It is sorted rather than in registration order, because this answers "what
// does this build accept" for a reader, and a set rendered in a different order
// from run to run reads as a different set.
//
// Sorting is by name, then by version as a string. That matches numeric order
// only while minor versions stay single-digit: a 3.10 would sort ahead of 3.2,
// not behind it. The deviation is left rather than fixed because this order is
// read, never compared against — nothing selects a compiler by position here —
// and a version comparator that guessed at every format's scheme would be a
// larger thing to get wrong than a list one line out of order.
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
