// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package engine

import (
	"fmt"

	"github.com/dexpace/morphic/ir"
)

// Diagnostic codes the engine itself raises, in its own stable namespace beside
// the compilers' (openapi/*) and the passes' (ir/*), so a CI wrapper can
// allowlist them by name.
//
// Each reports a problem with the source the caller named, which the pipeline
// can reject before any compiler runs. A Go error out of Run means the file
// could not be read or a compiler or pass broke its contract.
//
// Detection reports under no engine code: the engine parses nothing, so why a
// source could not be read belongs to the compiler that recognized it
// (openapi/undecodable-source).
const (
	// CodeUnrecognizedFormat reports a source no compiler claimed, none of them
	// having anything to say about why.
	CodeUnrecognizedFormat = "engine/unrecognized-format"
	// CodeNoCompilerForFormat reports a source a compiler read and named the
	// format of, which no registered compiler serves. An OpenAPI version outside
	// the supported range lands here, as does Swagger 2.0, which is recognized
	// and not yet lowered.
	CodeNoCompilerForFormat = "engine/no-compiler-for-format"
)

// specProblem builds an error-severity diagnostic about a source as a whole.
// Error is the severity because nothing was lowered: the spec reached the IR in
// no form at all.
//
// It names the spec as Source 0 of the table Run reports beside it, and no
// position in it: these are raised before anything is lowered, about the file
// as a whole.
func specProblem(code, format string, args ...any) ir.Diagnostic {
	return ir.NewDiagnostic(ir.SeverityError, code, fmt.Sprintf(format, args...),
		ir.Provenance{Source: 0})
}
