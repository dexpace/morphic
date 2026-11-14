// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package load

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"

	"github.com/speakeasy-api/openapi/marshaller"
	soa "github.com/speakeasy-api/openapi/openapi"
	"github.com/speakeasy-api/openapi/yml"
	yaml "go.yaml.in/yaml/v3"

	"github.com/dexpace/morphic/compilers"
	"github.com/dexpace/morphic/compilers/openapi/internal/bind/typed"
	"github.com/dexpace/morphic/compilers/openapi/internal/budget"
	"github.com/dexpace/morphic/compilers/openapi/internal/diag"
	"github.com/dexpace/morphic/compilers/openapi/internal/nodeview"
	"github.com/dexpace/morphic/compilers/openapi/internal/oas"
	"github.com/dexpace/morphic/compilers/openapi/internal/overlay"
	"github.com/dexpace/morphic/compilers/openapi/internal/provenance"
	refscope "github.com/dexpace/morphic/compilers/openapi/internal/resolve"
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
	// MaxSourceBytes bounds the size in bytes of the source, of the overlay and
	// of each document a reference reads, each checked before anything reads
	// it. The compiler's public Limits resolves its defaults and its spelling of
	// "unbounded" before projecting onto this (budget.FromOption), so the zero
	// Limit, unbounded, is a budget no caller set, as in each budget below.
	MaxSourceBytes budget.Limit
	// MaxSourceNodes bounds the nodes a document holds as it is read: the
	// source, the overlay and each document a reference reads, and the source
	// again as the overlay leaves it. A JSON text is held to it as it is read,
	// and a YAML text once it is parsed (doctree.Read).
	MaxSourceNodes budget.Limit
	// MaxAliasSurplus bounds the nodes YAML aliases may add to the source, and to
	// the overlay, beyond their own. The ratio refusal scan makes beside it holds
	// either way.
	MaxAliasSurplus budget.Limit
	// MaxExternalDocuments bounds the documents external references read in one
	// compile, each counted once however often it is asked for.
	MaxExternalDocuments budget.Limit
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
}

// readyForModel clears the name of each anchor in the model's source tree, in
// one walk. root must not be nil.
//
// The parser skips an anchored mapping entry where the model folds entries into
// a map, and drops it silently (docs/parser-library.md#anchored-entry-dropped,
// GitHub #459). That skip is the only read of a name; an alias reaches its
// target by a kept pointer. Run it after the pre-parse refusals, which read
// names. A document read through external is readied too (GitHub #501).
func readyForModel(ctx context.Context, root *yaml.Node) {
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

// Document is the successful output of the load phase: a parsed speakeasy
// document, where each of its references lands, and the identity metadata the
// rest of the compiler needs.
// A nil *Document with error-severity diagnostics means the source is a spec
// problem the compiler refuses to lower (e.g. an unsupported version). The
// normalized "openapi" + major.minor format reaches the IR through
// Source.Format alone; Document does not separately carry a
// compilers.SourceFormat, since nothing downstream ever read one.
type Document struct {
	Doc    *soa.OpenAPI  // parsed document, whose references Bindings resolve
	Source ir.SourceInfo // the entry Document.Sources records for this source
	// Location is where the source lies, as its references are read against
	// (see Location), and Places where every file the compile names lies.
	Location string
	Places   *refscope.Places
	// Bindings answers where each $ref lands and where its chain ends, a
	// schema's or a Reference Object's, as the compiler resolved it (bindAll),
	// and the models there.
	Bindings *typed.Bindings
	// Overlay attributes the positions an applied overlay is answerable for. Its
	// zero value — nothing applied — is the answer for a compile with no overlay.
	Overlay overlay.Origin
	// Siblings holds the fields the source writes beside its path items' $refs,
	// modelled and resolved with the document, and MediaTypes the media types
	// it reuses, which the parser does not model.
	Siblings   Siblings
	MediaTypes MediaTypes
	// Others is every document a reference read beside the source, in the
	// order Document.Sources lists them after the source and its overlay, and
	// Dialects the rows each schema resource root naming its dialect governs.
	Others   Others
	Dialects Dialects
	// Rows is what the table says at the minor the source declares, in the
	// dialect its jsonSchemaDialect names, which its Encoding Objects and its
	// schemas where none names its own (Dialects) are read with, from the table
	// the load judged it against: a document of Others read at that minor in
	// that dialect shares them.
	Rows Rows
}

// Rows is what the rule table says at the minor a document is read at, in a
// dialect: how its schemas read (oas.SchemaRules), what its Encoding Objects
// leave unwritten (oas.EncodingRules), and how its Security Requirement
// Objects name a scheme (oas.SecurityRules).
type Rows struct {
	Schema   oas.SchemaRules
	Encoding oas.EncodingRules
	Security oas.SecurityRules
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
	// A relative Location would leave what a reference names to the working
	// directory, which Location exists to keep out of the compile.
	if src.Location != "" && !filepath.IsAbs(src.Location) {
		return nil, nil, fmt.Errorf("openapi: source %d's location %q is not an absolute path", srcIndex, src.Location)
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
	parsed, err := parsedFor(ctx, src, opts.MaxSourceNodes)
	if d, past := pastNodes(ir.Provenance{Source: srcIndex}, "source", err); past {
		return nil, []ir.Diagnostic{d}, nil
	}
	if err != nil {
		// A parse its context stopped is no source that will not parse.
		return nil, nil, stopped(ctx, fmt.Errorf("openapi: decode source %d: %w: %w", srcIndex, err, ErrParse))
	}
	doc, diags, err := build(ctx, srcIndex, src, parsed, opts)
	if err != nil {
		return nil, nil, err
	}
	return doc, append(streamDiagnostics(srcIndex, sourceStream, parsed.doc.Rest), diags...), nil
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
	places := refscope.NewPlaces(opts.Reader)
	// Where the source lies, and the directory a read is confined to, are
	// placed as every file the compile names is, so a link to the source's
	// directory names the source as its target does.
	if location := Location(src); filepath.IsAbs(location) {
		src.Location = places.File(location)
	}
	opts.Within = places.Directory(opts.Within)
	l := &loading{srcIndex: srcIndex, src: src, opts: opts, read: parsed.doc, root: parsed.doc.Root, places: places}
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

// sourceChecks runs the rules the compiler checks over the source: the
// repeats of a parameter or servers list, each entry read through the
// bindings, as the lowering reads it.
func sourceChecks(ctx context.Context, place provenance.Doc, doc *soa.OpenAPI, sibs Siblings,
	media MediaTypes, bindings *typed.Bindings,
) []ir.Diagnostic {
	return listRepeats(place, sibs, bindings, walkSource(ctx, doc, sibs, media))
}

// defaultIndex indexes a decoded tree under the compiler's node bound. It is
// what Options.buildIndex stands in for when a caller leaves it nil, which
// everything outside this package's tests does.
func defaultIndex(ctx context.Context, root *yaml.Node) sourceindex.Index {
	return sourceindex.Build(ctx, root, sourceindex.MaxIndexedNodes)
}

// refusals indexes a decoded tree once and reports its pre-parse refusals, each
// alongside the others: a shape the parser cannot afford to build
// (shapeRefusals, docs/parser-library.md#alias-cycle among them), a mapping
// carrying a tag the parser faults on (diag.TaggedMapping), and a refusal of
// its own when the document is too large to index in full, each at the node
// tree places or at whole.
//
// The size refusal is here, not in scan: a truncated index answers only in
// part, and an alias allowance derived from a partial node count would refuse
// documents on a bound they never crossed.
func refusals(ctx context.Context, tree provenance.Tree, whole ir.Provenance, root *yaml.Node, opts Options,
) []ir.Diagnostic {
	build := opts.buildIndex
	if build == nil {
		build = defaultIndex
	}

	idx := build(ctx, root)
	if idx.Truncated() {
		return []ir.Diagnostic{diag.Newf(ir.SeverityError, diag.SourceTooLarge, whole,
			"source document exceeds the %d-node bound the pre-parse scan indexes",
			sourceindex.MaxIndexedNodes)}
	}

	diags := shapeRefusals(ctx, tree, whole, idx, opts)
	if tagged := idx.TaggedMappings(); len(tagged) > 0 {
		diags = append(diags, taggedMappingRefusal(tree, scan.Least(ctx, root, tagged)))
	}
	return diags
}

// shapeRefusals reports what of the shape of the tree idx indexes the parser
// cannot afford to build: its nesting (scan.Nesting), its aliases
// (scan.Aliases) and, where neither is refused, the bytes of its nodes'
// pointers (scan.PointerAmplification), which deep nesting and aliasing both
// multiply, so one defect is not reported twice. tree places a finding at a
// node, and whole one about the tree as a whole.
func shapeRefusals(ctx context.Context, tree provenance.Tree, whole ir.Provenance, idx sourceindex.Index, opts Options,
) []ir.Diagnostic {
	var diags []ir.Diagnostic
	if d, deep := scan.Nesting(ctx, tree, idx); deep {
		diags = append(diags, d)
	}
	diags = append(diags, scan.Aliases(ctx, tree, whole, idx, opts.MaxAliasSurplus)...)
	if diag.HasError(diags) {
		return diags
	}
	if d, long := scan.PointerAmplification(whole, idx); long {
		diags = append(diags, d)
	}
	return diags
}

// taggedMappingRefusal builds the diag.TaggedMapping refusal, anchored where
// tree places the mapping — the way the alias refusals are anchored. The parser
// panics on such a mapping at a reference object, where no recover reaches
// (docs/parser-library.md#tagged-mapping-panic).
func taggedMappingRefusal(tree provenance.Tree, n *yaml.Node) ir.Diagnostic {
	return diag.Newf(ir.SeverityError, diag.TaggedMapping, tree.Node(n),
		"mapping is tagged %q; an OpenAPI document limits YAML tags to YAML 1.2's JSON schema ruleset, which tags a mapping %s",
		n.Tag, sourceindex.MapTag)
}

// unmarshal builds a speakeasy document from an already-decoded node tree,
// reproducing soa.Unmarshal with the decode lifted out, so each node keeps its
// line and column through an overlay. The parser builds it behind
// buildRecovered, in one call a cancel waits for
// (docs/parser-library.md#context-unread).
func unmarshal(ctx context.Context, data []byte, root *yaml.Node) (*soa.OpenAPI, error) {
	if len(data) == 0 {
		return nil, errors.New("empty document")
	}
	return buildRecovered(func() (*soa.OpenAPI, error) {
		var out soa.OpenAPI
		out.InitCache()
		out.GetCore().SetConfig(yml.GetConfigFromDoc(data, root))

		if _, err := marshaller.UnmarshalNode(ctx, "", root, &out); err != nil {
			return nil, fmt.Errorf("unmarshal document: %w", err)
		}
		return &out, nil
	})
}

// buildRecovered runs build, the parser's build of a document, behind
// diag.Catch. The parser faults on degenerate input such as a whitespace-only
// document (docs/parser-library.md#recoverable-panic), and a fault is an
// ErrParse error quoting it, which the compiler reports as a source it cannot
// read.
//
// A fault on a goroutine the decoder starts is beyond any barrier, so the one
// shape known to fault there, a tagged mapping at a reference position
// (docs/parser-library.md#tagged-mapping-panic), is refused beforehand (see
// refusals).
func buildRecovered(build func() (*soa.OpenAPI, error)) (*soa.OpenAPI, error) {
	var (
		doc *soa.OpenAPI
		err error
	)
	if text, panicked := diag.Catch(func() { doc, err = build() }); panicked {
		return nil, fmt.Errorf("parser panicked (%s): %w", text, ErrParse)
	}
	return doc, err
}

// rowsAt returns what spec says at the minor version names, in dialect, and
// whether dialect is one spec has no Schema Object rows for
// (oas.Spec.SchemaRules).
func rowsAt(spec oas.Spec, version, dialect string) (rows Rows, unread bool) {
	schema, unread := spec.SchemaRules(version, dialect)
	return Rows{Schema: schema, Encoding: spec.EncodingRules(version), Security: spec.SecurityRules(version)}, unread
}

// supportedMinor returns the normalized major.minor prefix of an OpenAPI version
// string and whether the compiler supports it: a minor the rule table covers
// (oas.ParseMinor), so no document is compiled that the table holds to nothing.
func supportedMinor(version string) (string, bool) {
	m, ok := oas.ParseMinor(version)
	if !ok {
		return "", false
	}
	return m.String(), true
}
