package graphql

import (
	"fmt"

	"github.com/dexpace/morphic/ir"
)

// Stable diagnostic codes emitted by the GraphQL compiler. Codes are stable
// slash-namespaced strings so CI can allowlist them (ir-design §13).
const (
	// codeParse reports an SDL syntax error the parser could not recover from;
	// the compiler refuses to lower the document rather than crashing.
	codeParse = "graphql/parse"
	// codeUnknownType reports a type reference that resolves to neither a defined
	// type nor a built-in scalar; the validate pass reports the dangling ref.
	codeUnknownType = "graphql/unknown-type"
	// codeDuplicateType reports a type name defined more than once; the first
	// definition wins and later ones are recorded verbatim in Extensions.
	codeDuplicateType = "graphql/duplicate-type"
	// codeInvalidValue reports a literal value that could not be lowered exactly
	// (e.g. a numeric literal that is not a valid decimal).
	codeInvalidValue = "graphql/invalid-value"
	// codeDegradedConstruct reports a construct preserved raw because the IR has
	// no structural home for it, or a bound that guarded pathological input.
	codeDegradedConstruct = "graphql/degraded-construct"
)

// diagf builds an ir.Diagnostic with a formatted message. It is the single
// constructor for compiler diagnostics so severity, code, and provenance are
// always populated.
func diagf(sev ir.Severity, code string, prov ir.Provenance, format string, args ...any) ir.Diagnostic {
	return ir.Diagnostic{
		Severity:   sev,
		Code:       code,
		Message:    fmt.Sprintf(format, args...),
		Provenance: prov,
	}
}
