package load

import (
	"context"
	"encoding/json/jsontext"
	"errors"
	"fmt"

	"github.com/speakeasy-api/openapi/marshaller"
	soa "github.com/speakeasy-api/openapi/openapi"
	"github.com/speakeasy-api/openapi/yml"
	yaml "go.yaml.in/yaml/v3"

	"github.com/dexpace/morphic/compilers"
	"github.com/dexpace/morphic/compilers/openapi/internal/bind/typed"
	"github.com/dexpace/morphic/compilers/openapi/internal/diag"
	"github.com/dexpace/morphic/compilers/openapi/internal/nodeview"
	"github.com/dexpace/morphic/compilers/openapi/internal/oas"
	"github.com/dexpace/morphic/compilers/openapi/internal/overlay"
	"github.com/dexpace/morphic/compilers/openapi/internal/scan"
	"github.com/dexpace/morphic/compilers/openapi/internal/sourceindex"
	"github.com/dexpace/morphic/ir"
)

// Options is what Load needs from the compiler's own options. It is a separate
// type rather than the compiler's, because openapi.Options is public API whose
// shape is a published contract (its own doc comment says why) and most of it
// describes lowering, which nothing here can see.
type Options struct {
	// Reader reads the documents references name beyond the source, or nil to
	// read none, so the zero value reads nothing: a reference leaving the source
	// is reported unresolved. Every read goes through it; the load phase makes
	// none of its own.
	Reader compilers.DocumentReader
	// Within confines Reader's reads to one directory, or "" to confine them
	// nowhere. It is read only when Reader is set.
	Within string
	// Overlay is the OpenAPI Overlay document to apply to the source before the
	// model is built, or nil for none. With empty Data, the document its Path
	// names is read through Reader (readOverlay).
	Overlay *overlay.Options
	// OverlaySrcIndex is the index the overlay document takes in Document.Sources.
	// It is read only when Overlay is set.
	OverlaySrcIndex int
	// MaxSourceBytes bounds the source document's size in bytes. Zero is
	// unbounded: the compiler's public Limits resolves its defaults and translates
	// its own spelling of "unbounded" before projecting onto this, so a budget
	// still zero here is one no caller set.
	MaxSourceBytes int
	// MaxSourceNodes bounds the YAML nodes the source parses to, counted after any
	// overlay is applied. Zero is unbounded, as in MaxSourceBytes.
	MaxSourceNodes int
	// MaxAliasSurplus bounds the nodes YAML aliases may add to the source, and to
	// the overlay, beyond their own. Zero is unbounded, as in MaxSourceBytes; the
	// ratio refusal scan makes beside it holds either way.
	MaxAliasSurplus int
	// MaxExternalDocuments bounds the documents external references read in one
	// compile, each counted once however often it is asked for. Zero is
	// unbounded, as in MaxSourceBytes.
	MaxExternalDocuments int
	// buildIndex builds the pre-parse index over a decoded tree, or nil for the
	// compiler's own node bound. It is unexported because it is this package's
	// test seam: it drives the truncated-index refusal without materializing a
	// document of sourceindex.MaxIndexedNodes nodes, and counts the indexes one
	// load builds. Carrying it here rather than in a package-level variable keeps
	// the bound an input to the stage, so nothing a test does to it is visible to
	// a concurrent load.
	buildIndex func(ctx context.Context, root *yaml.Node) sourceindex.Index
	// entered is told each phase a load enters, before the context is asked, or
	// is nil. It is unexported because it is this package's test seam: a test
	// cancels at the start of a phase through it, and lists the phases a load
	// enters.
	entered func(phase)
	// rebuildDoc rebuilds the model for resolveExternal's second pass, or nil for
	// unmarshal itself. It is unexported because it is this package's test seam:
	// the rebuild repeats an unmarshal that already succeeded on the same bytes
	// and tree, and unmarshal depends on nothing else, so only a substitute
	// reaches the error build returns for a failed rebuild.
	rebuildDoc func(ctx context.Context, data []byte, root *yaml.Node) (*soa.OpenAPI, error)
}

// releaseAnchors clears the anchor name from every node of the model's source
// tree, so the parser folds every entry the document writes. root must not be
// nil.
//
// The parser skips an anchored mapping entry where the model folds entries into
// a map, and drops it silently (docs/parser-library.md#anchored-entry-dropped,
// GitHub #459). That skip is the only read of an anchor's name;
// an alias reaches its target by a kept pointer. Run it after the pre-parse
// refusals, which do read names. A document read through external is released
// too (GitHub #501), and replaces the resolver's own parse on a second
// resolution (GitHub #538).
func releaseAnchors(ctx context.Context, root *yaml.Node) {
	// An alias is a leaf of the walk: a recursive anchor cannot loop it.
	nodeview.Walk{Nodes: maxTreeNodes, Visit: func(n *yaml.Node, _ int) nodeview.Step {
		n.Anchor = ""
		return nodeview.Descend
	}}.Run(ctx, root)
}

// ErrParse marks a hard failure to read a source document: bytes that are not
// YAML, or that fault the parser. It is exported because the compiler above
// converts it into a diagnostic — a document that will not parse is a problem
// with the document, and engine.Run turns a Go error from a compiler into one of
// its own, which the CLI reports on the channel it uses for being invoked wrong.
var ErrParse = errors.New("parse source")

// Document is the successful output of the load phase: a parsed, resolved
// speakeasy document plus the identity metadata the rest of the compiler needs.
// A nil *Document with error-severity diagnostics means the source is a spec
// problem the compiler refuses to lower (e.g. an unsupported version). The
// normalized "openapi" + major.minor format reaches the IR through
// Source.Format alone; Document does not separately carry a
// compilers.SourceFormat, since nothing downstream ever read one.
type Document struct {
	Doc    *soa.OpenAPI  // parsed, reference-resolved document
	Source ir.SourceInfo // the entry Document.Sources records for this source
	// Bindings answers where each schema $ref lands and where its chain ends,
	// as the compiler resolved it (bindSchemas), and the schemas there.
	Bindings *typed.Bindings
	// Holds is resolve.Scope.Holds, answered from an index of the raw YAML
	// the model reads on in, which builds each mapping's index once, whoever
	// asks (GitHub #778).
	Holds func(raw *yaml.Node, token string) bool
	// Overlay attributes the positions an applied overlay is answerable for. Its
	// zero value — nothing applied — is the answer for a compile with no overlay.
	Overlay overlay.Origin
	// Siblings holds the fields the source writes beside its path items' $refs,
	// modelled and resolved with the document.
	Siblings Siblings
	// Others is every document a reference read beside the source, in the
	// order Document.Sources lists them after the source and its overlay.
	Others Others
}

// Load parses, validates, and resolves one source document. Spec problems
// become ir.Diagnostic values; the Go error return is reserved for I/O and
// programmer errors (a hard unmarshal failure). A nil document with diagnostics
// signals a refusal to lower (unsupported version) without aborting the batch.
func Load(ctx context.Context, srcIndex int, src compilers.Source, opts Options) (*Document, []ir.Diagnostic, error) {
	// An overlay sharing the source's index is the one way to get the attribution
	// silently wrong. Every position the overlay introduced would name the source,
	// and Document.Sources would carry an entry nothing references — no diagnostic,
	// and nothing structural to catch it, because both indexes address a declared
	// entry. An index past the end is caught downstream by irverify as the dangling
	// reference it is; this collision is not, so it is checked here.
	if opts.Overlay != nil && opts.OverlaySrcIndex == srcIndex {
		return nil, nil, fmt.Errorf("openapi: overlay source index %d is source %d's own", opts.OverlaySrcIndex, srcIndex)
	}

	// The byte budget is checked before anything reads the bytes, because it is
	// the one bound that can be: every finer measure of the document costs a parse
	// to take (GitHub #75).
	if d, over := OverByteBudget(ir.Provenance{Source: srcIndex}, src.Data, opts.MaxSourceBytes); over {
		return nil, []ir.Diagnostic{d}, nil
	}

	if err := opts.enter(ctx, phaseParse); err != nil {
		return nil, nil, err
	}
	parsed, err := parsedFor(ctx, src)
	if err != nil {
		// A parse its context stopped is no source that will not parse.
		return nil, nil, stopped(ctx, fmt.Errorf("openapi: decode source %d: %w: %w", srcIndex, err, ErrParse))
	}
	doc, diags, err := build(ctx, srcIndex, src, parsed, opts)
	if err != nil {
		return nil, nil, err
	}
	return doc, append(parsed.rest.diagnostics(srcIndex), diags...), nil
}

// build turns the decoded tree of a source's first document into a Document,
// phase by phase (steps), each entered only while ctx is live, and none built
// on once ctx is done. A nil document with error diagnostics is a refusal to
// lower.
//
// It is what Load does after the decode, split from it so that what the decode
// found past the first document is reported on every return path — a refusal
// included — without being read as a refusal itself.
func build(ctx context.Context, srcIndex int, src compilers.Source, parsed *Parsed, opts Options) (*Document, []ir.Diagnostic, error) {
	l := &loading{srcIndex: srcIndex, src: src, opts: opts, root: parsed.root}
	// Every return leaves the tree as the source wrote it.
	defer l.restoreTrees()
	for _, s := range steps() {
		if err := opts.enter(ctx, s.phase); err != nil {
			return nil, nil, err
		}
		refused, err := s.run(l, ctx)
		if err != nil {
			return nil, nil, stopped(ctx, err)
		}
		// A phase its context cut short leaves what it found partial, a
		// refusal among it, so none of it is reported or built on.
		if err := ctx.Err(); err != nil {
			return nil, nil, err
		}
		if refused {
			return nil, l.early, nil
		}
	}
	doc, diags := l.document(parsed.Hash())
	return doc, diags, nil
}

// rebuilder returns the rebuild of the model of root that resolveExternal's
// second pass is handed: o.rebuildDoc, or unmarshal itself.
func (o Options) rebuilder(ctx context.Context, data []byte, root *yaml.Node) func() (*soa.OpenAPI, error) {
	rebuildDoc := o.rebuildDoc
	if rebuildDoc == nil {
		rebuildDoc = unmarshal
	}
	return func() (*soa.OpenAPI, error) { return rebuildDoc(ctx, data, root) }
}

// sourceChecks runs the rules the compiler checks over the resolved source:
// the repeats of a parameter or servers list, and the references refused for
// reading past a path item $ref into the fields beside it, less any resolved
// already reports (see besideRefs).
func sourceChecks(ctx context.Context, at func(jsontext.Pointer) ir.Provenance, doc *soa.OpenAPI, sibs Siblings,
	resolved []ir.Diagnostic,
) []ir.Diagnostic {
	diags := listRepeats(at, sibs, walkSource(ctx, doc, sibs))
	return append(diags, besideRefs(at, sibs, walkSource(ctx, doc, sibs), resolved)...)
}

// defaultIndex indexes a decoded tree under the compiler's node bound. It is
// what Options.buildIndex stands in for when a caller leaves it nil, which
// everything outside this package's tests does.
func defaultIndex(ctx context.Context, root *yaml.Node) sourceindex.Index {
	return sourceindex.Build(ctx, root, sourceindex.MaxIndexedNodes)
}

// locator answers where a node of the patched tree pointers indexes is, for
// every diagnostic anchored on one: the overlay's index and the node's JSON
// pointer for a node the overlay introduced or rewrote — the answer the
// lowering gives for that pointer — and otherwise srcIndex with the node's own
// line and column and its pointer in the tree (scan.InTree). A zero origin, the
// answer for a compile with no overlay, never claims a node, so the two cases
// need no telling apart here.
func locator(srcIndex int, origin overlay.Origin, pointers func(*yaml.Node) (jsontext.Pointer, bool)) scan.Locator {
	inSource := scan.InTree(srcIndex, pointers)
	return func(n *yaml.Node) ir.Provenance {
		if prov, ok := origin.At(n); ok {
			return prov
		}
		return inSource(n)
	}
}

// pointerAt answers where a pointer is, as the lowering answers it
// (lowering.Ctx.ProvenanceAt): the overlay's index for a position the overlay
// introduced or rewrote, srcIndex otherwise. A load diagnostic placed by a
// pointer is placed where a lowering one at the same pointer is, which is what
// lets the compiler tell the two report the same position.
func pointerAt(srcIndex int, origin overlay.Origin) func(jsontext.Pointer) ir.Provenance {
	return func(p jsontext.Pointer) ir.Provenance {
		return ir.Provenance{Source: origin.IndexAt(p, srcIndex), Pointer: p}
	}
}

// refusals indexes a decoded tree once and reports its pre-parse refusals, each
// alongside the others: a document nested too deeply to build (scan.Nesting),
// aliases the parser cannot expand (scan.Aliases,
// docs/parser-library.md#alias-cycle), a mapping carrying a tag the parser
// faults on (diag.TaggedMapping), and a refusal of its own when the document
// is too large to index in full.
//
// The size refusal is here, not in scan, because a truncated index answers only
// in part, and an alias-expansion allowance derived from a partial node count
// would refuse documents on a bound they never crossed.
func refusals(ctx context.Context, locate scan.Locator, root *yaml.Node, opts Options) []ir.Diagnostic {
	build := opts.buildIndex
	if build == nil {
		build = defaultIndex
	}

	idx := build(ctx, root)
	if idx.Truncated() {
		return []ir.Diagnostic{diag.Newf(ir.SeverityError, diag.SourceTooLarge, locate(nil),
			"source document exceeds the %d-node bound the pre-parse scan indexes",
			sourceindex.MaxIndexedNodes)}
	}

	var diags []ir.Diagnostic
	if d, deep := scan.Nesting(locate, idx); deep {
		diags = append(diags, d)
	}
	diags = append(diags, scan.Aliases(ctx, locate, idx, int64(opts.MaxAliasSurplus))...)
	if tagged := idx.TaggedMappings(); len(tagged) > 0 {
		diags = append(diags, taggedMappingRefusal(locate, scan.Least(root, tagged)))
	}
	return diags
}

// taggedMappingRefusal builds the diag.TaggedMapping refusal, anchored where
// locate puts the mapping — the way the alias refusals are anchored. The parser
// panics on such a mapping at a reference object, where no recover reaches
// (docs/parser-library.md#tagged-mapping-panic).
func taggedMappingRefusal(locate scan.Locator, n *yaml.Node) ir.Diagnostic {
	return diag.Newf(ir.SeverityError, diag.TaggedMapping, locate(n),
		"mapping is tagged %q; an OpenAPI document limits YAML tags to YAML 1.2's JSON schema ruleset, which tags a mapping %s",
		n.Tag, sourceindex.MapTag)
}

// unmarshal builds a speakeasy document from an already-decoded node tree,
// reproducing soa.Unmarshal with the decode lifted out, so each node keeps its
// line and column through an overlay.
//
// A cancel waits for it (docs/parser-library.md#context-unread).
//
// A panic from the third-party parser, which faults on degenerate input such as
// a whitespace-only document (docs/parser-library.md#recoverable-panic),
// becomes an ErrParse error. The recover reaches only this goroutine, and the
// parser decodes on goroutines of its own, where a fault ends the process: a
// shape known to fault, a tagged mapping at a reference position
// (docs/parser-library.md#tagged-mapping-panic), is refused beforehand (see
// refusals).
func unmarshal(ctx context.Context, data []byte, root *yaml.Node) (doc *soa.OpenAPI, err error) {
	defer func() {
		if r := recover(); r != nil {
			doc = nil
			err = fmt.Errorf("parser panicked (%v): %w", r, ErrParse)
		}
	}()
	if len(data) == 0 {
		return nil, errors.New("empty document")
	}

	var out soa.OpenAPI
	out.InitCache()
	out.GetCore().SetConfig(yml.GetConfigFromDoc(data, root))

	if _, err = marshaller.UnmarshalNode(ctx, "", root, &out); err != nil {
		return nil, fmt.Errorf("unmarshal document: %w", err)
	}
	return &out, nil
}

// SupportedMinor returns the normalized major.minor prefix of an OpenAPI version
// string and whether the compiler supports it: a minor the rule table covers
// (oas.ParseMinor), so no document is compiled that the table holds to nothing.
func SupportedMinor(version string) (string, bool) {
	m, ok := oas.ParseMinor(version)
	if !ok {
		return "", false
	}
	return m.String(), true
}
