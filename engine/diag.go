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
// could not be read or a compiler broke its contract.
//
// Detection reports under no engine code: the engine parses nothing, so why a
// source could not be read belongs to the compiler that recognized it
// (openapi/undecodable-source).
const (
	// codeUnrecognizedFormat: no compiler claimed the source, and none of them
	// had anything to say about why.
	codeUnrecognizedFormat = "engine/unrecognized-format"
	// codeNoCompilerForFormat: a compiler read the source and named a format, but
	// none is registered for it. An OpenAPI version outside the supported range
	// lands here, as does Swagger 2.0, which is recognized and not yet lowered.
	codeNoCompilerForFormat = "engine/no-compiler-for-format"
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

// mergeDiagnostics returns stored followed by every diagnostic in produced that
// stored does not already hold. Identity is the whole value (severity, code,
// message and provenance), the same identity compilers/compile dedupes on.
//
// A compiler hands its findings back on two channels and need fill only one, so
// assigning either list over the other would silently lose whatever the loser
// held. Merging keeps both.
//
// The merged slice is freshly allocated when there is anything to merge: stored
// and produced routinely alias one another, and appending into a shared backing
// array would overwrite entries still to be read.
func mergeDiagnostics(stored, produced []ir.Diagnostic) []ir.Diagnostic {
	if len(produced) == 0 {
		return stored
	}

	held := make(map[ir.Diagnostic]struct{}, len(stored))
	for _, d := range stored {
		held[d] = struct{}{}
	}

	merged := make([]ir.Diagnostic, len(stored), len(stored)+len(produced))
	copy(merged, stored)
	for _, d := range produced {
		if _, dup := held[d]; dup {
			continue
		}
		merged = append(merged, d)
	}
	return merged
}
