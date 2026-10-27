package detect

import (
	"context"

	"github.com/dexpace/morphic/compilers"
	"github.com/dexpace/morphic/compilers/openapi/internal/budget"
	"github.com/dexpace/morphic/compilers/openapi/internal/diag"
	"github.com/dexpace/morphic/compilers/openapi/internal/load"
	"github.com/dexpace/morphic/compilers/openapi/internal/nodeview"
	"github.com/dexpace/morphic/ir"
)

// Recognize reads the dialect data declares, data being the source at srcIndex:
// the discriminating key it writes and its version's major.minor prefix, with
// the parse that read them. maxBytes bounds that parse; ctx stops it,
// declining silently.
//
// Two sources are declined with an error whatever their format: one past
// maxBytes, unread, with the refusal load.Load gives it, and one whose root
// merges past the depth the read expands, its own keys leaving the version
// open (settledByOwnPairs). Any other decline is silent unless the source is
// unreadable and declares a version where a document declares one
// (declaresProbeKey).
func Recognize(ctx context.Context, srcIndex int, data []byte, maxBytes budget.Limit) (compilers.Recognition, []ir.Diagnostic, bool) {
	atSource := ir.Provenance{Source: srcIndex}
	if d, over := load.OverByteBudget(atSource, data, maxBytes); over {
		return compilers.Recognition{}, []ir.Diagnostic{d}, false
	}

	probe, parsed, err := sniff(ctx, data)
	switch {
	case ctx.Err() != nil:
		return compilers.Recognition{}, nil, false
	case probe.Cut:
		// Declining in silence would let the bound pass for an answer.
		return compilers.Recognition{}, []ir.Diagnostic{diag.Newf(
			ir.SeverityError, diag.BudgetExceeded, atSource,
			"source cannot be recognized: its root mapping merges through `<<` deeper than %d levels, "+
				"past which a version it declares is not read", nodeview.MergeDepthLimit)}, false
	case probe.OpenAPI != "":
		return recognized("openapi", probe.OpenAPI, parsed), nil, true
	case probe.Swagger != "":
		return recognized("swagger", probe.Swagger, parsed), nil, true
	case err != nil && declaresProbeKey(data):
		return compilers.Recognition{}, []ir.Diagnostic{diag.Newf(
			ir.SeverityError, diag.UndecodableSource, atSource,
			"source declares an OpenAPI or Swagger version and cannot be read: %s", diag.OneLine(err))}, false
	default:
		return compilers.Recognition{}, nil, false
	}
}

// recognized builds the recognition for a source that declared version under
// name, carrying the parse that read it.
//
// parsed is never nil here. A probe names a key only when decodeYAML read it
// off a parsed root, so a recognition always has a parse to carry — which
// matters, since a nil *load.Parsed put into the interface would make one that
// is not nil, and a consumer's type assertion would accept it and read through.
func recognized(name, version string, parsed *load.Parsed) compilers.Recognition {
	return compilers.Recognition{
		Format: compilers.SourceFormat{Name: name, Version: majorMinor(version)},
		Parsed: parsed,
	}
}
