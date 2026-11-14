// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package load

import "github.com/dexpace/morphic/compilers/openapi/internal/nodeview"

// settle makes the tree read as the table judged it: each value the table
// refused as the wrong type where it is written, and accepted nowhere another
// place reads it, is read as the field left out by every reader of the source,
// the model among them (docs/compilers/openapi.md#validation). A verbatim copy
// still renders what the source wrote (nodeview.Written). A node two places
// refuse is one node, turned once.
func settle(j judgment) {
	for n := range j.refused {
		if !j.accepted[n] {
			nodeview.Unread(n)
		}
	}
}
