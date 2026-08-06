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
	// CyclicRef reports a degenerate reference cycle — a recursive YAML anchor, a
	// chain of $ref-only schemas that never reaches a concrete type, or a
	// reference whose pointer resolves through a reference already being resolved
	// — caught before it can crash the parser with a stack overflow or deadlock
	// the resolver on a lock its own goroutine holds.
	CyclicRef = "openapi/cyclic-ref"
	// CycleScanFailed reports that the pre-parse cycle scan did not run to
	// completion — either it aborted (a detector bug) or the document exceeded one
	// of its expansion bounds — leaving its stack-overflow protection incomplete
	// for the source. It is a warning, never a refusal: the compile still
	// proceeds, and every cycle the scan did classify is still caught.
	CycleScanFailed = "openapi/cycle-scan-failed"
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
	// with values that disagree: an incompatible target type (string vs. integer)
	// is unsatisfiable outright, while a conflicting constraint keyword (e.g.
	// minimum: 10 vs. exclusiveMinimum: 10) is usually still satisfiable but not
	// representable by a simple merge, so the merge keeps an arbitrary
	// source-order winner — possibly the looser bound — and surfaces the
	// disagreement instead of silently discarding it.
	ConflictingRedecl = "openapi/conflicting-redeclaration"
	// DisjointVisibility reports inline allOf branches restricting one field to
	// lifecycle sets sharing nothing — readOnly on one branch, writeOnly on
	// another — so the intersection allOf calls for admits no lifecycle at all.
	// It is neither of its neighbours: ConflictingRedecl keeps an arbitrary
	// source-order winner, and DegradedConstruct lowers to a weaker shape,
	// whereas Visibility{None: true} is the exact intersection and a shape the
	// IR already has. It is reported nonetheless, because a field no request or
	// response can carry is a composition that cannot take effect, which is
	// seldom what the document set out to say.
	DisjointVisibility = "openapi/disjoint-visibility"
	// AliasAmplification reports a document whose YAML aliases expand to far more
	// nodes than it declares — a billion-laughs shape that would exhaust memory
	// inside soa.Unmarshal before ResolveAllReferences ever runs (GitHub #27).
	// Unlike CycleScanFailed's incomplete-scan warning, this is a positive,
	// measured finding, so the document is refused outright rather than handed to
	// the parser.
	AliasAmplification = "openapi/alias-amplification"
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
	// DuplicateOperationID reports an operationId claimed by more than one
	// operation, which OpenAPI forbids. A path item mounted at two paths is the
	// shape that reaches this without the document repeating the id in source.
	DuplicateOperationID = "openapi/duplicate-operation-id"
	// ReservedHeaderParam reports a header parameter named Accept, Content-Type or
	// Authorization, which OpenAPI §4.8.12 says SHALL be ignored: each duplicates
	// something the protocol layer already owns — content negotiation, the request
	// body's media type, the security scheme's credential.
	//
	// The compiler keeps the parameter, because dropping declared content is a
	// loss and choosing between the two is an emitter's call, not a compiler's
	// (invariant 2). The diagnostic is what makes the deviation from the SHALL
	// visible, so an emitter can suppress the parameter rather than generate one
	// that fights the scheme it collides with (GitHub #39).
	//
	// Unconditional, and deliberately not behind an Options switch: invariant 6
	// governs what is *inferred*, and nothing here is. The three names are fixed
	// by the specification, the comparison is against a declared name, and the
	// document lowers byte-for-byte the same whether or not this fires — so there
	// is no inference to mark Inferred and no semantics to disable. Warning
	// rather than info because the document really did write something the spec
	// says has no effect; error is wrong twice over, since the document is
	// well-formed and harness.Check stops at the first error diagnostic, which
	// would hide every later finding in the same spec.
	ReservedHeaderParam = "openapi/reserved-header-parameter"
	// UnpreservableConstruct reports a construct that reached the IR in no form at
	// all: the compiler had no field to model it and its source node could not be
	// converted to JSON either, so Unmodeled could not hold it. It is an error
	// because it is a losslessness failure rather than a degradation —
	// DegradedConstruct's constructs survive in a weaker shape, and these survive
	// in none (GitHub #144).
	UnpreservableConstruct = "openapi/unpreservable-construct"
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
