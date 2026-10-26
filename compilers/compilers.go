package compilers

import (
	"context"
	"errors"
	"fmt"
	"maps"
	"reflect"
	"slices"
	"strings"

	"github.com/dexpace/morphic/barrier"
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
// Reader reads every document a compile reads beyond its sources: one a
// source's references name, or one its options name, such as an overlay. Nil
// reads none, so a reference leaving the sources is a spec problem the
// compiler reports, never a read it makes itself.
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
}

// Compiler lowers source documents into the IR. Implementations must be pure:
// no package-level mutable state, no writes to stderr. Spec problems are
// ir.Diagnostic values; errors are for I/O and programmer faults, and no method
// panics.
//
// One value must accept overlapping Compile calls, except over one Source
// carrying a Parsed. Every method is required. Compile may also store its
// findings on the Document; a caller unions the two lists.
//
// The duties below are checked where a caller can: by Registry.Register and
// Registry.Detect, by Detection.Compile against a compile's inputs, and by
// irverify on the Document. A panic is a breach too.
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
	Detect(ctx context.Context, src Source, opts Options) (rec Recognition, diags []ir.Diagnostic, ok bool)
	// DecodeOptions turns textual settings into the value this compiler expects
	// in Options.FormatOptions. An empty set yields defaults. An unknown key or
	// an unusable value is an error — a setting that is silently ignored leaves
	// the caller believing they configured something. It reads nothing: a file a
	// setting names is Compile's to read, through Options.Reader.
	DecodeOptions(set OptionSet) (any, error)
	// SourceLimit is the most bytes a source may hold for this compiler to read
	// it under opts, zero or less for any size. Detect declines a source past it
	// unread, with an error saying why, and Compile refuses one. So a caller
	// reading a source before detection need read no further than one byte past
	// the largest limit among the compilers it asks: each declines what is past
	// its own. It reads no input and fails on none.
	SourceLimit(opts Options) int
	// Compile lowers sources into a Document, or refuses them with a nil one, no
	// error and at least one error diagnostic saying why. The Document is the
	// caller's alone, stamped ir.IRVersion, and its Sources begin with the table
	// SourceTable gives for the same arguments, each entry filled in with the
	// format Detect recognized and the SHA-256 of Data. Entries past it, if any,
	// name each further document the compile read. Every Provenance.Source it
	// reports other than ir.NoSource indexes them; a refusal's diagnostics index
	// SourceTable's table. The Document holds what irverify checks. Compile
	// stops when ctx is done.
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
// Concurrent Detect and Formats are safe once registration is complete.
// Register is not: it writes an unsynchronized map, so every Register must
// happen before the first concurrent read. Compose a Registry fully before
// publishing it, the way engine.New registers into a fresh one and only then
// wraps it.
type Registry struct {
	// byFormat finds a compiler by format. ordered is registration order, the one
	// order a caller controls and the order Detect asks in; ranging byFormat would
	// resolve two claimants of a source differently per run.
	byFormat map[SourceFormat]*registered
	ordered  []*registered
}

// registered is a compiler with the formats it reported when it registered.
// The registry asks for them once, behind a barrier, and keeps the answer: a
// later call could panic outside the registration, or name other formats than
// the ones the registry holds the compiler to.
type registered struct {
	c       Compiler
	formats []SourceFormat
}

// NewRegistry returns an empty registry.
func NewRegistry() *Registry {
	return &Registry{byFormat: make(map[SourceFormat]*registered)}
}

// Register adds c under every format it reports. It rejects a nil compiler, and
// a compiler reporting no formats, naming no dialect (an empty name or version,
// or a name holding '@') or panicking in Formats, the last three with an error
// wrapping ErrContract, and it fails if any format is already claimed. On
// failure nothing is registered.
//
// Rejecting nil keeps a caller's mistake a Go error rather than a segmentation
// fault in this package, so the check comes before any call on c.
func (r *Registry) Register(c Compiler) error {
	if isNilCompiler(c) {
		return errors.New("compilers: register: nil compiler")
	}
	formats, err := reported(c)
	if err != nil {
		return fmt.Errorf("compilers: register: %w", err)
	}
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
		r.byFormat = make(map[SourceFormat]*registered, len(formats))
	}
	entry := &registered{c: c, formats: formats}
	for _, format := range formats {
		r.byFormat[format] = entry
	}
	r.ordered = append(r.ordered, entry)
	return nil
}

// reported asks c for its formats, behind a barrier, and returns a copy: the
// compiler may keep the slice it answered with and change it later.
func reported(c Compiler) ([]SourceFormat, error) {
	var formats []SourceFormat
	if err := barrier.Catch(func() { formats = c.Formats() }); err != nil {
		return nil, breach("Formats", err)
	}
	return slices.Clone(formats), nil
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
// It fails on a canceled ctx; on undecodable options for the compiler taking
// src; and, wrapping ErrContract, on a claim or decline Compiler.Detect does
// not admit and a Detect, configure or SourceLimit that panics.
func (r *Registry) Detect(ctx context.Context, src Source, configure Configure) (Detection, bool, error) {
	if len(src.Data) == 0 {
		return Detection{Source: src}, false, nil
	}
	if configure == nil {
		configure = func(Compiler) (Options, error) { return Options{}, nil }
	}

	var declined []ir.Diagnostic
	for _, e := range r.ordered {
		if err := ctx.Err(); err != nil {
			return Detection{}, false, err
		}
		a, err := e.ask(ctx, src, configure)
		if err != nil {
			return Detection{}, false, err
		}
		// A Detect that returned once ctx was done may have stopped short, so
		// what it said is not judged, as no answer it gave would be whole.
		if err := ctx.Err(); err != nil {
			return Detection{}, false, err
		}
		if err := e.heldToLimit(a, len(src.Data)); err != nil {
			return Detection{}, false, err
		}
		if !a.ok {
			if err := checkDecline(a.diags); err != nil {
				return Detection{}, false, fmt.Errorf("compilers: detect by %s: %w", e.served(), err)
			}
			declined = append(declined, a.diags...)
			continue
		}
		// A claim that names no dialect answers no question, and passing over it
		// would hide which compiler swallowed a source the next one might take.
		if !a.rec.Format.valid() {
			return Detection{}, false, fmt.Errorf("compilers: detect by %s: claimed the source as %q: %w",
				e.served(), a.rec.Format, ErrContract)
		}
		det, ok, err := r.claim(e, a, configure)
		if err != nil {
			return Detection{}, false, err
		}
		det.Source = src
		return det, ok, nil
	}
	return Detection{Source: src, Declined: declined}, false, nil
}

// SourceLimit is the most bytes of a source any registered compiler reads,
// each under the options configure gives it (Compiler.SourceLimit), and zero
// when one reads any size or none is registered: how far a caller need read a
// source it hands to Detect. A compiler whose options fail to decode is asked
// under its defaults, as Detect asks it. It fails, wrapping ErrContract, on a
// configure or SourceLimit that panics.
func (r *Registry) SourceLimit(configure Configure) (int, error) {
	if configure == nil {
		configure = func(Compiler) (Options, error) { return Options{}, nil }
	}
	most := 0
	for _, e := range r.ordered {
		opts, _, err := e.configured(configure)
		if err != nil {
			return 0, err
		}
		limit, err := e.sourceLimit(opts)
		if err != nil {
			return 0, err
		}
		if limit <= 0 {
			return 0, nil
		}
		most = max(most, limit)
	}
	return most, nil
}

// sourceLimit asks e's compiler for its SourceLimit under opts, behind a
// barrier.
func (e *registered) sourceLimit(opts Options) (int, error) {
	var limit int
	if perr := barrier.Catch(func() { limit = e.c.SourceLimit(opts) }); perr != nil {
		return 0, fmt.Errorf("compilers: source limit of %s: %w", e.served(), breach("SourceLimit", perr))
	}
	return limit, nil
}

// heldToLimit holds what e said of a source of n bytes, a, to e's SourceLimit
// under the options a was asked under. Past it, a compiler declines with a
// reason, so a source a caller read no further than the limits allow is never
// taken: what it holds may be only the first bytes of one.
func (e *registered) heldToLimit(a answer, n int) error {
	limit, err := e.sourceLimit(a.opts)
	if err != nil {
		return err
	}
	switch {
	case limit <= 0 || n <= limit:
		return nil
	case a.ok:
		return fmt.Errorf("compilers: detect by %s: claimed a source of %d bytes, past its limit of %d: %w",
			e.served(), n, limit, ErrContract)
	case len(a.diags) == 0:
		return fmt.Errorf("compilers: detect by %s: declined a source of %d bytes, past its limit of %d, "+
			"saying nothing: %w", e.served(), n, limit, ErrContract)
	default:
		return nil
	}
}

// answer is what one compiler said of a source, with the options it was asked
// under and what configuring it said.
type answer struct {
	rec     Recognition
	diags   []ir.Diagnostic
	ok      bool
	opts    Options
	optsErr error
}

// ask configures e's compiler and asks it to recognize src, each behind a
// barrier. A panic in either is a breach, and an error from configuring is not:
// it is the answer a decoder gives to options that are another compiler's.
func (e *registered) ask(ctx context.Context, src Source, configure Configure) (answer, error) {
	opts, optsErr, err := e.configured(configure)
	if err != nil {
		return answer{}, err
	}

	a := answer{opts: opts, optsErr: optsErr}
	if perr := barrier.Catch(func() { a.rec, a.diags, a.ok = e.c.Detect(ctx, src, opts) }); perr != nil {
		return answer{}, fmt.Errorf("compilers: detect by %s: %w", e.served(), breach("Detect", perr))
	}
	return a, nil
}

// configured resolves the options e's compiler compiles with. The error it
// returns is a panic in configuring, a breach; what configuring itself failed
// with comes back as the second result for the caller to judge, beside the
// defaults: whatever a decoder that failed had got partway is not options.
func (e *registered) configured(configure Configure) (opts Options, failed, breached error) {
	if perr := barrier.Catch(func() { opts, failed = configure(e.c) }); perr != nil {
		return Options{}, nil, fmt.Errorf("compilers: detect by %s: %w", e.served(), breach("configure", perr))
	}
	if failed != nil {
		opts = Options{}
	}
	return opts, failed, nil
}

// served renders the formats e's compiler reports, for a breach that names it:
// a Compiler is an interface value, and its formats are what tell one apart.
func (e *registered) served() string {
	names := make([]string, 0, len(e.formats))
	for _, f := range e.formats {
		names = append(names, f.String())
	}
	return strings.Join(names, ", ")
}

// claim resolves what e recognized into the compiler that will compile the
// source and the options it compiles with. The caller records the source.
func (r *Registry) claim(e *registered, a answer, configure Configure) (Detection, bool, error) {
	rec, opts, optsErr := a.rec, a.opts, a.optsErr
	owner := r.byFormat[rec.Format]
	if owner != e {
		// The recognizer is not the owner, so its parse is not the owner's.
		rec.Parsed = nil
	}
	if owner == nil {
		// Nothing will compile the source, so no options are wanted for it.
		return Detection{Recognition: rec}, false, nil
	}
	if owner != e {
		// Nor are its options the owner's.
		var err error
		if opts, optsErr, err = owner.configured(configure); err != nil {
			return Detection{}, false, err
		}
	}
	if optsErr != nil {
		return Detection{}, false, fmt.Errorf("compilers: options for %s: %w", rec.Format, optsErr)
	}
	return Detection{Compiler: owner.c, Recognition: rec, Options: opts}, true, nil
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
