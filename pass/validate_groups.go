// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package pass

import (
	"fmt"

	"github.com/dexpace/morphic/ir"
	"github.com/dexpace/morphic/ir/irfacts"
)

// checkGroupWalkTruncated reports that the walk of operation groups stopped at a
// bound, nesting past ir.MaxGroupDepth or groups and operations past
// ir.MaxWalkSteps, where the reference walk did not: a document past both is
// reported once, by checkDanglingRefs.
//
// The bounds are deliberate, but an unreported one makes every check that
// reaches operations through them describe a subset of the document while
// Validate still returns "internally consistent". facts says whether its walk
// of the operations stopped, which is the walk those checks share.
func checkGroupWalkTruncated(facts *irfacts.Index, referencesTruncated bool) []ir.Diagnostic {
	if referencesTruncated || !facts.Partial() {
		return nil
	}
	return []ir.Diagnostic{diag(ir.SeverityError, "ir/walk-truncated",
		fmt.Sprintf("operation groups nest deeper than %d or hold more than %d groups and operations; "+
			"some operations went unchecked", ir.MaxGroupDepth, ir.MaxWalkSteps),
		ir.DocumentPath)}
}
