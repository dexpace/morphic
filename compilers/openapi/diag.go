package openapi

import (
	"fmt"

	"github.com/dexpace/morphic/ir"
)

// Stable diagnostic codes emitted by the OpenAPI compiler. Codes are stable
// strings so CI can allowlist them (ir-design §13).
const (
	// codeValidation reports a speakeasy validation finding; it is suffixed
	// with the library rule name (e.g. "openapi/validation/duplicate-tag").
	codeValidation = "openapi/validation"
	// codeUnsupportedVersion reports an OpenAPI version the compiler cannot
	// lower.
	codeUnsupportedVersion = "openapi/unsupported-version"
	// codeUnresolvedRef reports a $ref that could not be resolved.
	codeUnresolvedRef = "openapi/unresolved-ref"
	// codeCyclicRef reports a degenerate reference cycle — a recursive YAML
	// anchor or a chain of $ref-only schemas that never reaches a concrete
	// type — caught before it can crash the parser with a stack overflow.
	codeCyclicRef = "openapi/cyclic-ref"
	// codeCycleScanFailed reports that the pre-parse cycle scan did not run to
	// completion — either it aborted (a detector bug) or the document exceeded
	// one of its expansion bounds — leaving its stack-overflow protection
	// incomplete for the source. It is a warning, never a refusal: the compile
	// still proceeds, and every cycle the scan did classify is still caught.
	codeCycleScanFailed = "openapi/cycle-scan-failed"
	// codeValidationOnlyKeyword reports a validation-only JSON Schema keyword
	// kept verbatim under Unmodeled (ir-design §4.7).
	codeValidationOnlyKeyword = "openapi/validation-only-keyword"
	// codeFalseSchema reports a boolean `false` schema (matches nothing).
	codeFalseSchema = "openapi/false-schema"
	// codeNumericPrecision reports a numeric bound literal that is not a finite
	// number (error severity: Morphic owns these keywords, so this is the sole
	// diagnostic for the defect — see boundLiteralDiag).
	codeNumericPrecision = "openapi/invalid-numeric-literal"
	// codeExclusiveBoundForm reports an exclusiveMinimum/exclusiveMaximum whose
	// value form is wrong for the document's dialect (a boolean under 2020-12, or
	// a number under 3.0) — see exclusiveFormDiag.
	codeExclusiveBoundForm = "openapi/invalid-exclusive-bound"
	// codeDegradedConstruct reports a construct the compiler could not carry
	// into the IR as written: preserved raw for want of a structural home,
	// lowered to a weaker shape (a heterogeneous enum as a union, an
	// unconvertible value as the top type), or — for an annotation like a
	// default or example — dropped. It marks the lossy lowerings the compiler
	// reports, not a guarantee that every lossy lowering is reported.
	codeDegradedConstruct = "openapi/degraded-construct"
	// codeCompositionLowering reports that a schema conjoining a structural body
	// with a oneOf/anyOf was lowered by distributing the body across the union
	// variants (ir-design §4.3, §4.8). Nothing is lost, so this records a
	// decision rather than a degradation: the IR's shape no longer mirrors the
	// source's, which is what a reader comparing the two needs told.
	codeCompositionLowering = "openapi/composition-lowering"
	// codeDynamicRefExpanded reports that a $dynamicRef was resolved to the one
	// $dynamicAnchor matching it in this document (ir-design §4.7). Like
	// codeCompositionLowering this records a decision rather than a loss: the
	// reference resolved, but what the source wrote as dynamic is now a fixed
	// target, so a reader comparing IR to source needs telling that the
	// indirection was collapsed at compile time rather than left to evaluation.
	codeDynamicRefExpanded = "openapi/dynamic-ref-expanded"
	// codeConflictingRedecl reports that inline allOf branches redeclare one
	// field with values that disagree: an incompatible target type (string vs.
	// integer) is unsatisfiable outright, while a conflicting constraint
	// keyword (e.g. minimum: 10 vs. exclusiveMinimum: 10) is usually still
	// satisfiable but not representable by a simple merge, so the merge keeps
	// an arbitrary source-order winner — possibly the looser bound — and
	// surfaces the disagreement instead of silently discarding it.
	codeConflictingRedecl = "openapi/conflicting-redeclaration"
	// codeAliasAmplification reports a document whose YAML aliases expand to
	// far more nodes than it declares — a billion-laughs shape that would
	// exhaust memory inside soa.Unmarshal before ResolveAllReferences ever
	// runs (GitHub #27). Unlike codeCycleScanFailed's incomplete-scan warning,
	// this is a positive, measured finding, so the document is refused
	// outright rather than handed to the parser.
	codeAliasAmplification = "openapi/alias-amplification"
	// codeUnattachableRequired reports a composition-scope `required` name (an
	// allOf branch's own required list, or the composed schema's own) that
	// matches none of the model's own properties, so it has no IR home to
	// attach to (ir-design §4.3: Properties holds only own properties, and
	// flattening across Base/Mixins is computed, never stored).
	codeUnattachableRequired = "openapi/unattachable-required"
	// codeInternalInvariant reports that lowering's own pointer-to-registry
	// invariant broke: a pointer named a type ID the registry does not hold, so
	// whatever was about to be attached there had nowhere to go. No source can
	// provoke this — it is a compiler bug — but it is reported rather than
	// dropped in silence, since the alternative is losing constructs with no
	// trace at all.
	codeInternalInvariant = "openapi/internal-invariant"
	// codeDuplicateOperationID reports an operationId claimed by more than one
	// operation, which OpenAPI forbids. A path item mounted at two paths is the
	// shape that reaches this without the document repeating the id in source.
	codeDuplicateOperationID = "openapi/duplicate-operation-id"
	// codeUnpreservableConstruct reports a construct that reached the IR in no
	// form at all: the compiler had no field to model it and its source node could
	// not be converted to JSON either, so Unmodeled could not hold it. It is an
	// error because it is a losslessness failure rather than a degradation —
	// codeDegradedConstruct's constructs survive in a weaker shape, and these
	// survive in none (GitHub #144).
	codeUnpreservableConstruct = "openapi/unpreservable-construct"
)

// diagf builds an ir.Diagnostic with a formatted message. It is the single
// constructor for compiler diagnostics so severity, code, and provenance are
// always populated.
func diagf(sev ir.Severity, code string, prov ir.Provenance, format string, args ...any) ir.Diagnostic {
	return ir.NewDiagnostic(sev, code, fmt.Sprintf(format, args...), prov)
}

// hasErrorDiag reports whether any diagnostic carries error severity. The load
// phase uses it to tell a refusal (a real spec problem, e.g. a degenerate cycle)
// from advisory warnings it must carry forward rather than abort on.
func hasErrorDiag(diags []ir.Diagnostic) bool {
	return ir.HasError(diags)
}
