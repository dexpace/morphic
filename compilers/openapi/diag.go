package openapi

import (
	"fmt"

	"github.com/dexpace/morphic/ir"
)

// Stable diagnostic codes emitted by the OpenAPI compiler. Codes are stable
// strings so CI can allowlist them (ir-design §13).
//
//nolint:unused // diagnostic seam consumed by later compiler files
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
	// codeValidationOnlyKeyword reports a validation-only JSON Schema keyword
	// preserved verbatim in Extensions (ir-design §4.7).
	codeValidationOnlyKeyword = "openapi/validation-only-keyword"
	// codeFalseSchema reports a boolean `false` schema (matches nothing).
	codeFalseSchema = "openapi/false-schema"
	// codeNumericPrecision reports a numeric literal that could not be parsed
	// as an exact decimal.
	codeNumericPrecision = "openapi/invalid-numeric-literal"
	// codeDegradedConstruct reports a construct preserved raw because the IR
	// has no structural home for it.
	codeDegradedConstruct = "openapi/degraded-construct"
)

// diagf builds an ir.Diagnostic with a formatted message. It is the single
// constructor for compiler diagnostics so severity, code, and provenance are
// always populated.
//
//nolint:unused // diagnostic seam consumed by later compiler files
func diagf(sev ir.Severity, code string, prov ir.Provenance, format string, args ...any) ir.Diagnostic {
	return ir.Diagnostic{
		Severity:   sev,
		Code:       code,
		Message:    fmt.Sprintf(format, args...),
		Provenance: prov,
	}
}
