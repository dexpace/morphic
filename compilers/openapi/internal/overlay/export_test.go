// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package overlay

import (
	"context"

	yaml "go.yaml.in/yaml/v3"

	"github.com/dexpace/morphic/compilers/openapi/internal/budget"
	"github.com/dexpace/morphic/ir"
)

// applyWithin is Apply, at index 1, with the attribution walk's node bound
// alone lowered, the selectors held to MaxSteps.
func applyWithin(ctx context.Context, root, doc *yaml.Node, opts Options, walks budget.Limit) (Origin, []ir.Diagnostic) {
	return applyWithLimits(ctx, 1, root, doc, opts, walks, budget.AtMost(MaxSteps))
}
