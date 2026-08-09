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
