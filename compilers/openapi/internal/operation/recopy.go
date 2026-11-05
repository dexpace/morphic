// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package operation

import (
	"strings"

	"github.com/dexpace/morphic/compilers/openapi/internal/annotation"
	"github.com/dexpace/morphic/compilers/openapi/internal/lowering"
	"github.com/dexpace/morphic/ir"
)

// chargeItemCopy charges the run's expansion budget, in c, the mount's
// context, what an additionalOperations entry copies of what the mount's item
// declares itself: kept, the bytes of the entries kept onto it; servers, those
// of the servers it takes; and the item's inline parameters among taken, those
// the merge left it. Declared charges an item's $ref'd parameters at every
// operation. These grow as 3.2's additionalOperations entries times what the
// item declares (GitHub #1428), as no fixed method's do. Every entry is
// charged, the first too, so a mount's charge is the same in any order.
func chargeItemCopy(c lowering.Ctx, kept, servers int, taken []sourcedParam) {
	lowering.ChargeExpansion(c, kept+servers)
	for _, sp := range taken {
		if sp.ref.IsReference() {
			continue
		}
		pc := c
		if sp.in != nil {
			pc = *sp.in
		}
		if p := sp.ref.GetObject(); p != nil {
			lowering.ChargeExpansionAt(pc, sp.pointer, p.GetRootNode())
		}
	}
}

// keptWeight returns what a copy of kept weighs: for each entry, the keyword
// the item wrote, past the scope pathItemScope puts it under, a byte for the
// key and the raw value held, so a copy weighs about what its source does. It
// sums the lengths of what memory holds, which no int overflows.
func keptWeight(kept ir.Unmodeled) int {
	prefix := annotation.EntryKey(pathItemScope, "")
	weight := 0
	for key, entry := range kept {
		weight += len(strings.TrimPrefix(key, prefix)) + 1 + len(entry.Value)
	}
	return weight
}

// serversWeight returns the bytes of the IDs named, which an operation naming
// no servers of its own takes from its mount.
func serversWeight(named []ir.ServerID) int {
	weight := 0
	for _, id := range named {
		weight += len(id)
	}
	return weight
}
