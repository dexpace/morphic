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
	// unmarshal itself. It is unexported because it is this package's test seam:
	// the rebuild repeats an unmarshal that already succeeded on the same bytes
	// and tree, and unmarshal depends on nothing else, so only a substitute
	// reaches the error build returns for a failed rebuild.
	rebuildDoc func(ctx context.Context, data []byte, root *yaml.Node) (*soa.OpenAPI, []error, error)
	// chainCheck runs the reference-chain cycle refusal, or nil for chainCycle
	// itself. It is unexported because it is this package's test seam: the fuzz
	// oracle turns the check off to find what it stands between the resolver and.
	chainCheck func(ctx context.Context, locate scan.Locator, root *yaml.Node, doc *soa.OpenAPI) (ir.Diagnostic, bool)
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

// releaseAnchors clears the anchor name from every node of the model's source
// tree, so the parser folds every entry the document writes. root must not be
// nil.
//
// The parser skips an anchored mapping entry where the model folds entries into
// a map, and drops it silently (GitHub #459; speakeasy-api/openapi v1.25.2,
// marshaller/unmarshaller.go). That skip is the only read of an anchor's name;
// an alias reaches its target by a kept pointer. Run it after the pre-parse
// refusals, which do read names. A document read through external is released
// too (GitHub #501), and replaces the resolver's own parse on a second
// resolution (GitHub #538).
func releaseAnchors(root *yaml.Node) {
	stack := []*yaml.Node{root}
	for len(stack) > 0 {
		n := stack[len(stack)-1]
		stack = stack[:len(stack)-1]
		n.Anchor = ""
		// Follows Content, never an alias: a recursive anchor cannot loop it.
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
	// Targets holds the schema each discriminator mapping target names,
	// resolved as a $ref to it is.
	Targets MappingTargets
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
// version check, the reference-chain cycle refusal, then validation findings
// and reference resolution as diagnostics. A nil document with error
// diagnostics is a refusal to lower.
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

	locate := locator(srcIndex, origin)
	if d, found := opts.chains(ctx, locate, root, doc); found {
		if d.Severity == ir.SeverityError {
			return nil, append(cyc, d), nil // a chain the resolver would recurse through forever
		}
		cyc = append(cyc, d)
	}
	diags := cyc
	diags = append(diags, findings(ctx, locate, doc, valErrs, minor)...)
	rebuildDoc := opts.rebuildDoc
	if rebuildDoc == nil {
		rebuildDoc = unmarshal
	}
	rebuild := func() (*soa.OpenAPI, error) {
		again, _, err := rebuildDoc(ctx, src.Data, root)
		return again, err
	}
	self := newSourceDocument(src.Path, src.Data, root, valErrs)
	doc, targets, resolveDiags, err := resolve(ctx, pointerAt(srcIndex, origin), doc, self, opts, rebuild)
	if err != nil {
		return nil, nil, fmt.Errorf("openapi: rebuild source %d: %w", srcIndex, err)
	}
	diags = append(diags, resolveDiags...)

	return &Document{
		Doc:     doc,
		Targets: targets,
		Source: ir.SourceInfo{
			Format: "openapi@" + minor,
			Path:   src.Path,
			Hash:   parsed.Hash(),
		},
		Overlay: origin,
	}, diags, nil
}

// findings converts the model build's validation errors into diagnostics,
// dropping the two kinds that are library artifacts rather than spec problems —
// a numeric literal Morphic captures losslessly anyway, and a schema finding
// raised only because the library checked against the wrong meta-schema — and
// the findings of a rule the compiler checks itself.
func findings(ctx context.Context, locate scan.Locator, doc *soa.OpenAPI, valErrs []error, minor string) []ir.Diagnostic {
	wrongMetaSchema := metaSchemaVersionArtifacts(ctx, doc, minor)
	diags := make([]ir.Diagnostic, 0, len(valErrs))
	for _, ve := range valErrs {
		if verr, ok := asValidationError(ve); ok &&
			(dropped(verr) || wrongMetaSchema[findingSite(verr)]) {
			continue
		}
		diags = append(diags, validationDiag(locate, ve))
	}
	return diags
}

// dropped reports whether a finding is one the compiler drops wherever the
// library makes it, the source or a document a $ref reads: a library artifact
// (numericLiteralArtifact), or a finding of a rule the compiler checks itself
// (compilerOwned).
func dropped(verr validation.Error) bool {
	return numericLiteralArtifact(verr) || compilerOwned(verr)
}

// compilerOwned reports whether a finding belongs to a rule the compiler checks
// itself.
//
// operationId uniqueness is the one. The library counts the operations written
// under paths, walking the model before references are resolved: an operation a
// YAML alias mounts twice counts twice, a $ref's target counts only where it is
// written under paths, and no webhook, callback or component operation counts
// at all. So it refused the alias form where the $ref form drew only a warning,
// and missed repeats outside paths. The service lowering judges every claim
// once it has seen them all (GitHub #502).
func compilerOwned(verr validation.Error) bool {
	return verr.Rule == validation.RuleValidationOperationIdUnique
}

// resolve resolves every reference in doc and reports what each resolution
// found at the $ref that produced it (see resolveWith). It returns the document
// it resolved, which is a rebuild of doc when resolveExternal had to recover an
// anchored external document the resolver parsed itself.
//
// What the references bring in from other documents is validated after the
// last resolution, over the document returned (see validateReached): a first
// resolution's objects are discarded when a rebuild replaces it, so validating
// them would be work whose findings are thrown away, or findings reported
// about objects the IR is not built from.
func resolve(ctx context.Context, at func(jsontext.Pointer) ir.Provenance, doc *soa.OpenAPI, self sourceDocument,
	opts Options, rebuild func() (*soa.OpenAPI, error),
) (*soa.OpenAPI, MappingTargets, []ir.Diagnostic, error) {
	if !opts.AllowExternalRefs {
		targets, diags := resolveWith(ctx, at, doc, self, opts, nil)
		return doc, targets, diags, nil
	}
	resolved, targets, diags, err := resolveExternal(ctx, at, doc, self, opts, rebuild)
	if err != nil {
		return nil, MappingTargets{}, nil, err
	}
	return resolved, targets, append(diags, validateReached(ctx, at, resolved, opts)...), nil
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
		return ir.Provenance{Source: origin.IndexAt(p, srcIndex), Pointer: p}
	}
}

// refusals indexes a decoded tree once and reports its pre-parse refusals: the
// degenerate reference and alias structures scan finds, a mapping carrying a
// tag the parser faults on (see diag.TaggedMapping), and a refusal of its own
// when the document is too large to index in full. A tag refusal is reported
// alongside a cycle, not instead of one.
//
// The size refusal is here, not in scan, because a truncated index answers only
// in part, and an alias-expansion allowance derived from a partial node count
// would refuse documents on a bound they never crossed.
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
// taken as data so each outcome is testable. An error there refuses before the
// library is handed anything. Anything less, only the scan's report that it
// faulted, is carried into what the compile reports: the overlay's protection
// from the library is incomplete.
//
// It re-runs the pre-parse refusals afterwards, because an overlay action can
// graft a $ref cycle onto a document that had none. They locate through the
// attribution the application produced, so a refusal on a grafted node names
// the overlay, not a position the node lacks.
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

// overlayRefusals refuses an overlay whose aliases the library would follow
// without end or far past its size, before the library is handed it.
//
// The library clones each update by following aliases, so an anchor naming one
// of its own ancestors overflows the stack and an alias bomb exhausts memory.
// Neither is a panic, so no barrier catches them (GitHub #489). They are the
// source's refusals, under the same limits.
//
// The whole overlay is decoded, since a scan of each update alone would miss a
// cycle whose anchor sits outside it. Undecodable bytes are left to the
// library.
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

// metaSchemaVersionArtifacts returns the findings the library raised only by
// checking schemas against the wrong meta-schema, keyed by findingSite, or nil
// unless minor is metaSchemaReconciledMinor.
//
// JSONSchema[T].Validate discards its options (jsonschema/oas3/jsonschema.go),
// so the document's version never reaches schema validation and every schema is
// checked against the 3.1 meta-schema. A valid 3.2 document using a 3.2-only
// keyword, such as discriminator.defaultMapping, then fails.
//
// The findings are derived, not listed. oas3.Validate does honour the option,
// so validating every schema twice, at the document's version and as the
// library does, isolates them: a finding only the second run raises is an
// artifact and the rest stay.
func metaSchemaVersionArtifacts(ctx context.Context, doc *soa.OpenAPI, minor string) map[string]bool {
	if minor != metaSchemaReconciledMinor {
		return nil
	}
	return schemaArtifacts(ctx, func() iter.Seq[soa.WalkItem] { return soa.Walk(ctx, doc) }, doc.OpenAPI)
}

// schemaArtifacts returns the findings sites schema validation raises over the
// walk only because it checked against the wrong meta-schema (see
// metaSchemaVersionArtifacts), for a document at version. walk is called once
// per run, so each run gets a walk of its own.
func schemaArtifacts(ctx context.Context, walk func() iter.Seq[soa.WalkItem], version string) map[string]bool {
	atDocumentVersion := schemaFindings(ctx, walk(),
		validation.WithContextObject(&oas3.ParentDocumentVersion{OpenAPI: &version}))
	asLibraryChecks := schemaFindings(ctx, walk())
	for key := range atDocumentVersion {
		delete(asLibraryChecks, key)
	}
	return asLibraryChecks
}

// schemaFindings validates every schema object items reaches under opts,
// returning each finding's site.
func schemaFindings(ctx context.Context, items iter.Seq[soa.WalkItem], opts ...validation.Option) map[string]bool {
	out := map[string]bool{}
	matchSchemas(items, func(js *oas3.JSONSchema[oas3.Referenceable]) error {
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
	if _, after, ok := strings.CutLast(name, "."); ok {
		name = after
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

// nodeCount returns the number of nodes in the parsed tree rooted at root. An
// alias node has empty Content in yaml.v3 and its edge is never followed, so
// the iterative walk visits each node once.
//
// The alias-amplification budget in scan takes the same measure of the same
// tree. The two walks are deliberately not shared: ten lines over a third-party
// node type are not worth a dependency between two packages with different
// jobs.
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
// compile lowers, the first with content, and reads what follows so extra
// documents are reported, not dropped. The error is the parser's own, which
// detection quotes and Load wraps as ErrParse.
//
// It is split from unmarshal so an overlay can patch the tree in between;
// re-parsing after serialising would renumber every line. It bounds no alias
// expansion, because a node tree holds an alias as one node, and scan's weigher
// refuses a billion-laughs document. It needs no recover: yaml.v3 turns its own
// faults into errors.
func decodeStream(data []byte) (*yaml.Node, tail, error) {
	dec := yaml.NewDecoder(bytes.NewReader(data))
	root, err := firstWithContent(dec)
	if err != nil {
		return nil, tail{}, err
	}
	return root, readTail(dec), nil
}

// Parsed is a source's decoded form, produced once and used twice: detection
// reads the document's keys off it, and the compile it routes to lowers the
// same tree. It travels in compilers.Source.Parsed.
//
// It holds the digest of the bytes it came from, so a reader can check that it
// describes the Data beside it and read the bytes afresh if not. The digest is
// what SourceInfo.Hash records, so a document's hash is that of the bytes it
// was lowered from.
//
// The tree is live: an overlay patches it in place, so a Parsed belongs to one
// compile.
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
// The bytes are compared by content, not by slice identity: a caller reading
// into a pooled buffer hands back the same backing array with different bytes,
// and the compile would lower a tree the source no longer holds while stamping
// SourceInfo.Hash from the bytes it does, breaking the identity that snapshots,
// IR diffing and caching key on (ir-design §7). The digest costs nothing extra:
// SourceInfo.Hash records it anyway.
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
// returns it. A leading document that decodes to null, such as a bare `---`, is
// what a file assembled from fragments begins with; taking it as the document
// would refuse the spec behind it.
//
// A stream with no such document yields the first one it holds, a null the
// model build refuses, or a node of no kind when it holds none; the two reach
// different refusals. A decoder error before content is the source's. The
// search reads at most maxStreamDocuments documents; past it the first one read
// stands.
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
		prov.Position = ir.Position{Line: t.line, Column: t.column}
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
// reproducing soa.Unmarshal with the decode lifted out, so each node keeps its
// line and column through an overlay.
//
// A panic from the third-party parser, which faults on degenerate input such as
// a whitespace-only document, becomes an ErrParse error. The recover reaches
// only this goroutine, and the parser fans a model's fields out over an
// errgroup, so a fault in one of those goroutines ends the process: a shape
// known to fault, a tagged mapping at a reference position (GitHub #474), is
// refused beforehand (see refusals).
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
