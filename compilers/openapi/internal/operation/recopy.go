// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package operation

import (
	"math"
	"strings"

	"github.com/dexpace/morphic/compilers/openapi/internal/annotation"
	"github.com/dexpace/morphic/compilers/openapi/internal/lowering"
	"github.com/dexpace/morphic/ir"
)

// recopied charges the run's expansion budget what an operation past its
// mount's first copies of what the mount's item declares itself: kept, the
// bytes of the entries kept onto it; servers, those of the servers it takes;
// and the item's inline parameters among taken, those the merge left it.
// Declared charges an item's $ref'd parameters at every operation. OpenAPI
// 3.2's additionalOperations admits any number of operations, so these grow as
// operations times what the item declares (GitHub #1428). It reports whether
// the copies so far are within the budget, read in c, the mount's context.
func recopied(c lowering.Ctx, kept, servers int, taken []sourcedParam) bool {
	if !lowering.Recopied(c, sum(kept, servers)) {
		return false
	}
	for _, sp := range taken {
		if sp.ref.IsReference() {
			continue
		}
		pc := c
		if sp.in != nil {
			pc = *sp.in
		}
		if p := sp.ref.GetObject(); p != nil && !lowering.RecopiedAt(pc, sp.pointer, p.GetRootNode()) {
			return false
		}
	}
	return true
}

// keptWeight returns what a copy of kept weighs: for each entry, the keyword
// the item wrote, past the scope pathItemScope puts it under, a byte for the
// key and the raw value held, so a copy weighs about what its source does.
func keptWeight(kept ir.Unmodeled) int {
	prefix := annotation.EntryKey(pathItemScope, "")
	weight := 0
	for key, entry := range kept {
		weight = sum(weight, len(strings.TrimPrefix(key, prefix))+1+len(entry.Value))
	}
	return weight
}

// serversWeight returns the bytes of the IDs named, which an operation naming
// no servers of its own takes from its mount.
func serversWeight(named []ir.ServerID) int {
	weight := 0
	for _, id := range named {
		weight = sum(weight, len(id))
	}
	return weight
}

// sum is a+b for weights that are not negative, held at math.MaxInt.
func sum(a, b int) int {
	if a > math.MaxInt-b {
		return math.MaxInt
	}
	return a + b
}
