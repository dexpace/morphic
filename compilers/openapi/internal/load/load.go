// Package load turns one source document into a parsed, reference-resolved
// OpenAPI document plus the identity metadata the rest of the compiler stamps
// into the IR.
//
// It sits on the entry side of the pipeline: nothing below it in the compiler
// calls back into it, and it knows nothing about lowering. Spec problems leave
// as ir.Diagnostic values; the Go error return is reserved for I/O and
// programmer errors.
package load

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json/jsontext"
	"errors"
	"fmt"
	"io"
	"iter"
	"strings"

	oas3 "github.com/speakeasy-api/openapi/jsonschema/oas3"
	"github.com/speakeasy-api/openapi/marshaller"
	soa "github.com/speakeasy-api/openapi/openapi"
	"github.com/speakeasy-api/openapi/validation"
	"github.com/speakeasy-api/openapi/yml"
	yaml "gopkg.in/yaml.v3"

	"github.com/dexpace/morphic/compilers"
	"github.com/dexpace/morphic/compilers/openapi/internal/diag"
	"github.com/dexpace/morphic/compilers/openapi/internal/overlay"
	"github.com/dexpace/morphic/compilers/openapi/internal/scan"
	"github.com/dexpace/morphic/compilers/openapi/internal/sourceindex"
	"github.com/dexpace/morphic/compilers/openapi/internal/value"
	"github.com/dexpace/morphic/ir"
)

// Options is what Load needs from the compiler's own options. It is a separate
// type rather than the compiler's, because openapi.Options is public API whose
// shape is a published contract (its own doc comment says why) and most of it
// describes lowering, which nothing here can see.
type Options struct {
	// AllowExternalRefs lets reference resolution reach outside the document —
	// off the filesystem or over the network. Off is the default, so the zero
	// value performs no I/O.
	AllowExternalRefs bool
	// Overlay is the OpenAPI Overlay document to apply to the source before the
	// model is built, or nil for none. Its bytes are the caller's to read, like
	// the source's.
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
	// buildIndex builds the pre-parse index over a decoded tree, or nil for the
	// compiler's own node bound. It is unexported because it is this package's
	// test seam: it drives the truncated-index refusal without materializing a
	// document of sourceindex.MaxIndexedNodes nodes, and counts the indexes one
	// load builds. Carrying it here rather than in a package-level variable keeps
	// the bound an input to the stage, so nothing a test does to it is visible to
	// a concurrent load.
	buildIndex func(root *yaml.Node) sourceindex.Index
	// rebuildDoc rebuilds the model for resolveExternal's second pass, or nil for
	// unmarshal itself. It is unexported because it is this package's test seam,
	// for the same reason buildIndex is one: unmarshal is a pure function of the
	// (ctx, data, root) build's rebuild closure always calls it with, the exact
	// arguments its first, already-successful call in this same build used, so
	// that closure can never observe unmarshal fail. A test substitutes this to
	// drive the rebuild-source error return in resolve's caller regardless.
	rebuildDoc func(ctx context.Context, data []byte, root *yaml.Node) (*soa.OpenAPI, []error, error)
}

// exceeds reports whether an observed count crosses limit, treating a zero or
// negative limit as unbounded. Both budgets are read through it so that "no
// budget set" cannot be spelled two ways.
func exceeds(observed, limit int) bool {
	return limit > 0 && observed > limit
}

// budgetRefusal builds the diagnostic that refuses a source for crossing one of
// this phase's size budgets. Provenance names the source and no position within
// it: what crossed the budget is the document, and no line in it is more
// answerable than any other.
func budgetRefusal(srcIndex int, format string, observed, limit int) ir.Diagnostic {
	return diag.Newf(ir.SeverityError, diag.BudgetExceeded,
		ir.Provenance{Source: srcIndex}, format, observed, limit)
}

// OverByteBudget reports whether a source of data is past the byte budget limit,
// zero being none, and if so the refusal to report at prov. It is exported for
// detection, which reads a source before Load does and is held to the same
// budget with the same words, so a source refused at either reads alike.
func OverByteBudget(prov ir.Provenance, data []byte, limit int) (ir.Diagnostic, bool) {
	if !exceeds(len(data), limit) {
		return ir.Diagnostic{}, false
	}
	return diag.Newf(ir.SeverityError, diag.BudgetExceeded, prov,
		"source document is %d bytes, past the %d-byte budget", len(data), limit), true
}

// releaseAnchors clears the anchor name from every node of the tree the model is
// about to be built from, so the parser folds every entry the document writes.
//
// The parser skips a mapping entry whose value carries an anchor wherever a
// model folds its entries into a map — the paths, a path item's operations, the
// responses, a callback's expressions — taking the anchor for an alias
// definition rather than a value (speakeasy-api/openapi v1.25.2,
// marshaller/unmarshaller.go). In OpenAPI such an entry is an entry like any
// other, and skipping it dropped a whole operation, response or callback from
// the IR with no diagnostic, while the same document without the anchor
// compiled it (GitHub #459).
//
// Clearing the name changes nothing else the parser reads. That skip is the
// only place it reads an anchor's name at all; an alias reaches its target
// through the pointer yaml.v3 resolves it to, which is kept, so every alias
// still stands for what it named. It runs after every pre-parse refusal, which
// do read names — the recursive-anchor refusal quotes the one it found — and
// nothing after the parse reads one.
//
// A document an external reference names is read by the resolver rather than
// by Load, and external releases its anchors the same way, after the same
// refusals, before the resolver builds a model from it (GitHub #501).
//
// The walk follows Content and never an alias, so it visits each node of the
// tree once and cannot cycle through a recursive anchor, which the refusals
// have rejected by now in any case. root is never nil — a source with no
// document decodes to an empty node — and yaml.v3 leaves no nil in Content.
func releaseAnchors(root *yaml.Node) {
	stack := []*yaml.Node{root}
	for len(stack) > 0 {
		n := stack[len(stack)-1]
		stack = stack[:len(stack)-1]
		n.Anchor = ""
		stack = append(stack, n.Content...)
	}
}

// ErrParse marks a hard failure to read a source document: bytes that are not
// YAML, or that fault the parser. It is exported because the compiler above
// converts it into a diagnostic — a document that will not parse is a problem
// with the document, and engine.Run turns a Go error from a compiler into one of
// its own, which the CLI reports on the channel it uses for being invoked wrong.
var ErrParse = errors.New("parse source")

// maxSchemaScanDepth bounds the scalar scan of a schema node (styleguide
// bounded-recursion rule); a schema nested deeper is pathological, not a spec the
// compiler is expected to classify.
const maxSchemaScanDepth = 512

// Document is the successful output of the load phase: a parsed, resolved
// speakeasy document plus the identity metadata the rest of the compiler needs.
// A nil *Document with error-severity diagnostics means the source is a spec
// problem the compiler refuses to lower (e.g. an unsupported version). The
// normalized "openapi" + major.minor format reaches the IR through
// Source.Format alone; Document does not separately carry a
// compilers.SourceFormat, since nothing downstream ever read one.
type Document struct {
	Doc    *soa.OpenAPI  // parsed, reference-resolved document
	Source ir.SourceInfo // format tag, path, content hash
	// Overlay attributes the positions an applied overlay is answerable for. Its
	// zero value — nothing applied — is the answer for a compile with no overlay.
	Overlay overlay.Origin
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

	parsed, err := parsedFor(src)
	if err != nil {
		return nil, nil, fmt.Errorf("openapi: decode source %d: %w: %w", srcIndex, err, ErrParse)
	}
	doc, diags, err := build(ctx, srcIndex, src, parsed, opts)
	if err != nil {
		return nil, nil, err
	}
	return doc, append(parsed.rest.diagnostics(srcIndex), diags...), nil
}

// build turns the decoded tree of a source's first document into a Document:
// the pre-parse refusals, the overlay, the node budget, the model build, the
// version check, then validation findings and reference resolution as
// diagnostics. A nil document with error diagnostics is a refusal to lower.
//
// It is what Load does after the decode, split from it so that what the decode
// found past the first document is reported on every return path — a refusal
// included — without being read as a refusal itself.
func build(ctx context.Context, srcIndex int, src compilers.Source, parsed *Parsed, opts Options) (*Document, []ir.Diagnostic, error) {
	root := parsed.root
	cyc := refusals(scan.InSource(srcIndex), root, opts)
	if diag.HasError(cyc) {
		return nil, cyc, nil // degenerate cycle: refuse to lower, do not crash the parser
	}
	// cyc may still hold a non-fatal scan-incomplete warning; carry it forward.

	origin, patchDiags := patch(srcIndex, root, opts)
	cyc = append(cyc, patchDiags...)
	if diag.HasError(cyc) {
		return nil, cyc, nil // an overlay that did not apply: refuse to lower a half-patched tree
	}

	// The node budget is checked after the overlay for the reason patch re-runs
	// the pre-parse refusals: the tree the model is built from is no longer the
	// bytes that were measured above, and an overlay action can graft nodes onto
	// it. Building that model and resolving its references is what the budget
	// exists to bound, and both are still ahead.
	if nodes := nodeCount(root); exceeds(nodes, opts.MaxSourceNodes) {
		return nil, append(cyc, budgetRefusal(srcIndex,
			"source document parses to %d nodes, past the %d-node budget",
			nodes, opts.MaxSourceNodes)), nil
	}

	releaseAnchors(root)
	doc, valErrs, err := unmarshal(ctx, src.Data, root)
	if err != nil {
		return nil, nil, fmt.Errorf("openapi: unmarshal source %d: %w", srcIndex, err)
	}

	minor, ok := SupportedMinor(doc.OpenAPI)
	if !ok {
		return nil, append(cyc, diag.Newf(ir.SeverityError, diag.UnsupportedVersion,
			ir.Provenance{Source: srcIndex},
			"unsupported OpenAPI version %q; want 3.0, 3.1, or 3.2", doc.OpenAPI)), nil
	}

	diags := cyc
	diags = append(diags, findings(ctx, locator(srcIndex, origin), doc, valErrs, minor)...)
	rebuildDoc := opts.rebuildDoc
	if rebuildDoc == nil {
		rebuildDoc = unmarshal
	}
	rebuild := func() (*soa.OpenAPI, error) {
		again, _, err := rebuildDoc(ctx, src.Data, root)
		return again, err
	}
	doc, resolveDiags, err := resolve(ctx, pointerAt(srcIndex, origin), doc, src.Path, opts, rebuild)
	if err != nil {
		return nil, nil, fmt.Errorf("openapi: rebuild source %d: %w", srcIndex, err)
	}
	diags = append(diags, resolveDiags...)

	return &Document{
		Doc: doc,
		Source: ir.SourceInfo{
			Format: "openapi@" + minor,
			Path:   src.Path,
			Hash:   parsed.Hash(),
		},
		Overlay: origin,
	}, diags, nil
}

// findings converts the model build's validation errors into diagnostics,
// dropping the two kinds that are library artifacts rather than spec problems:
// a numeric literal Morphic captures losslessly anyway, and a schema finding
// raised only because the library checked against the wrong meta-schema.
func findings(ctx context.Context, locate scan.Locator, doc *soa.OpenAPI, valErrs []error, minor string) []ir.Diagnostic {
	wrongMetaSchema := metaSchemaVersionArtifacts(ctx, doc, minor)
	diags := make([]ir.Diagnostic, 0, len(valErrs))
	for _, ve := range valErrs {
		if verr, ok := asValidationError(ve); ok &&
			(numericLiteralArtifact(verr) || wrongMetaSchema[findingSite(verr)]) {
			continue
		}
		diags = append(diags, validationDiag(locate, ve))
	}
	return diags
}

// resolve resolves every reference in doc and reports what each resolution
// found at the $ref that produced it (see resolveWith). It returns the document
// it resolved, which is a rebuild of doc when doc's own resolution parsed an
// external document the compiler had prepared under another key (see
// resolveExternal).
func resolve(ctx context.Context, at func(jsontext.Pointer) ir.Provenance, doc *soa.OpenAPI, path string,
	opts Options, rebuild func() (*soa.OpenAPI, error),
) (*soa.OpenAPI, []ir.Diagnostic, error) {
	if !opts.AllowExternalRefs {
		return doc, resolveWith(ctx, at, doc, path, opts, nil), nil
	}
	return resolveExternal(ctx, at, doc, path, opts, rebuild)
}

// defaultIndex indexes a decoded tree under the compiler's node bound. It is
// what Options.buildIndex stands in for when a caller leaves it nil, which
// everything outside this package's tests does.
func defaultIndex(root *yaml.Node) sourceindex.Index {
	return sourceindex.Build(root, sourceindex.MaxIndexedNodes)
}

// locator answers where a raw node is, for every diagnostic anchored on one:
// the overlay's index and the node's JSON pointer for a node the overlay
// introduced or rewrote — the answer the lowering gives for that pointer — and
// srcIndex with the node's own line and column otherwise. A zero origin, the
// answer for a compile with no overlay, never claims a node, so the two cases
// need no telling apart here.
func locator(srcIndex int, origin overlay.Origin) scan.Locator {
	inSource := scan.InSource(srcIndex)
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
		return ir.Provenance{Source: origin.IndexAt(p, srcIndex), Pointer: string(p)}
	}
}

// refusals indexes a decoded tree once and reports the pre-parse refusals over
// it: the degenerate reference and alias structures scan finds, a mapping
// carrying a tag the parser faults on, or — when the document is too large to
// index in full — a refusal of its own. Each is anchored where locate puts its
// node.
//
// The size refusal is here rather than in scan because every answer in a
// truncated index is a partial one, and the alias-expansion allowance derived
// from a partial node count would refuse documents on a bound they never
// crossed. A document that large is beyond what the pre-parse guarantees cover,
// so it is refused rather than lowered on incomplete information.
//
// The tag refusal is here because it is read straight off the index; what it
// guards is stated on diag.TaggedMapping. It is reported alongside a cycle
// rather than instead of one, so a document with both hears about both.
func refusals(locate scan.Locator, root *yaml.Node, opts Options) []ir.Diagnostic {
	build := opts.buildIndex
	if build == nil {
		build = defaultIndex
	}

	idx := build(root)
	if idx.Truncated() {
		return []ir.Diagnostic{diag.Newf(ir.SeverityError, diag.SourceTooLarge, locate(nil),
			"source document exceeds the %d-node bound the pre-parse scan indexes",
			sourceindex.MaxIndexedNodes)}
	}

	diags := scan.Cycles(locate, idx, int64(opts.MaxAliasSurplus))
	if n, ok := idx.TaggedMapping(); ok {
		diags = append(diags, taggedMappingRefusal(locate, n))
	}
	return diags
}

// taggedMappingRefusal builds the diag.TaggedMapping refusal, anchored where
// locate puts the mapping — the way the cycle refusals are anchored.
func taggedMappingRefusal(locate scan.Locator, n *yaml.Node) ir.Diagnostic {
	return diag.Newf(ir.SeverityError, diag.TaggedMapping, locate(n),
		"mapping is tagged %q; an OpenAPI document limits YAML tags to YAML 1.2's JSON schema ruleset, which tags a mapping %s",
		n.Tag, sourceindex.MapTag)
}

// patch applies the caller's overlay to the decoded tree, or does nothing when
// there is none. The overlay document is refused first if the library would
// follow its aliases without end (overlayRefusals); applyOverlay does the rest.
func patch(srcIndex int, root *yaml.Node, opts Options) (overlay.Origin, []ir.Diagnostic) {
	if opts.Overlay == nil {
		return overlay.Origin{}, nil
	}
	return applyOverlay(srcIndex, root, opts, overlayRefusals(opts))
}

// applyOverlay applies the overlay given what its pre-apply refusals found,
// which it takes as data so what it does with each outcome is a function of
// that outcome and can be held to it directly.
//
// An error there refuses before the library is handed anything. Anything less
// is carried into what the compile reports: the only such finding is the
// scan's own report that it faulted, and that says the overlay's protection
// from the library is incomplete — exactly what a caller must hear before
// trusting a compile that went on regardless.
//
// It re-runs the pre-parse refusals over the result, because the tree that
// reaches the parser is no longer the one they first saw: an overlay action can
// graft a $ref cycle onto a document that had none, and the guarantee those
// refusals exist for is about what the parser is handed. That run locates
// through the attribution the application produced, so a refusal on a node the
// overlay grafted names the overlay rather than the source at a position the
// node does not have.
func applyOverlay(srcIndex int, root *yaml.Node, opts Options, pre []ir.Diagnostic) (overlay.Origin, []ir.Diagnostic) {
	if diag.HasError(pre) {
		return overlay.Origin{}, pre
	}
	// The overlay builds within the same node budget the patched tree is checked
	// against afterwards, so it can refuse an action before paying for it.
	ov := *opts.Overlay
	ov.MaxNodes = opts.MaxSourceNodes
	origin, diags := overlay.Apply(opts.OverlaySrcIndex, root, ov)
	diags = append(pre, diags...)
	if diag.HasError(diags) {
		return overlay.Origin{}, diags
	}
	return origin, append(diags, refusals(locator(srcIndex, origin), root, opts)...)
}

// overlayRefusals refuses an overlay document whose aliases the library would
// follow without end or far past its size, before the library is handed it.
//
// The overlay library copies each update by cloning it, and its clone follows
// an alias into what the alias names. An anchor naming one of its own ancestors
// gave that recursion no base case and ended the process with a stack overflow;
// an alias bomb gave it an exponential one and ended it out of memory — a
// 393-byte overlay cost 355 MB at six levels. Neither is a panic, so the barrier
// around the application cannot see them (GitHub #489). They are the two
// refusals the source gets before its own parser, held to the same ratio and
// the same alias budget, and they run here because this is the one package
// that reaches them.
//
// The overlay is decoded into a node tree of its own for this, which expands
// nothing — a tree holds an alias as one node pointing at its anchor — and the
// library then decodes it again. An overlay is a patch, so the second decode
// costs little; the alternative was a scan over each update value alone, which
// cannot see a cycle whose anchor sits outside the value naming it.
//
// Bytes that will not decode are left to the library, which refuses them with
// its own reason; this answers only about documents that do.
func overlayRefusals(opts Options) []ir.Diagnostic {
	// The byte budget is checked before anything reads the bytes, as the
	// source's is (GitHub #75): an overlay is an input document like the source,
	// read in full by the decode below and again by the library, and it had no
	// bound on its size at all (GitHub #491).
	if exceeds(len(opts.Overlay.Data), opts.MaxSourceBytes) {
		return []ir.Diagnostic{budgetRefusal(opts.OverlaySrcIndex,
			"overlay document is %d bytes, past the %d-byte budget",
			len(opts.Overlay.Data), opts.MaxSourceBytes)}
	}
	var tree yaml.Node
	if err := yaml.Unmarshal(opts.Overlay.Data, &tree); err != nil {
		return nil
	}
	build := opts.buildIndex
	if build == nil {
		build = defaultIndex
	}
	locate := scan.InSource(opts.OverlaySrcIndex)
	idx := build(&tree)
	if idx.Truncated() {
		return []ir.Diagnostic{diag.Newf(ir.SeverityError, diag.SourceTooLarge, locate(nil),
			"overlay document exceeds the %d-node bound the pre-parse scan indexes",
			sourceindex.MaxIndexedNodes)}
	}
	return scan.Aliases(locate, idx, int64(opts.MaxAliasSurplus))
}

// metaSchemaReconciledMinor is the OpenAPI minor whose schema findings are
// reconciled against the document's own meta-schema.
//
// The library defaults to the 3.1 meta-schema when no version reaches schema
// validation (jsonschema/oas3/validation.go), so a 3.1 document is checked
// correctly by accident and needs no reconciling. A 3.0 document is mis-checked
// in the other direction, and is deliberately left alone: the 3.0 meta-schema
// words the same defect differently and raises findings 3.1 does not, so
// reconciling it is a change to what a 3.0 document reports rather than the
// removal of a false positive, and belongs to its own change.
const metaSchemaReconciledMinor = "3.2"

// metaSchemaVersionArtifacts returns the validation findings the library produced
// only because it checked schema objects against the wrong meta-schema, keyed by
// findingSite.
//
// JSONSchema[T].Validate discards its options and calls Schema.Validate with none
// (jsonschema/oas3/jsonschema.go, under a "for now" comment), so the
// ParentDocumentVersion the document walk supplies never reaches schema
// validation: every schema object is checked against the 3.1 meta-schema whatever
// the document claims to be. A conformant 3.2 document writing a 3.2-only schema
// keyword — discriminator.defaultMapping is the one that surfaced — then fails
// against a meta-schema that does not declare it, and under the CLI's default
// --fail-on error a valid spec exits 1.
//
// Which findings to drop is derived, not listed. oas3.Validate does honour the
// option, so validating every schema twice — once at the document's own version,
// once as the library does — bounds the artifact exactly: a finding the second run
// raises and the first does not is the library checking against a meta-schema the
// document never claimed. Everything else is kept, including a finding on a schema
// this walk did not reach, which appears in neither run and so is never in the
// difference. Nothing names a keyword, so a 3.2 addition beyond defaultMapping is
// covered without an edit — and once the library stops dropping its options the
// two runs agree and this drops nothing.
func metaSchemaVersionArtifacts(ctx context.Context, doc *soa.OpenAPI, minor string) map[string]bool {
	if minor != metaSchemaReconciledMinor {
		return nil
	}
	version := doc.OpenAPI
	atDocumentVersion := schemaFindings(ctx, doc,
		validation.WithContextObject(&oas3.ParentDocumentVersion{OpenAPI: &version}))
	asLibraryChecks := schemaFindings(ctx, doc)
	for key := range atDocumentVersion {
		delete(asLibraryChecks, key)
	}
	return asLibraryChecks
}

// schemaFindings validates every schema object the document walk reaches under
// opts, returning each finding's site.
func schemaFindings(ctx context.Context, doc *soa.OpenAPI, opts ...validation.Option) map[string]bool {
	out := map[string]bool{}
	matchSchemas(soa.Walk(ctx, doc), func(js *oas3.JSONSchema[oas3.Referenceable]) error {
		for _, found := range oas3.Validate(ctx, js, opts...) {
			if verr, ok := asValidationError(found); ok {
				out[findingSite(verr)] = true
			}
		}
		return nil
	})
	return out
}

// matchSchemas dispatches every walk item to collect, stopping at the first match
// that reports an error.
//
// Stopping is safe rather than silent. The two runs this feeds walk the same
// document the same way, so a schema neither run reached contributes to neither
// set and can never fall into the difference between them: what is left is a
// smaller reconciliation, not a wrong one. Propagating the error instead would
// only give the caller the same choice, with a branch no input can take —
// Match returns exactly what the matcher handed it, and collect returns nil.
func matchSchemas(items iter.Seq[soa.WalkItem], collect func(*oas3.JSONSchema[oas3.Referenceable]) error) {
	for item := range items {
		if err := item.Match(soa.Matcher{Schema: collect}); err != nil {
			return
		}
	}
}

// findingSite identifies a validation finding by its rule and position, not by
// its rendered message.
//
// Two meta-schemas word the same defect differently — 3.0 omits 'null' from the
// admissible type list 3.1 prints — so comparing messages would read a rewording
// as a disappearance and drop a real finding. Rule and position are what both runs
// agree on when they are describing the same thing.
func findingSite(verr validation.Error) string {
	return fmt.Sprintf("%s\x00%s\x00%d\x00%d",
		verr.Rule, verr.GetDocumentLocation(), verr.GetLineNumber(), verr.GetColumnNumber())
}

// numericBoundKeywords are the schema keywords the library binds to *float64 and
// therefore mis-validates when their literal exceeds float64 range or uses a
// valid non-JSON spelling. Morphic reads these from the raw nodes instead.
var numericBoundKeywords = map[string]struct{}{
	"minimum":          {},
	"maximum":          {},
	"exclusiveMinimum": {},
	"exclusiveMaximum": {},
	"multipleOf":       {},
}

// numericLiteralArtifact reports whether a library validation finding must be
// dropped because Morphic, not the library's float64 model, is authoritative for
// numeric-bound keywords: a type-mismatch on such a keyword is always Morphic's
// to own, and an invalid-syntax finding is dropped only when caused solely by a
// recoverable non-JSON spelling (.5), never a genuinely unrepresentable literal
// (.inf).
//
// This is coupled to the speakeasy library's finding shape — TypeMismatchError's
// parent path and the offending node's YAML tag. A library upgrade that changes
// either must be revalidated against the numeric-precision conformance corpus.
func numericLiteralArtifact(verr validation.Error) bool {
	switch verr.Rule {
	case validation.RuleValidationTypeMismatch:
		return isNumericBoundKeyword(verr)
	case validation.RuleValidationInvalidSyntax:
		return invalidSyntaxOnValidNumbers(verr.GetNode())
	default:
		return false
	}
}

// isNumericBoundKeyword reports whether a type-mismatch finding concerns one of
// the numeric-bound keywords Morphic reads from the raw node, identified by the
// trailing segment of the mismatch's parent path. The library emits both a
// meta-schema and an unmarshal finding for one bad bound; both carry the keyword
// in their parent path, so both are recognized and suppressed together.
func isNumericBoundKeyword(verr validation.Error) bool {
	var mismatch *validation.TypeMismatchError
	if !errors.As(verr.UnderlyingError, &mismatch) {
		return false
	}
	name := mismatch.ParentName
	if i := strings.LastIndexByte(name, '.'); i >= 0 {
		name = name[i+1:]
	}
	_, ok := numericBoundKeywords[name]
	return ok
}

// invalidSyntaxOnValidNumbers reports whether an invalid-syntax finding is caused
// solely by numeric literals written in a spelling JSON rejects (.5, 5., 0644)
// that Morphic captures losslessly anyway.
//
// A literal is only a candidate cause when its source text fails IsValid —
// that is the grammar the library's YAML-to-JSON conversion has to satisfy, so a
// spelling JSON already accepts provoked nothing and cannot excuse the finding.
// Every rejected literal must then be one value.NumericLiteral recovers, the very
// conversion the lowerer will run; a single unrecoverable one (.inf) keeps the
// finding. Because the scan covers every numeric scalar under the node, no
// candidate cause goes unclassified.
func invalidSyntaxOnValidNumbers(node *yaml.Node) bool {
	if node == nil {
		return false
	}
	var recovered, unrepresentable bool
	walkNumericScalars(node, 0, func(scalar *yaml.Node) {
		if jsontext.Value(scalar.Value).IsValid() {
			return
		}
		if _, err := value.NumericLiteral(scalar); err != nil {
			unrepresentable = true
			return
		}
		recovered = true
	})
	return recovered && !unrepresentable
}

// walkNumericScalars visits every numeric-tagged scalar reachable from node
// within maxSchemaScanDepth. Only a numeric spelling has been observed to defeat
// the library's YAML-to-JSON conversion — a custom tag or a non-string key
// converts fine — so non-numeric scalars are skipped.
func walkNumericScalars(node *yaml.Node, depth int, visit func(*yaml.Node)) {
	if node == nil || depth > maxSchemaScanDepth {
		return
	}
	if node.Kind == yaml.ScalarNode {
		if node.Tag == "!!int" || node.Tag == "!!float" {
			visit(node)
		}
		return
	}
	for _, child := range node.Content {
		walkNumericScalars(child, depth+1, visit)
	}
}

// nodeCount returns the number of nodes in the parsed tree rooted at root.
//
// The parse tree is a tree rather than a graph — aliasing only adds edges this
// walk never follows, and yaml.v3 gives an alias node empty Content — so the
// iterative stack visits each node once and is bounded by the tree's own size.
//
// The alias-amplification budget in scan takes the same measure of the same
// tree, and the two are deliberately not shared: a ten-line walk over a
// third-party node type is not a dependency worth adding between two packages
// with different jobs, and this repo has no utility package to put it in.
func nodeCount(root *yaml.Node) int {
	if root == nil {
		return 0
	}
	count := 0
	stack := []*yaml.Node{root}
	for len(stack) > 0 {
		n := stack[len(stack)-1]
		stack = stack[:len(stack)-1]
		count++
		stack = append(stack, n.Content...)
	}
	return count
}

// decodeStream parses source bytes into the node tree of the document the
// compile lowers — the first in the stream that holds content — and reads what
// follows it so a stream of several is reported rather than silently cut to
// one. The error it returns is the parser's own: Decode hands it to detection,
// which quotes it, and Load wraps it as ErrParse.
//
// It is split out from unmarshal so an overlay can be applied to the tree
// between the two: the alternative — overlaying, re-serialising and re-parsing —
// renumbers every line in the document, and every diagnostic about the source
// would then name a position in a file that exists nowhere.
//
// It is the compile's only parse of the source: the pre-parse refusals used to
// decode the same bytes a second time to scan them, and now read the tree this
// produces. Nothing here bounds alias expansion, and nothing needs to: a node
// tree holds an alias as one node pointing at its anchor, so decoding into one
// expands nothing, and yaml.v3's excessive-aliasing guard — which counts
// expansions — never fires for a Node target. What refuses a billion-laughs
// document is scan's weigher over this tree.
//
// It carries no recover of its own, unlike the model build and the resolve
// below it. yaml.v3 converts its own faults into errors before they leave
// Decode; the third-party code that has been seen to fault is the layer above
// the decode, which is where the barriers are.
func decodeStream(data []byte) (*yaml.Node, tail, error) {
	dec := yaml.NewDecoder(bytes.NewReader(data))
	root, err := firstWithContent(dec)
	if err != nil {
		return nil, tail{}, err
	}
	return root, readTail(dec), nil
}

// Parsed is a source's decoded form, produced once and used twice: detection
// reads the document's own keys off it to name the format, and the compile it
// routes to lowers that same tree rather than reading the bytes again. The two
// happen back to back over one source, so parsing in both is parsing twice.
//
// It is the value this compiler puts in compilers.Source.Parsed, and the only
// type it reads back out of one. Holding the digest of the bytes it came from
// is what lets a reader check that: a Parsed reached its Compile beside the
// Data it describes, or it is ignored and the bytes are read afresh.
//
// The digest is what SourceInfo.Hash records, so the hash a document carries is
// the hash of the bytes that document was lowered from — not of whatever bytes
// sat beside the tree at the time. Nothing here pays for that: the hash was
// already computed on every compile for exactly that field, and comparing it is
// the same work done once instead of trusted.
//
// The tree is live, not a snapshot: an overlay patches it in place, so a Parsed
// belongs to one compile (compilers.Source.Parsed says so).
type Parsed struct {
	hash [sha256.Size]byte
	root *yaml.Node
	rest tail
}

// Root is the document the compile lowers — the first in the stream that holds
// content — for a reader that wants only what the source declares.
func (p *Parsed) Root() *yaml.Node { return p.root }

// Decode parses source bytes into the document the compile lowers and what
// follows it. It is decode under an exported name, for detection to read the
// same document the compile will (GitHub #481) and to leave the parse behind
// for it (Parsed). The error is the parser's own, unwrapped, since detection
// quotes it; the compile wraps it as ErrParse.
func Decode(data []byte) (*Parsed, error) {
	root, rest, err := decodeStream(data)
	if err != nil {
		return nil, err
	}
	return &Parsed{hash: sha256.Sum256(data), root: root, rest: rest}, nil
}

// parsedFor returns the decoded form of src: the one its caller already made,
// when it is this compiler's own and describes these very bytes, and a fresh
// parse otherwise.
//
// The bytes are compared by content, not by the identity of the slice holding
// them. Identity is the cheaper question and the wrong one: a caller reading
// into a pooled buffer hands back the same backing array with different bytes
// in it, and every compile would then lower a tree the source no longer holds
// while stamping SourceInfo.Hash from the bytes it does — a document whose
// recorded hash describes content it was not built from, which is the identity
// golden snapshots, IR diffing and caching all key on (ir-design §7).
//
// It costs nothing to ask. The digest is the one SourceInfo.Hash has always
// recorded, so the hash taken here replaces the one build took rather than
// adding to it; equal bytes then yield the tree they parse to, whichever buffer
// they arrived in.
func parsedFor(src compilers.Source) (*Parsed, error) {
	// A nil *Parsed stored in the interface is not a nil interface, so the
	// assertion succeeds and hands back nothing to read; ir.IsNilTypeDef and
	// compilers' own nil-compiler screen exist for the same two spellings.
	if p, ok := src.Parsed.(*Parsed); ok && p != nil && p.hash == sha256.Sum256(src.Data) {
		return p, nil
	}
	return Decode(src.Data)
}

// Hash is the lowercase hex SHA-256 of the bytes this parse was built from, for
// SourceInfo.Hash.
func (p *Parsed) Hash() string { return hex.EncodeToString(p.hash[:]) }

// firstWithContent reads documents from dec until one holds content, and
// returns it. A leading document that decodes to null — a bare `---`, a
// comment, any spelling of null — is what a file assembled from fragments or
// a template with its header stripped begins with, and taking it as the
// document refused every such source for a null root while a whole spec sat
// behind it.
//
// A stream with no such document yields the first document it holds — a null,
// which the model build refuses as it always has — or a node of no kind when
// it holds none at all, as yaml.Unmarshal leaves one for an empty source; the
// two reach different refusals and are kept apart. An error from the decoder
// before content is found is the source's: nothing readable came before it.
// The search reads at most maxStreamDocuments documents, the bound readTail
// reads under; past it the first document read stands.
func firstWithContent(dec *yaml.Decoder) (*yaml.Node, error) {
	var first *yaml.Node
	for range maxStreamDocuments {
		var doc yaml.Node
		err := dec.Decode(&doc)
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return nil, err
		}
		if holdsContent(&doc) {
			return &doc, nil
		}
		if first == nil {
			first = &doc
		}
	}
	if first == nil {
		return &yaml.Node{}, nil
	}
	return first, nil
}

// maxStreamDocuments bounds how many documents decode reads past the one it
// takes, to count them, and how many it reads to find one. Each is parsed into
// a tree that is never lowered, so the bound caps what a stream can cost
// beyond the byte budget it already fits; a stream past it is reported as
// holding at least that many, never as fewer.
const maxStreamDocuments = 1024

// tail is what a source carries past the document the compiler lowers. Its
// zero value is the answer for a source with one document holding content:
// nothing dropped.
type tail struct {
	// line and column are where the first dropped document that holds content
	// begins, for the diagnostic to name; zero when none parsed. yaml.v3 counts
	// lines from one, so zero is not a position. Only the position is kept: the
	// document's tree is never lowered and is left to be collected.
	line, column int
	// dropped counts the documents past the first that hold content, up to
	// maxStreamDocuments.
	dropped int
	// capped reports that counting stopped at maxStreamDocuments, so dropped
	// is a floor.
	capped bool
	// unparsed reports that the stream continued with content yaml.v3 could
	// not parse — bytes yaml.Unmarshal never read, since it parses one document
	// per call. What it could not parse is not always malformed: a later
	// document opening with a %YAML directive is refused as "incompatible", as
	// it would be as the first document too.
	unparsed bool
}

// readTail reads the documents that follow the one decode took, counting those
// that hold content. A document that holds nothing — a bare separator, an
// explicit null — is what a trailing `---` produces, and skipping it drops
// nothing, on the same reading firstWithContent skips a leading one by.
func readTail(dec *yaml.Decoder) tail {
	var t tail
	for range maxStreamDocuments {
		var doc yaml.Node
		err := dec.Decode(&doc)
		if errors.Is(err, io.EOF) {
			return t
		}
		if err != nil {
			t.unparsed = true
			return t
		}
		if holdsContent(&doc) {
			if t.line == 0 {
				t.line, t.column = doc.Content[0].Line, doc.Content[0].Column
			}
			t.dropped++
		}
	}
	t.capped = true
	return t
}

// holdsContent reports whether a decoded document wraps a root that is not the
// null scalar an empty document decodes to. yaml.v3 wraps exactly one root in
// every document it parses; the length check is the guard, not a case.
func holdsContent(doc *yaml.Node) bool {
	if doc.Kind != yaml.DocumentNode || len(doc.Content) != 1 {
		return false
	}
	root := doc.Content[0]
	return root.Kind != yaml.ScalarNode || root.ShortTag() != "!!null"
}

// diagnostics reports the drop as one error diagnostic, anchored where the
// first dropped document begins, or nothing for a single-document source.
func (t tail) diagnostics(srcIndex int) []ir.Diagnostic {
	if t.dropped == 0 && !t.unparsed {
		return nil
	}
	prov := ir.Provenance{Source: srcIndex}
	if t.line > 0 {
		prov.Pointer = fmt.Sprintf("%d:%d", t.line, t.column)
	}
	return []ir.Diagnostic{diag.Newf(ir.SeverityError, diag.StreamDocumentsDropped, prov,
		"only one document of the YAML stream was lowered; %s", t.describe())}
}

// describe spells what the stream held past its first document.
func (t tail) describe() string {
	var parts []string
	switch {
	case t.capped:
		parts = append(parts, fmt.Sprintf("at least %d after it hold content the IR does not", t.dropped))
	case t.dropped == 1:
		parts = append(parts, "the one after it holds content the IR does not")
	case t.dropped > 1:
		parts = append(parts, fmt.Sprintf("the %d after it hold content the IR does not", t.dropped))
	}
	if t.unparsed {
		parts = append(parts, "what follows could not be parsed as YAML")
	}
	return strings.Join(parts, ", and ")
}

// unmarshal builds a speakeasy document from an already-decoded node tree,
// reproducing soa.Unmarshal's sequence with the decode lifted out: seed the
// cache, carry the source's YAML formatting onto the core model, populate from
// the node, then validate and sort the findings as the library does. Reading the
// nodes rather than bytes is what preserves each one's line and column through
// an overlay.
//
// It converts a panic from the third-party parser — which faults on degenerate
// input such as a whitespace-only document — into an ErrParse error, so the
// compiler upholds the no-panics-escape invariant instead of crashing the
// caller's process. The named returns are reset in the recover so a
// partially-assigned document never leaks.
//
// The recover reaches only this goroutine: the model's entry point, populating
// it from the core, and validating it — where the whitespace fault is raised.
// The parser fans a model's fields out over an errgroup, so a fault while
// building one of them — a tagged mapping at a reference position, before the
// pre-parse refusals learned to catch it (GitHub #474) — is raised on a
// goroutine the parser owns, and ends the process. Nothing here can change
// that: recover is per goroutine and the parser exposes no hook. A shape known
// to fault there is refused before the tree is handed over (see refusals).
func unmarshal(ctx context.Context, data []byte, root *yaml.Node) (doc *soa.OpenAPI, valErrs []error, err error) {
	defer func() {
		if r := recover(); r != nil {
			doc, valErrs = nil, nil
			err = fmt.Errorf("parser panicked (%v): %w", r, ErrParse)
		}
	}()
	if len(data) == 0 {
		return nil, nil, errors.New("empty document")
	}

	var out soa.OpenAPI
	out.InitCache()
	out.GetCore().SetConfig(yml.GetConfigFromDoc(data, root))

	valErrs, err = marshaller.UnmarshalNode(ctx, "", root, &out)
	if err != nil {
		return nil, nil, fmt.Errorf("unmarshal document: %w", err)
	}
	valErrs = append(valErrs, out.Validate(ctx)...)
	validation.SortValidationErrors(valErrs)
	return &out, valErrs, nil
}

// validationDiag converts one speakeasy validation error into a diagnostic. A
// structured *validation.Error yields severity, a rule-suffixed code, the
// provenance locate gives its node, and the finding itself as the message;
// anything else degrades to an error with the bare message and the source
// alone.
func validationDiag(locate scan.Locator, err error) ir.Diagnostic {
	if verr, ok := asValidationError(err); ok {
		return diag.Newf(mapSeverity(verr.Severity), diag.Validation+"/"+verr.Rule,
			locate(verr.Node), "%s", validationMessage(verr))
	}
	return diag.Newf(ir.SeverityError, diag.Validation, locate(nil), "%s", err.Error())
}

// validationMessage is the finding a validation error carries, without the
// prefix the library's Error renders around it. That prefix spells the
// severity, the rule and the node's line and column, each of which the
// diagnostic already holds as a field — and the position it spells is the
// node's own, which for a node an overlay grafted is 0:0, contradicting the
// provenance beside it. The document location is kept when there is one, as
// the library keeps it: no field of the diagnostic holds it.
func validationMessage(verr validation.Error) string {
	if verr.UnderlyingError == nil {
		return verr.Rule // never produced by the library, whose own Error would fault on it
	}
	msg := verr.UnderlyingError.Error()
	if verr.DocumentLocation != "" {
		msg += " (document: " + verr.DocumentLocation + ")"
	}
	return msg
}

// asValidationError extracts a structured validation error. The wrapped value
// may be stored by value or by pointer, so both forms are probed. The chain is
// walked manually with type assertions instead of errors.As; see the nolint
// comment below for why.
func asValidationError(err error) (validation.Error, bool) {
	for e := err; e != nil; e = errors.Unwrap(e) {
		//nolint:errorlint // Deliberately hand-walked: errors.As would invoke the
		// speakeasy errors.Error type's As method, which matches any target named
		// "Error" and calls SetString, panicking on the validation.Error struct.
		switch v := e.(type) {
		case *validation.Error:
			return *v, true
		case validation.Error:
			return v, true
		}
	}
	return validation.Error{}, false
}

// mapSeverity maps a speakeasy validation severity onto an ir.Severity.
func mapSeverity(s validation.Severity) ir.Severity {
	switch string(s) {
	case "warning":
		return ir.SeverityWarning
	case "hint":
		return ir.SeverityInfo
	default:
		return ir.SeverityError
	}
}

// SupportedMinor returns the normalized major.minor prefix of an OpenAPI version
// string and whether the compiler supports it (3.0, 3.1, or 3.2).
func SupportedMinor(version string) (string, bool) {
	parts := strings.SplitN(version, ".", 3)
	if len(parts) < 2 {
		return "", false
	}
	mm := parts[0] + "." + parts[1]
	switch mm {
	case "3.0", "3.1", "3.2":
		return mm, true
	default:
		return "", false
	}
}
