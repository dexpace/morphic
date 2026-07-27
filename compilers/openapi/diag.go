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
	// preserved verbatim in Extensions (ir-design §4.7).
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
	// codeDegradedConstruct reports a construct preserved raw because the IR
	// has no structural home for it.
	codeDegradedConstruct = "openapi/degraded-construct"
	// codeConflictingRedecl reports that inline allOf branches redeclare one
	// field with definitions that disagree — a differing target type or a
	// constraint keyword pinned to incompatible values. allOf is an
	// intersection, so what "disagree" means differs by kind: an incompatible
	// target type genuinely describes an unsatisfiable field (string and
	// integer cannot both hold), but a contradictory constraint keyword is
	// normally still satisfiable on its own (maxLength: 10 and maxLength: 20
	// together simply mean 10; minimum: 10 and exclusiveMinimum: 10 together
	// mean "> 10") — the real defect there is that the merge cannot represent
	// the intersection of the two keyword values, so it keeps an arbitrary
	// source-order winner that may be the looser of the two and so silently
	// weaken the validation the spec intended. Either way, the merge keeps the
	// first declaration's value but surfaces the disagreement rather than
	// discarding it.
	codeConflictingRedecl = "openapi/conflicting-redeclaration"
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
