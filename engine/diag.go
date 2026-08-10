package engine

import (
	"fmt"

	"github.com/dexpace/morphic/ir"
)

// Diagnostic codes the engine itself raises, in its own stable namespace beside
// the compilers' (openapi/*) and the passes' (ir/*), so a CI wrapper can
// allowlist them by name.
//
// Each reports a problem with the source the caller named, not with the call.
// They exist because the pipeline can reject a spec before any compiler runs,
// and a rejection is a finding about the spec whichever stage makes it. A Go
// error out of Run means something other than the spec went wrong: the file
// could not be read, or a compiler broke its own contract.
// Detection itself reports under no engine code. The engine parses nothing, so
// the account of why a source could not be read belongs to the compiler that
// recognized it — openapi/undecodable-source, for one that declares an OpenAPI
// key and will not parse. An engine code here would have to describe every
// format at once, and would be wrong for the first one that is not YAML.
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
// The provenance is deliberately positionless. These are raised before anything
// is lowered, so there is no document whose Sources table an index could
// address, and ir.NoSource is the IR's value for a finding that names no entry
// in one. An index invented here would have a renderer point at a file the
// finding is not about.
func specProblem(code, format string, args ...any) ir.Diagnostic {
	return ir.NewDiagnostic(ir.SeverityError, code, fmt.Sprintf(format, args...),
		ir.Provenance{Source: ir.NoSource})
}

// mergeDiagnostics returns stored followed by every diagnostic in produced that
// stored does not already hold. Identity is the whole value — severity, code,
// message and provenance — the same identity compilers/compile dedupes on.
//
// It exists because a compiler hands its findings back on two channels and is
// obliged to fill only one of them. Assigning either list over the other loses
// whatever the loser held, in silence and at every severity; merging keeps both
// and hands a compiler that fills them alike, as the OpenAPI one does, back
// exactly what it gave.
//
// The merged slice is always freshly allocated when there is anything to merge:
// stored and produced routinely alias one another, and appending into a shared
// backing array would overwrite entries still to be read.
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
