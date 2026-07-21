package protobuf

import (
	"fmt"

	"github.com/dexpace/morphic/ir"
)

// Stable diagnostic codes emitted by the protobuf compiler. Codes are stable
// strings so CI can allowlist them (ir-design §13).
const (
	// codeCompile reports a hard failure to parse or link the .proto source; the
	// document cannot be lowered.
	codeCompile = "protobuf/compile-error"
	// codeUnresolvedImport reports an import the compiler could not resolve —
	// any import other than a bundled well-known type, since the compiler
	// receives a single root file and does no file I/O.
	codeUnresolvedImport = "protobuf/unresolved-import"
	// codeWarning reports a non-fatal finding surfaced by the parser.
	codeWarning = "protobuf/warning"
	// codeReserved reports reserved field numbers/names preserved verbatim in
	// Extensions and guarded by the validate pass (ir-design §14 protobuf row).
	codeReserved = "protobuf/reserved"
	// codeCustomOptionDefinition reports an extension that defines a custom option
	// (extend google.protobuf.*Options) rather than a data field; its identity is
	// preserved but it contributes no model property.
	codeCustomOptionDefinition = "protobuf/custom-option-definition"
	// codeDegradedConstruct reports a construct preserved raw because the IR has no
	// structural home for it.
	codeDegradedConstruct = "protobuf/degraded-construct"
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
