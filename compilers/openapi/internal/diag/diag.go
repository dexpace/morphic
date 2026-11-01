// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package diag

import (
	"fmt"

	"github.com/dexpace/morphic/ir"
)

// The stable codes the OpenAPI compiler reports under. They are stable strings
// so CI can allowlist them (docs/ir/provenance.md).
const (
	// Validation reports a document breaking a rule of the OpenAPI Specification
	// at its declared minor, which the load phase's rule table finds; it is
	// suffixed with the finding's class (e.g.
	// "openapi/validation/validation-required-field").
	Validation = "openapi/validation"
	// UnsupportedVersion reports an OpenAPI version the compiler cannot lower.
	UnsupportedVersion = "openapi/unsupported-version"
	// UnresolvedRef reports a $ref that could not be resolved, or a reference
	// left unresolved because its pointer passes through the fields beside a path
	// item $ref, which the resolver reads in the item that $ref names (GitHub
	// #824).
	UnresolvedRef = "openapi/unresolved-ref"
	// CyclicRef reports a degenerate cycle. A recursive YAML anchor is refused
	// before the parser library overflows its stack on it
	// (docs/parser-library.md#alias-cycle). A loop of schema $refs that never
	// reaches a schema is reported once, at its least member the source writes,
	// else at the least source $ref leading into it (GitHub #912), and the
	// document still compiles: the compiler binds schema $refs itself
	// (docs/parser-library.md#schema-cycle-overflow). The resolver reports a
	// chain of reference objects itself, as UnresolvedRef
	// (docs/parser-library.md#reference-object-cycle).
	CyclicRef = "openapi/cyclic-ref"
	// CycleScanFailed reports that the pre-parse alias scan aborted on a fault
	// of its own (scan.Aliases), leaving its protection against a recursive
	// anchor incomplete for the source. It is a warning: the compile still
	// proceeds.
	CycleScanFailed = "openapi/cycle-scan-failed"
	// SourceTooLarge reports a document with more YAML nodes than the pre-parse
	// scan indexes (sourceindex.MaxIndexedNodes). Every answer the index gives
	// about such a document is a partial one, including the node count the
	// alias-expansion allowance is derived from, so the document is refused rather
	// than scanned against a bound computed from a count that stopped early.
	SourceTooLarge = "openapi/source-too-large"
	// SourceTooDeep reports a document nested deeper than the pre-parse scan
	// admits (scan.MaxNesting), its aliases expanded. What the parser builds
	// costs more per node the deeper the node, so the document is refused before
	// the parser builds it, at the least pointer where its nesting passes the
	// bound (GitHub #905). An overlay document is refused alike before it is
	// applied, and one an external reference names, failing the reference.
	SourceTooDeep = "openapi/source-too-deep"
	// TaggedMapping reports a mapping carrying a YAML tag other than !!map, which
	// OpenAPI forbids: its YAML form limits tags to YAML 1.2's JSON schema
	// ruleset. At a reference position whose model is a struct (a request body,
	// response, parameter and the like) the parser leaves the model unbuilt and
	// dereferences it, on a goroutine no recover here reaches
	// (docs/parser-library.md#tagged-mapping-panic, GitHub #474).
	//
	// The document is refused before parsing whatever the position, since telling
	// the faulting ones apart would mean copying the parser's object model, at
	// the tagged mapping with the least pointer. A tagged scalar is not refused
	// (GitHub #245).
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
	// verbatim under Unmodeled
	// (docs/ir/boundaries.md#validation-only-constructs).
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
	// DuplicateEnumValue reports an `enum` entry whose value an earlier entry
	// already holds, at the repeat's own position, whether the set lowers to an
	// Enum or a Union. Every occurrence is kept, since dropping one would lose a
	// declaration; in an Enum each repeat's member ID ends in its occurrence
	// number. Entries compare as JSON Schema compares instances: a number by its
	// value, an object regardless of member order.
	//
	// Warning: the document is well-formed, and JSON Schema's own uniqueness
	// requirement on enum is a SHOULD.
	DuplicateEnumValue = "openapi/duplicate-enum-value"
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
	// DuplicateParameter reports a parameter list entry repeating the name and
	// location of an earlier entry in the same list, its $refs resolved, which
	// OpenAPI forbids. The rule table states the rule and leaves it here, where
	// every entry is read through its $ref: every list the source writes is
	// checked whether or not it lowers (load), and a list another document
	// writes where it lowers (the parameter lowering). A lowered list keeps
	// every occurrence, each repeat's ID ending in its occurrence number.
	//
	// An error, as the specification forbids the repeat.
	DuplicateParameter = "openapi/duplicate-parameter"
	// DuplicateServerName reports a servers list entry whose name an earlier
	// entry in the same list already has, compared as written, at the repeat's
	// name. Load checks every list the source writes, the document's, a path
	// item's or an operation's, lowered or not, but none only a $ref reaches
	// (GitHub #880). Both entries are kept, though a consumer keying servers by
	// name cannot tell them apart. Names equal only once canonicalized, and URL
	// hints, are not reported: an emitter uniquifies them.
	//
	// Warning, where DuplicateParameter is an error: OpenAPI 3.2 calls the name
	// "an optional unique string" with no RFC 2119 keyword.
	DuplicateServerName = "openapi/duplicate-server-name"
	// InvalidMethodKey reports an additionalOperations key that names no method:
	// the empty string. The operation still lowers, binding the key as written, so
	// nothing the entry declares is lost — what is reported is that the binding's
	// method is unusable. speakeasy rejects a key naming a *standard* method, which
	// belongs in its own field, but accepts this one.
	InvalidMethodKey = "openapi/invalid-method-key"
	// DegradedConstruct reports a construct the compiler could not carry into the
	// IR as written: preserved raw for want of a structural home, lowered to a
	// weaker shape (a heterogeneous enum as a union, an unconvertible value as
	// the top type), or dropped — an annotation like a default or example, or
	// the fields beside a path item $ref the load phase's walk does not reach.
	// It marks the lossy lowerings the compiler reports, not a guarantee that
	// every lossy lowering is reported.
	DegradedConstruct = "openapi/degraded-construct"
	// CompositionLowering reports that a schema conjoining a structural body with
	// a oneOf/anyOf was lowered by distributing the body across the union
	// variants (docs/ir/models.md#composition-beside-a-union,
	// docs/compilers/openapi.md#union-beside-structure). Nothing is lost, so
	// this records a decision rather than a degradation: the IR's shape no
	// longer mirrors the source's, which is what a reader comparing the two
	// needs told.
	CompositionLowering = "openapi/composition-lowering"
	// DynamicRefExpanded reports that a $dynamicRef was resolved to the one
	// $dynamicAnchor matching it in this document
	// (docs/compilers/openapi.md#dynamic-ref). Like CompositionLowering this
	// records a decision rather than a loss: the reference resolved, but what the
	// source wrote as dynamic is now a fixed target, so a reader comparing IR to
	// source needs telling that the indirection was collapsed at compile time
	// rather than left to evaluation.
	DynamicRefExpanded = "openapi/dynamic-ref-expanded"
	// ConflictingRedecl reports that inline allOf branches redeclare one field
	// with disagreeing values. The judgment finds the types narrower (no
	// diagnostic), overlapping (a DegradedConstruct: shared values the merge does
	// not narrow) or disjoint; only disjoint types, a scalar against a structure
	// and two structures of different instance types conflict. A conflicting
	// constraint keyword (minimum: 10 vs. exclusiveMinimum: 10) is usually
	// satisfiable, but no simple merge states it, so the merge keeps a
	// source-order winner.
	//
	// Either way the losing declaration is kept whole under the merged property's
	// Unmodeled (merge.keepLosingDeclaration); #10 records intersecting bounds.
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
	// for small documents) — a billion-laughs shape that would exhaust memory
	// inside soa.Unmarshal (docs/parser-library.md#alias-expansion, GitHub #27).
	// A positive, measured finding, unlike CycleScanFailed, it refuses the
	// document outright, at the least pointer of the smallest structures past
	// the ratio.
	//
	// It is a ratio and no caller's budget, so no setting admits a document it
	// names. A document inside the ratio whose aliases still add more nodes than
	// openapi.Limits.MaxAliasSurplus is BudgetExceeded instead, and one past both
	// is this.
	AliasAmplification = "openapi/alias-amplification"
	// BudgetExceeded reports an input that crossed a budget. openapi.Limits sets
	// most: a document's bytes or nodes (an overlay's included), alias nodes, an
	// enum's members (GitHub #75), external documents. Fixed bounds cover load's
	// schema $refs, chains through other documents and rule checks, detection's
	// merges, and the declarations of one field the allOf merge judges.
	//
	// None is the input's fault, unlike AliasAmplification's bomb. Each is an
	// error: the document is refused, validation stops, an enum loses its members
	// (UnpreservableConstruct), what was not reached is unresolved, or what is
	// past the bound is kept verbatim, unlowered.
	BudgetExceeded = "openapi/budget-exceeded"
	// InternalInvariant reports that one of the compiler's own invariants broke:
	// a pointer named a type ID the registry does not hold, a second resolution
	// read a document load never prepared, or the library faulted while load put
	// the $refs it respelled back as written. It is a compiler bug, not a problem
	// with the source, but it is reported rather than dropped in silence, since
	// the alternative is losing constructs with no trace at all. An entry the
	// registry refused is compile.RegistryRefused instead.
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
	// PathItemRefCollision reports a construct declared beside a path item's
	// $ref that the item it references declares too, or the fields beside a
	// later $ref of its chain: a method or additionalOperations key, summary,
	// description, servers, x-* name, undeclared key, or item-level (name, in)
	// parameter.
	//
	// OpenAPI does not say which declaration wins, so the one nearer the use
	// site does, as for a $ref-adjacent schema sibling, and the other is not
	// lowered at that mount. A warning, as InvalidStatusKey is: the document
	// still lowers, nothing is lost unnamed, and an error would stop
	// harness.Check early (GitHub #577).
	PathItemRefCollision = "openapi/path-item-ref-collision"
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
	// OAuth2NoFlow reports an oauth2 securitySchemes entry that states no way to
	// obtain a token: its flows object declares no flow — `flows: {}`, or only
	// x-* and keys that name no flow — and it has no oauth2MetadataUrl. Every
	// flow is optional, so the loader accepts that shape; a `flows` its
	// REQUIRED-field rule finds missing is that rule's to report (GitHub #646).
	// Placed at the declaration, which its same-document aliases share (#874).
	//
	// Reported, not refused as IncompleteSecurityScheme is: the IR states exactly
	// what the document said, and refusing would drop the entry's text. A warning,
	// like ReservedHeaderName, since the document lowers whole.
	OAuth2NoFlow = "openapi/oauth2-no-flow"
	// EmptyRequestContent reports a request body whose content map is empty, at
	// the map. Every minor's schema admits it, and OpenAPI 3.2 says the map
	// SHOULD hold an entry, leaving a body with none implementation-defined
	// (GitHub #622). The body lowers to a Payload with no Contents, keeping
	// whether it must be sent, its description and its x-*. A content the
	// parser finds missing is the parser's to report.
	//
	// A warning, as OAuth2NoFlow is: the document lowers whole, but states no
	// way to send the body.
	EmptyRequestContent = "openapi/empty-request-content"
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
	// UnknownSchemaKeyword reports a JSON Schema keyword the schema model does
	// not name, or names but the schema's dialect does not define (`nullable`
	// with the nullable-widening policy off), kept verbatim under Unmodeled.
	//
	// Info, because the document did nothing wrong: JSON Schema requires an
	// implementation to ignore a keyword it does not recognize, and says such a
	// keyword may carry meaning for other tooling, so it is legal input rather
	// than a defect. What is recorded is this compiler's own
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
	// InvalidLocationKeyword reports a style, explode or allowReserved at in:
	// querystring, at the keyword. OpenAPI 3.2 binds the whole query string from
	// the parameter's content there and states its serialization through the
	// media type alone, so none of the three has anything left to qualify.
	//
	// A warning, though 3.2 says MUST NOT: the value lowers as declared, since
	// dropping stated content is an emitter's call (invariant 2), and what the
	// parameter means is the content's to say, which it does.
	InvalidLocationKeyword = "openapi/invalid-location-keyword"
	// InapplicableLocationKeyword reports a Parameter Object field written where
	// it has no effect, at the keyword: an allowReserved where its document's
	// version does not apply it — before 3.2 anywhere but in: query, in 3.2 a
	// header or a cookie in style cookie — and an allowEmptyValue under a style
	// that cannot write an empty value. A code of its own, apart from
	// InvalidLocationKeyword, because no MUST is broken: the keyword simply has
	// no effect, and a consumer may weigh that differently. A warning, and the
	// value lowers as declared (invariant 2).
	InapplicableLocationKeyword = "openapi/inapplicable-location-keyword"
	// MissingArrayItems reports a schema in an OpenAPI 3.0 document that writes
	// type array and no `items`, which 3.0 requires, at the schema's pointer. It
	// lowers as it would in 3.1, where `items` is optional: a List's element is
	// the top type. A warning rather than an error: nothing the document
	// declared is dropped, and harness.Check stops at the first error
	// diagnostic. A code of its own rather than DegradedConstruct, because
	// nothing is kept in a weaker shape.
	MissingArrayItems = "openapi/missing-array-items"
	// LaterVersionConstruct reports a field, or a value of one, that a later
	// OpenAPI minor than the document declares added to the object it is written
	// on, or a keyword beside a 3.0 schema's $ref, which 3.1 applies. It is
	// lowered as in a document declaring that minor, the reading that loses the
	// least: from the lowering and this diagnostic a consumer can set it aside,
	// while from a verbatim copy it could not recover the lowering
	// (docs/compilers/openapi.md#later-version-construct).
	//
	// Warning, as UnknownObjectKey is: an object's key set is closed, so the
	// document is invalid for the version it declares, but it still lowers.
	LaterVersionConstruct = "openapi/later-version-construct"
	// ItemSchemaOnDocument reports an itemSchema on a media type whose body is one
	// document, so it has no sequence of items to describe
	// (docs/compilers/openapi.md#item-schema). It still lowers to Content.Item.
	//
	// Warning, not a validation finding: the specification says what itemSchema
	// describes and forbids no media type, so the document stays valid.
	ItemSchemaOnDocument = "openapi/item-schema-on-document"
	// InvalidPathKey reports a Paths Object key that breaks OpenAPI's path
	// templating rules, those of 3.2.0 §4.8.2 read at every version: no leading
	// "/", a character a path admits only percent-encoded, a brace outside a
	// template expression, an expression used twice, an empty segment, or a
	// query. One warning per key names each, at the key's path item.
	//
	// Warning, as InvalidStatusKey is: the key is used as written, and an error
	// would stop harness.Check before later findings. A "#" fragment is GitHub
	// #602's to report, so nothing from it on is read, and matching the
	// expressions to path parameters is #640's.
	InvalidPathKey = "openapi/invalid-path-key"
	// InferredNullable reports a null the nullable-widening policy reads in
	// where the document's version does not: a `nullable: true` in a 3.1 or 3.2
	// schema, beside a $ref, or in a 3.0 schema with no type beside it or a
	// keyword that excludes null, at the keyword; or keywords beside a $ref that
	// admit null over a target that does not, at the schema writing them. Each
	// is reported once, and each reference whose null rests on it is marked
	// Inferred (docs/compilers/openapi.md#nullable). Info: the reading is the
	// document's evident intent, and the mark keeps the declared one.
	InferredNullable = "openapi/inferred-nullable"
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
