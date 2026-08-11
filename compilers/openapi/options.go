package openapi

import (
	"fmt"
	"maps"
	"slices"
	"strconv"
	"strings"

	"github.com/dexpace/morphic/compilers"
	"github.com/dexpace/morphic/compilers/openapi/internal/lowering"
)

// GroupingStrategy selects how operations are grouped into OperationGroups. It
// is the injectable-policy seam (architecture principle 6): grouping is inferred
// policy, not source semantics, and can be switched or disabled.
//
// The vocabulary is declared once, beneath both walks, and named here rather
// than restated: two declarations of one strategy set can drift, and a strategy
// only the public half knew would fall through to the default unreported.
type GroupingStrategy = lowering.GroupingStrategy

// Grouping strategies.
const (
	// GroupByTags groups operations by their first OpenAPI tag (default).
	GroupByTags = lowering.GroupByTags
	// GroupByPathPrefix groups operations by the first path segment.
	GroupByPathPrefix = lowering.GroupByPathPrefix
)

// StreamingMedia is the media-type streaming policy: which media types imply
// that a body is a sequence of frames when the document declares nothing that
// says so. It is the second injectable-policy seam (architecture principle 6),
// and it is named here rather than restated for the reason GroupingStrategy is.
type StreamingMedia = lowering.StreamingMedia

// DefaultStreamingMediaTypes returns the media types StreamingMedia classifies
// as streams when the caller names none. It is exported so a caller extending
// the list can start from it rather than transcribe it.
func DefaultStreamingMediaTypes() []string { return lowering.DefaultStreamingMediaTypes() }

// Options configures the OpenAPI compiler. It is the concrete type this
// compiler expects in compilers.Options.FormatOptions; the zero value is valid
// and normalized by withDefaults.
//
// It stays whole here while each phase below takes its own input projected from
// it — load.Options, and the strategy the lowering context carries. Its shape is
// a published contract (ir-design §10), and most of it describes work no single
// phase does.
type Options struct {
	// Grouping selects the operation-grouping strategy.
	Grouping GroupingStrategy `json:"grouping,omitempty"`
	// StreamingMedia selects which media types imply a stream. The zero value is
	// the default list, on; a caller who wants only what a document declares
	// disables it.
	StreamingMedia StreamingMedia `json:"streamingMedia"`
	// AllowExternalRefs lets reference resolution leave the source document —
	// reading files off disk and fetching http(s) URLs. Off by default, because
	// compilers.Source is the whole input ("the caller loads bytes so compilation
	// stays pure and reentrant") and a spec is untrusted data whose $refs would
	// otherwise name any readable file or reachable host. A $ref leaving the
	// document is reported unresolved instead of followed.
	//
	// Turning it on departs from that contract knowingly, and buys less than it
	// looks: resolution reads relative to the process working directory, so the
	// same bytes compile differently in two directories, and the resolved content
	// still does not reach lowering — Sources records one entry either way
	// (GitHub #74 carries the multi-file work).
	AllowExternalRefs bool `json:"allowExternalRefs"`
	// Overlay is an OpenAPI Overlay document to apply to the source before
	// lowering, or nil for none. It is the source-document patching hook
	// architecture §2.2 names, and is deliberately not the IR overlay pass beside
	// it: a fix that has to land before naming and hoisting heuristics read the
	// broken shape cannot be made afterwards.
	Overlay *Overlay `json:"overlay,omitempty"`
	// Limits bounds how large an input this compile will lower. The zero value
	// takes every default, so it carries no omitempty: a struct is never empty to
	// encoding/json, and a tag implying otherwise would be read as a promise.
	Limits Limits `json:"limits"`
}

// Compiled-in defaults for Limits, each calibrated against measured documents
// rather than chosen round. The measurements are of three flagship public
// descriptions, taken 2026-08-09: GitHub's REST API (12,920,264 bytes, 471,735
// parsed YAML nodes, largest enum 53 members), Stripe's (7,967,776 bytes,
// 265,846 nodes, largest enum 599 members) and Kubernetes' aggregated Swagger
// (4,475,339 bytes, 138,990 nodes, no enum at all).
const (
	// DefaultMaxSourceBytes is the byte budget: 64 MiB, a 5.2x margin over the
	// largest measured description. It is the only budget enforceable before the
	// source is parsed, which is why it is the loosest — it exists to bound the
	// parse itself, and everything finer is measured after it.
	DefaultMaxSourceBytes = 1 << 26
	// DefaultMaxSourceNodes is the parsed-node budget: 2,097,152, a 4.4x margin
	// over the largest measured description. Node count, not byte count, is what
	// the phases after the parse cost — building the typed model and resolving
	// its references peaked at 1.5 GB of RSS for GitHub's 471,735 nodes — so this
	// is the budget that bounds a compile, and the byte budget above is only what
	// gets a document far enough to be counted.
	DefaultMaxSourceNodes = 1 << 21
	// DefaultMaxEnumMembers is the per-enum member budget: 65,536, a 109x margin
	// over the largest measured enum and roughly 7x the largest registry an API
	// might reasonably inline (IATA airport codes, BCP-47 language subtags, both
	// under 10,000 entries).
	//
	// An enum is the one construct whose IR cost per source node is
	// disproportionate: every member becomes an ir.EnumMember with a canonical
	// word sequence of its own, and a heterogeneous one becomes a hoisted Literal
	// type plus a union variant per member. That is the amplification GitHub #75
	// reports: one 1,000,000-member enum turning 10 MB of source into 2.6 GB of
	// peak RSS, on a document well inside the node budget above — which is why
	// that budget does not cover this one. An enum at this budget compiles in
	// under 200 MB.
	DefaultMaxEnumMembers = 1 << 16
)

// Limits bounds the size and cardinality of one compile, so that an input which
// is legal but pathologically large is refused with a diagnostic rather than
// left to exhaust the host (GitHub #75).
//
// It is policy rather than semantics (architecture principle 6): what counts as
// pathological depends on the machine doing the compiling, so every budget here
// is the caller's to set. In each field zero takes the documented default and a
// negative value means unbounded — the compile then behaves as it did before
// these budgets existed, which is the escape hatch for a caller who has measured
// their own input and their own machine.
//
// These are budgets on the size of the input. They are not the only bounds the
// compiler enforces: schema nesting depth, YAML alias expansion, reference-chain
// length and several walk node counts are each bounded by a constant beside the
// code that walks them, because none of those describes something a caller could
// legitimately want more of.
type Limits struct {
	// MaxSourceBytes bounds one source document's size in bytes, checked before
	// it is parsed.
	MaxSourceBytes int `json:"maxSourceBytes,omitempty"`
	// MaxSourceNodes bounds the YAML nodes one source document parses to,
	// checked before the typed model is built from it.
	MaxSourceNodes int `json:"maxSourceNodes,omitempty"`
	// MaxEnumMembers bounds the members of a single enum. An enum past it lowers
	// as the top type with an error diagnostic naming the budget; the rest of the
	// document still lowers.
	MaxEnumMembers int `json:"maxEnumMembers,omitempty"`
}

// withDefaults returns a copy of l with each unset budget filled from its
// default. A negative budget is left alone — it is the caller asking for none.
func (l Limits) withDefaults() Limits {
	if l.MaxSourceBytes == 0 {
		l.MaxSourceBytes = DefaultMaxSourceBytes
	}
	if l.MaxSourceNodes == 0 {
		l.MaxSourceNodes = DefaultMaxSourceNodes
	}
	if l.MaxEnumMembers == 0 {
		l.MaxEnumMembers = DefaultMaxEnumMembers
	}
	return l
}

// bounded projects one resolved budget onto the internal form the phases below
// read, where zero alone means unbounded. It is the single place the public
// spelling of "no budget" — a negative value — is translated, so no phase has to
// know two spellings of it.
func bounded(limit int) int {
	if limit < 0 {
		return 0
	}
	return limit
}

// Overlay is one pre-read OpenAPI Overlay document (the Overlay Specification's
// own format, applied with JSONPath selectors) and how strictly to apply it.
//
// The document arrives as bytes, like the spec itself, because a compiler
// performs no file I/O — reading it is the caller's job, which is what keeps
// compilation pure and reentrant. A programmatic caller sets this through
// engine.RunOptions.FormatOptions, which the engine forwards verbatim; a caller
// who has only text names the file with the "overlay" setting and the reader in
// compilers.OptionSet loads it, so the read is still the caller's.
//
// An applied overlay becomes a second entry in Document.Sources, and every
// position it introduced or rewrote names that entry as its Provenance.Source.
// The positions it left alone keep the source's own line and column, because the
// overlay is applied to the parsed node tree rather than to re-serialised bytes.
type Overlay struct {
	// Path names the overlay document. It is recorded as the overlay's
	// SourceInfo path and never opened.
	Path string `json:"path,omitempty"`
	// Data is the overlay document's bytes.
	Data []byte `json:"data,omitempty"`
	// Lax turns off strict application.
	//
	// Strict — the zero value — is the default because an action whose selector
	// matches nothing is nearly always a typo in a JSONPath, and an overlay that
	// silently does nothing ships an SDK missing the very fix it was written to
	// make. Under strict such an action is reported and the compile refuses;
	// under lax it is not reported at all.
	Lax bool `json:"lax,omitempty"`
}

// withDefaults returns a copy of o with unset fields filled from the defaults.
func (o Options) withDefaults() Options {
	if o.Grouping == "" {
		o.Grouping = GroupByTags
	}
	o.Limits = o.Limits.withDefaults()
	return o
}

// The option names Options answers to as text. They are this compiler's own
// vocabulary: a caller writes them without knowing which compiler reads them,
// and nothing above this package holds the list.
const (
	optGrouping          = "grouping"
	optAllowExternalRefs = "allow-external-refs"
	optOverlay           = "overlay"
	optOverlayLax        = "overlay-lax"
)

// optionNames lists the vocabulary, sorted, for the error that reports a name
// outside it. Deriving the list from one place is what keeps the error honest
// as the vocabulary grows.
func optionNames() []string {
	names := []string{optGrouping, optAllowExternalRefs, optOverlay, optOverlayLax}
	slices.Sort(names)
	return names
}

// DecodeOptions implements compilers.Compiler: it turns textual settings into
// an Options value. Settings are read in sorted order so that a set with two
// bad values always reports the same one.
func (*Compiler) DecodeOptions(set compilers.OptionSet) (any, error) {
	var decode optionDecode
	for _, key := range slices.Sorted(maps.Keys(set.Settings)) {
		if err := decode.set(key, set.Settings[key]); err != nil {
			return nil, err
		}
	}
	opts, err := decode.finish(set.ReadFile)
	if err != nil {
		return nil, err
	}
	return opts, nil
}

// optionDecode is an Options under construction. The overlay is described by two
// settings — the document and how strictly to apply it — so it is assembled once
// every setting has been read, and neither order of the two changes the result.
type optionDecode struct {
	opts        Options
	overlayPath string
	lax         bool
	laxSet      bool
}

// set reads one setting.
func (d *optionDecode) set(key, value string) error {
	switch key {
	case optGrouping:
		return decodeGrouping(&d.opts.Grouping, value)
	case optAllowExternalRefs:
		return decodeBool(&d.opts.AllowExternalRefs, key, value)
	case optOverlay:
		// An empty path is refused rather than read as "no overlay". Taking it
		// would apply none while the caller believes one was applied, and would
		// then blame optOverlayLax for applying without an overlay the caller
		// did in fact name.
		if value == "" {
			return fmt.Errorf("openapi: option %q: want a file path, got an empty value", optOverlay)
		}
		d.overlayPath = value
		return nil
	case optOverlayLax:
		d.laxSet = true
		return decodeBool(&d.lax, key, value)
	default:
		return fmt.Errorf("openapi: unknown option %q (known: %s)",
			key, strings.Join(optionNames(), ", "))
	}
}

// finish assembles the decoded Options, loading the overlay document a setting
// named through the caller's reader.
func (d *optionDecode) finish(readFile func(name string) ([]byte, error)) (Options, error) {
	if d.overlayPath == "" {
		if d.laxSet {
			return Options{}, fmt.Errorf("openapi: option %q applies only with %q",
				optOverlayLax, optOverlay)
		}
		return d.opts, nil
	}
	if readFile == nil {
		return Options{}, fmt.Errorf("openapi: option %q names a file and the caller supplied no reader",
			optOverlay)
	}
	data, err := readFile(d.overlayPath)
	if err != nil {
		return Options{}, fmt.Errorf("openapi: option %q: %w", optOverlay, err)
	}
	d.opts.Overlay = &Overlay{Path: d.overlayPath, Data: data, Lax: d.lax}
	return d.opts, nil
}

// decodeGrouping reads a grouping strategy by its own name, which is the same
// string the JSON form uses.
func decodeGrouping(out *GroupingStrategy, value string) error {
	switch strategy := GroupingStrategy(value); strategy {
	case GroupByTags, GroupByPathPrefix:
		*out = strategy
		return nil
	default:
		return fmt.Errorf("openapi: option %q: want %q or %q, got %q",
			optGrouping, GroupByTags, GroupByPathPrefix, value)
	}
}

// decodeBool reads a boolean setting, spelled as strconv.ParseBool accepts it.
func decodeBool(out *bool, key, value string) error {
	parsed, err := strconv.ParseBool(value)
	if err != nil {
		return fmt.Errorf("openapi: option %q: want a boolean, got %q", key, value)
	}
	*out = parsed
	return nil
}
