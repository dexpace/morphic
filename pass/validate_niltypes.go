package pass

import (
	"fmt"
	"maps"
	"slices"

	"github.com/dexpace/morphic/ir"
)

// checkNilTypes reports registry entries holding a nil type definition.
//
// Without it a malformed registry reads as internally consistent: the reference
// walk tolerates nil entries and the checks that range the registry skip them
// (liveTypeIDs), so Validate would report nothing for a document no emitter can
// consume.
func checkNilTypes(doc *ir.Document) []ir.Diagnostic {
	var diags []ir.Diagnostic
	for _, id := range slices.Sorted(maps.Keys(doc.Types)) {
		if !ir.IsNilTypeDef(doc.Types[id]) {
			continue
		}
		diags = append(diags, diag(ir.SeverityError, "ir/nil-type",
			fmt.Sprintf("types registry entry %q holds a nil type definition", id),
			"types["+string(id)+"]"))
	}
	return diags
}

// liveTypeIDs returns the registry's type IDs in sorted order, omitting entries
// that hold a nil type definition.
//
// Every check that iterates doc.Types dereferences the value it matched, and a
// typed nil satisfies both a type-switch case and a comma-ok assertion — so the
// match itself is no evidence the value is safe to read. Screening at each call
// site instead would leave the next check the pass gains to rediscover the
// crash; checkNilTypes reports whatever this omits.
func liveTypeIDs(doc *ir.Document) []ir.TypeID {
	ids := slices.Sorted(maps.Keys(doc.Types))
	live := make([]ir.TypeID, 0, len(ids))
	for _, id := range ids {
		if !ir.IsNilTypeDef(doc.Types[id]) {
			live = append(live, id)
		}
	}
	return live
}
