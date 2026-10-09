// Package diag holds the OpenAPI compiler's diagnostic vocabulary: the stable
// codes it reports under, and the single constructor that builds a diagnostic
// from them.
//
// It sits at the bottom of the compiler's import graph because every package
// that reports anything reaches it and it reaches nothing but ir. The codes are
// format-specific strings and stay here rather than promoting to
// compilers/compile — that rule wants evidence from all three compilers, and the
// GraphQL and Protobuf drafts have not been compared. Whether Newf itself later
// promotes is left open for the same reason.
package diag

import (
	"fmt"
	"strings"
	"unicode/utf8"

	"github.com/dexpace/morphic/ir"
)

// The stable codes the OpenAPI compiler reports under. They are stable strings
// so CI can allowlist them (ir-design §13).
const (
	// Validation reports a speakeasy validation finding; it is suffixed with the
	// library rule name (e.g. "openapi/validation/duplicate-tag").
	Validation = "openapi/validation"
	// UnsupportedVersion reports an OpenAPI version the compiler cannot lower.
	UnsupportedVersion = "openapi/unsupported-version"
	// UnresolvedRef reports a $ref that could not be resolved.
	UnresolvedRef = "openapi/unresolved-ref"
	// CyclicRef reports a degenerate reference cycle, caught before it can crash
	// the parser with a stack overflow or deadlock the resolver on a lock its own
	// goroutine holds. Two checks report it: a pre-parse scan over the decoded
	// tree, for a recursive YAML anchor or a chain of $ref-only schemas joined by
	// ordinary same-document pointers; and load's reach, for a cycle that closes
	// only through $anchor, $id or $defs-relative resolution. Reach
	// over-approximates the resolver, so it can refuse a document the resolver
	// survives; a $defs reference load holds out of the resolver is exact
	// (GitHub #557).
	CyclicRef = "openapi/cyclic-ref"
	// CycleScanFailed reports that a cycle check did not run to completion,
	// leaving its stack-overflow protection incomplete for the source: the
	// pre-parse scan, load's reach, or load's reading of schema chains for one
	// closing through the source's file name either aborted (a detector bug) or
	// hit one of its bounds. It is a warning: the compile still proceeds, and
	// every cycle the check did classify is still caught. Past the chain read's
	// bound, a $ref it did not read is left unresolved where the source is held,
	// since its chain could close through the held file and run the stack out.
	CycleScanFailed = "openapi/cycle-scan-failed"
	// SourceTooLarge reports a document with more YAML nodes than the pre-parse
	// scan indexes (sourceindex.MaxIndexedNodes). Every answer the index gives
	// about such a document is a partial one, including the node count the
	// alias-expansion allowance is derived from, so the document is refused rather
	// than scanned against a bound computed from a count that stopped early.
	SourceTooLarge = "openapi/source-too-large"
	// TaggedMapping reports a mapping carrying a YAML tag other than !!map, which
	// OpenAPI forbids: its YAML form limits tags to YAML 1.2's JSON schema
	// ruleset. At a reference position whose model is a struct (a request body,
	// response, parameter and the like) the parser leaves the model unbuilt and
	// dereferences it, on a goroutine no recover here reaches (GitHub #474).
	//
	// The document is refused before parsing, at every position, because telling
	// the faulting ones apart would mean maintaining a copy of the parser's object
	// model. A tagged scalar is a separate question (GitHub #245) and is not
	// refused.
	TaggedMapping = "openapi/tagged-mapping"
	// StreamDocumentsDropped reports a YAML stream holding more than one
	// document with content. An OpenAPI document is one YAML document, so the
	// first is what is lowered; every one after it reaches the IR in no form at
	// all, which is a losslessness failure rather than a degradation (the
	// distinction UnpreservableConstruct draws) and so an error, though the
	// compile proceeds with the first (GitHub #387). A document that holds
	// nothing — a bare separator, an explicit null — drops nothing and is not
	// reported.
	StreamDocumentsDropped = "openapi/stream-documents-dropped"
	// UndecodableSource reports a source that declares one of this compiler's
	// discriminating keys and does not parse as YAML or JSON. It is reported from
	// detection rather than from the compile, because a document that cannot be
	// parsed never reaches one: without it the engine could only say no compiler
	// recognized the source, which is wrong twice over — this compiler did
	// recognize it, and the reason it declined is the parse error it holds.
	UndecodableSource = "openapi/undecodable-source"
	// OverlayInvalid reports an overlay document that could not be parsed, or that
	// parsed but is not a valid Overlay — a missing version, no actions, an action
	// naming no target. Nothing is applied, so the compile refuses rather than
	// lowering a source the caller believes was patched.
	OverlayInvalid = "openapi/invalid-overlay"
	// OverlayFailed reports an overlay the library could not apply as written: a
	// selector matching nothing under strict application, or an action whose
	// update disagrees with the shape it targets. Actions are applied in order and
	// the ones that landed are not undone, so the compile refuses — the tree left
	// behind is neither the source nor what the overlay asked for.
	OverlayFailed = "openapi/overlay-failed"
	// OverlayAction reports one strict-mode finding about a single action, naming
	// the action and its target: that it changed nothing, or that it relies on
	// JSONPath behaviour the overlay did not opt into.
	//
	// It stands alone as often as it accompanies a refusal, and the difference is
	// the point. An action whose selector matched nothing is a typo and refuses
	// under OverlayFailed, with these naming which actions are why; an action that
	// matched and then changed nothing is merely redundant — the fix it describes
	// is already in the source — so it is reported and the compile proceeds.
	OverlayAction = "openapi/overlay-action"
	// OverlayOriginIncomplete reports that the overlay applied but the walk that
	// attributes positions to it exceeded its node budget. Provenance degrades to
	// naming the source for every position, which is what a compile with no
	// overlay reports; nothing else about the lowering changes.
	OverlayOriginIncomplete = "openapi/overlay-origin-incomplete"
	// ValidationOnlyKeyword reports a validation-only JSON Schema keyword kept
	// verbatim under Unmodeled (ir-design §4.7).
	ValidationOnlyKeyword = "openapi/validation-only-keyword"
	// FalseSchema reports a boolean `false` schema (matches nothing).
	FalseSchema = "openapi/false-schema"
	// EmptyEnum reports an `enum` whose member list is empty. JSON Schema gives it
	// a meaning, a value space holding no member so the position accepts no
	// instance, which the IR states exactly as a closed Enum with no members.
	//
	// Warning where FalseSchema is info: a boolean `false` schema is the idiom for
	// "forbid this here", but an empty member list is that statement written the
	// way nobody does on purpose, usually a generator emitting a list it never
	// filled. Not an error: the document is well-formed, and harness.Check stops
	// at the first error diagnostic, hiding later findings.
	EmptyEnum = "openapi/empty-enum"
	// NumericPrecision reports a numeric bound literal that is not a finite
	// number (error severity: Morphic owns these keywords, so this is the sole
	// diagnostic for the defect — see boundLiteralDiag).
	NumericPrecision = "openapi/invalid-numeric-literal"
	// ExclusiveBoundForm reports an exclusiveMinimum/exclusiveMaximum whose value
	// form is wrong for the document's dialect (a boolean under 2020-12, or a
	// number under 3.0) — see exclusiveFormDiag.
	ExclusiveBoundForm = "openapi/invalid-exclusive-bound"
	// InvalidStatusKey reports a responses-map key that names no status: not a
	// 100–599 code, not one of the 1XX–5XX wildcard ranges, not "default". The
	// response still lowers, with no status condition rather than the catch-all
	// range that "default" alone denotes (GitHub #262).
	InvalidStatusKey = "openapi/invalid-status-key"
	// DuplicateStatusKey reports two responses-map keys on one operation that
	// resolve to the same status range — "4XX" beside "4xx", or "200" beside a
	// second "200" a merge key introduced. The key reaches the IR neutralized, so
	// both lower to one hint and one condition, and an ErrorCase carries no ID:
	// name and conditions are the whole of what tells two apart. Both are kept,
	// because neither key is wrong on its own and dropping one would pick a winner
	// on nothing but declaration order.
	DuplicateStatusKey = "openapi/duplicate-status-key"
	// InvalidMethodKey reports an additionalOperations key that names no method:
	// the empty string. The operation still lowers, binding the key as written, so
	// nothing the entry declares is lost — what is reported is that the binding's
	// method is unusable. speakeasy rejects a key naming a *standard* method, which
	// belongs in its own field, but accepts this one.
	InvalidMethodKey = "openapi/invalid-method-key"
	// DegradedConstruct reports a construct the compiler could not carry into the
	// IR as written: preserved raw for want of a structural home, lowered to a
	// weaker shape (a heterogeneous enum as a union, an unconvertible value as
	// the top type), or — for an annotation like a default or example — dropped.
	// It marks the lossy lowerings the compiler reports, not a guarantee that
	// every lossy lowering is reported.
	DegradedConstruct = "openapi/degraded-construct"
	// CompositionLowering reports that a schema conjoining a structural body with
	// a oneOf/anyOf was lowered by distributing the body across the union
	// variants (ir-design §4.3, §4.8). Nothing is lost, so this records a
	// decision rather than a degradation: the IR's shape no longer mirrors the
	// source's, which is what a reader comparing the two needs told.
	CompositionLowering = "openapi/composition-lowering"
	// DynamicRefExpanded reports that a $dynamicRef was resolved to the one
	// $dynamicAnchor matching it in this document (ir-design §4.7). Like
	// CompositionLowering this records a decision rather than a loss: the
	// reference resolved, but what the source wrote as dynamic is now a fixed
	// target, so a reader comparing IR to source needs telling that the
	// indirection was collapsed at compile time rather than left to evaluation.
	DynamicRefExpanded = "openapi/dynamic-ref-expanded"
	// ConflictingRedecl reports that inline allOf branches redeclare one field
	// with disagreeing values. An incompatible target type (string vs. integer) is
	// unsatisfiable; a conflicting constraint keyword (minimum: 10 vs.
	// exclusiveMinimum: 10) is usually satisfiable but not representable by a
	// simple merge, so the merge keeps an arbitrary source-order winner, possibly
	// the looser bound.
	//
	// Either way the losing declaration is kept whole under the merged property's
	// Unmodeled (merge.keepLosingDeclaration), so a consumer reading the document
	// finds it. Intersecting the bounds instead is the recorded direction (GitHub
	// #10).
	ConflictingRedecl = "openapi/conflicting-redeclaration"
	// DisjointVisibility reports one field restricted to lifecycle sets that
	// share nothing — readOnly against writeOnly — so no lifecycle admits it at
	// all. Both spellings raise it under this one code: one schema writing both
	// flags, and inline allOf branches whose intersection is empty. It is
	// neither of its neighbours: ConflictingRedecl keeps an arbitrary
	// source-order winner, and DegradedConstruct lowers to a weaker shape,
	// whereas Visibility{None: true} is the exact answer and a shape the
	// IR already has. It is reported nonetheless, because a field no request or
	// response can carry is seldom what the document set out to say.
	DisjointVisibility = "openapi/disjoint-visibility"
	// AliasAmplification reports a document whose YAML aliases expand it past a
	// fixed multiple of its own size (scan's maxAliasAmplification, with a floor
	// for small documents) — a billion-laughs shape
	// that would exhaust memory inside soa.Unmarshal before any reference is
	// resolved (GitHub #27). Unlike CycleScanFailed's incomplete-scan warning,
	// this is a positive, measured finding, so the document is refused outright
	// rather than handed to the parser.
	//
	// It is a ratio and no caller's budget, so no setting admits a document it
	// names. A document inside the ratio whose aliases still add more nodes than
	// openapi.Limits.MaxAliasSurplus is BudgetExceeded instead, and one past both
	// is this.
	AliasAmplification = "openapi/alias-amplification"
	// BudgetExceeded reports an input that crossed a budget. openapi.Limits sets
	// most: a document past the byte or node budget (or an overlay action that
	// would build it past that), aliases adding more nodes than the alias budget,
	// an enum past the member budget (GitHub #75). Load bounds two kinds of work
	// itself: resolving the mapping targets, and resuming chains through other
	// documents.
	//
	// Unlike AliasAmplification's bomb, these are documents legitimately that
	// large. Every site reports an error: the document is refused, its
	// validation stops, an enum loses its members (UnpreservableConstruct), or
	// what the work did not reach is left unresolved.
	BudgetExceeded = "openapi/budget-exceeded"
	// UnattachableRequired reports a composition-scope `required` name (an allOf
	// branch's own required list, or the composed schema's own) that matches none
	// of the model's own properties, so it has no IR home to attach to
	// (ir-design §4.3: Properties holds only own properties, and flattening
	// across Base/Mixins is computed, never stored).
	UnattachableRequired = "openapi/unattachable-required"
	// InternalInvariant reports that lowering's own pointer-to-registry invariant
	// broke: a pointer named a type ID the registry does not hold, so whatever
	// was about to be attached there had nowhere to go. No source can provoke
	// this — it is a compiler bug — but it is reported rather than dropped in
	// silence, since the alternative is losing constructs with no trace at all.
	InternalInvariant = "openapi/internal-invariant"
	// DuplicateOperationID reports an operationId that one declaration claims
	// more than once because it is mounted more than once: a path item or an
	// operation reused by a $ref, a YAML alias or a merge key. The document
	// writes the id once, so it is a warning, but each mount is an operation of
	// its own and an emitter renders them all under one identifier.
	DuplicateOperationID = "openapi/duplicate-operation-id"
	// ConflictingOperationID reports an operationId that a second declaration
	// writes again. OpenAPI requires the id to be unique among every operation
	// the document describes, webhooks and callbacks included, and this is the
	// document repeating it, so it is an error (GitHub #502).
	ConflictingOperationID = "openapi/conflicting-operation-id"
	// IncompleteSecurityScheme reports a securitySchemes entry that omits the
	// field naming its mechanism: `type`, or for type http the RFC 7235 `scheme`
	// token. Nothing is interned for it and every requirement naming it is dropped
	// (GitHub #294).
	//
	// Error, as for UnresolvedRef: the entry reached the IR in no form, so a
	// reader told only that it was degraded would look for a scheme that is not
	// there. The document is invalid either way, since OpenAPI requires both
	// fields.
	IncompleteSecurityScheme = "openapi/incomplete-security-scheme"
	// OAuth2NoFlow reports an oauth2 securitySchemes entry declaring no flow:
	// `flows` absent, `flows: {}`, or only keys the model does not name, which all
	// lower to an empty flow list. It is reported once, at the entry's pointer,
	// because the loader cannot place it: it accepts `flows: {}` and reports an
	// absent `flows` with no pointer (GitHub #646).
	//
	// Reported, not refused as IncompleteSecurityScheme is: the IR states exactly
	// what the document said, and refusing would drop the entry's text. A warning,
	// like ReservedHeaderName, since the document lowers whole. oauth2MetadataUrl
	// does not exempt the entry; an incomplete flow is the loader's finding.
	OAuth2NoFlow = "openapi/oauth2-no-flow"
	// ReservedHeaderName reports a header declaration OpenAPI says SHALL be
	// ignored, because the protocol layer already owns the name. Three positions:
	//
	//   - §4.8.12, a parameter with in: header named Accept, Content-Type or
	//     Authorization;
	//   - §4.8.17, a Content-Type entry in a response's headers map;
	//   - §4.8.15, a Content-Type entry in an encoding's headers map.
	//
	// The declaration is kept, since dropping it is a loss and an emitter's call
	// (invariant 2; GitHub #39); the diagnostic lets an emitter suppress it. It is
	// unconditional, not an Options switch, because nothing is inferred (invariant
	// 6). Warning, not error: the document is well-formed, and harness.Check stops
	// at the first error, hiding later findings.
	ReservedHeaderName = "openapi/reserved-header-name"
	// UnpreservableConstruct reports a construct that reached the IR in no form at
	// all: the compiler had no field to model it and its source node could not be
	// converted to JSON either, so Unmodeled could not hold it. It is an error
	// because it is a losslessness failure rather than a degradation —
	// DegradedConstruct's constructs survive in a weaker shape, and these survive
	// in none (GitHub #144).
	UnpreservableConstruct = "openapi/unpreservable-construct"
	// UnknownSchemaKeyword reports a JSON Schema keyword no field of the schema
	// model names, kept verbatim under Unmodeled.
	//
	// Info, because the document did nothing wrong: JSON Schema requires an
	// implementation to ignore a keyword it does not recognize, and says such a
	// keyword may carry meaning for other tooling, so an unrecognized keyword is
	// legal input rather than a defect. What is recorded is this compiler's own
	// decision — that it read no meaning from the keyword and kept the text — which
	// is the same thing ValidationOnlyKeyword records beside it.
	UnknownSchemaKeyword = "openapi/unknown-schema-keyword"
	// UnknownObjectKey reports a key on an OpenAPI object that the specification
	// neither defines nor admits as an extension, kept verbatim under Unmodeled.
	//
	// Warning where its schema neighbour UnknownSchemaKeyword is info: OpenAPI
	// gives its objects a closed key set and requires extensions to be prefixed
	// x-, so such a key is a document error, usually a misspelling of the field
	// beside it. Not an error, for the reason ReservedHeaderName is not: the
	// document still lowers, and harness.Check stops at the first error
	// diagnostic, which would hide every later finding.
	UnknownObjectKey = "openapi/unknown-object-key"
	// UnknownKeyBudget reports an object declaring more keys the model does not
	// name than the compiler keeps, so the ones past the bound reached the IR in no
	// form at all.
	//
	// Every collection here is bounded, and this one is over a key set the document
	// chooses the size of. The bound is far above what any document writes by
	// accident, so tripping it is either a generated file or a hostile one; the
	// diagnostic is what keeps the discarded remainder from being a silent loss.
	UnknownKeyBudget = "openapi/unknown-key-budget"
	// UnknownKeyUnreachable reports a key the parsed model reported as undeclared
	// whose value the raw mapping does not present, so nothing of it reached the
	// IR.
	//
	// Distinct from UnpreservableConstruct beside it, which is a value that was
	// found and could not be rendered. This one was never reached: the parser
	// reads a mapping through its `<<` merge keys and the raw readers here do not,
	// so a merged-in key is named by the census and has no pair to read. Warning
	// rather than that one's error because such a document is legal and still
	// lowers.
	UnknownKeyUnreachable = "openapi/unknown-key-unreachable"
	// UnknownKeyEntryTaken reports a key whose Unmodeled entry is already held by
	// a construct declared somewhere else, so the key reached the IR in no form.
	//
	// The carriers holding more than one object's entries are where two constructs
	// can spell one entry: a parameter's own keys and the keywords its schema had
	// no home for share one unscoped namespace on ir.Parameter. Warning rather
	// than error because the document is otherwise lowered whole, and the entry
	// that did survive is in it.
	UnknownKeyEntryTaken = "openapi/unknown-key-entry-taken"
	// InvalidLocationKeyword reports explode or allowReserved at in: querystring.
	// OpenAPI 3.2 binds the whole query string from the parameter's content there
	// and states its serialization through the media type alone, so neither
	// keyword has anything left to qualify.
	//
	// The bundled parser enforces this for style at that location but not for
	// these two (GitHub #408), so the compiler reports the gap itself. The value
	// still lowers as declared: dropping stated content is an emitter's call
	// (invariant 2).
	//
	// Warning, not the error style gets: that is the parser's own validation
	// refusal, whereas here the compiler has already kept the value.
	InvalidLocationKeyword = "openapi/invalid-location-keyword"
)

// Newf builds an ir.Diagnostic with a formatted message. It is the single
// constructor for this compiler's diagnostics, so severity, code and provenance
// are always populated.
func Newf(sev ir.Severity, code string, prov ir.Provenance, format string, args ...any) ir.Diagnostic {
	return ir.NewDiagnostic(sev, code, fmt.Sprintf(format, args...), prov)
}

// HasError reports whether any diagnostic carries error severity. The load phase
// uses it to tell a refusal (a real spec problem, e.g. a degenerate cycle) from
// advisory warnings it must carry forward rather than abort on.
func HasError(diags []ir.Diagnostic) bool {
	return ir.HasError(diags)
}

// MaxQuotedErrorBytes bounds what a foreign error contributes to a diagnostic
// message. A library error obeys no size limit: yaml.v3 reports a duplicated
// mapping key once per prior occurrence, so a 32 KB source repeating one key
// 6,553 times raises a 1.2 GB error.
//
// The cap is generous because the errors worth quoting are lists, such as the
// overlay validator's one sentence per finding, and a list cut to its first
// entry says less than the reader came for.
const MaxQuotedErrorBytes = 4 << 10

// elidedMarker ends a message the cap cut. It carries no count: a marker whose
// text depends on how much was dropped makes the message depend on the whole
// error again, which is the dependency the cap exists to remove.
const elidedMarker = "… (elided)"

// OneLine collapses err's text onto one line and cuts it at
// MaxQuotedErrorBytes, for a diagnostic that carries an error raised by
// something else.
//
// A diagnostic is rendered one per line, so an embedded newline splits one
// report into several, each after the first lacking severity, code and
// location. yaml.v3 and the overlay validator both write multi-line errors.
//
// Parts are joined with "; ", except after a part ending in a colon, whose next
// line is that header's content. The scan stops at the cap, so the work is
// bounded by what is kept, not by what the library wrote.
func OneLine(err error) string {
	var out strings.Builder
	for rest := err.Error(); rest != "" && out.Len() < MaxQuotedErrorBytes; {
		var line string
		line, rest, _ = strings.Cut(rest, "\n")

		part := strings.Join(strings.Fields(line), " ")
		if part == "" {
			continue
		}
		if out.Len() > 0 {
			if strings.HasSuffix(out.String(), ":") {
				out.WriteString(" ")
			} else {
				out.WriteString("; ")
			}
		}
		out.WriteString(part)
	}
	return cutToCap(out.String())
}

// cutToCap returns msg bounded by MaxQuotedErrorBytes, marked when it cut.
//
// The cut lands on a rune boundary. The bytes are a foreign library's and may be
// multi-byte, and half a rune in a diagnostic is ill-formed text put in front of
// a reader — the one thing a report must not do, and the reason irverify's
// checkUTF8 quotes none of the invalid UTF-8 it finds back.
func cutToCap(msg string) string {
	if len(msg) <= MaxQuotedErrorBytes {
		return msg
	}
	cut := MaxQuotedErrorBytes
	for cut > 0 && !utf8.RuneStart(msg[cut]) {
		cut--
	}
	return msg[:cut] + elidedMarker
}
