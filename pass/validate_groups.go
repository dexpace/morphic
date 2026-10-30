// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package pass

import (
	"fmt"

	"github.com/dexpace/morphic/ir"
)

// checkGroupWalkTruncated reports that operation-group nesting exceeded the
// bounded walk.
//
// The bound itself is deliberate, but an unreported bound makes every check that
// reaches operations through it describe a subset of the document while Validate
// still returns "internally consistent". The sibling reference walk reports its
// own cap the same way.
func checkGroupWalkTruncated(doc *ir.Document) []ir.Diagnostic {
	if !ir.ForEachOperation(doc, func(ir.Operation) {}) {
		return nil
	}
	return []ir.Diagnostic{diag(ir.SeverityError, "ir/walk-truncated",
		fmt.Sprintf("operation groups nest deeper than %d; some operations went unchecked", ir.MaxGroupDepth),
		ir.DocumentPath)}
}
