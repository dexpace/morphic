package operation

import (
	"encoding/json/jsontext"

	"github.com/dexpace/morphic/compilers/openapi/internal/diag"
	"github.com/dexpace/morphic/compilers/openapi/internal/ids"
	"github.com/dexpace/morphic/compilers/openapi/internal/lowering"
	"github.com/dexpace/morphic/ir"
)

// paramSlot is what OpenAPI identifies a parameter by: its name and location.
// It holds exactly what ids.Param mints from, so parameters in two slots never
// share an ID. Names compare as written, header names included, as the merge's
// paramKey compares them (#635).
type paramSlot struct {
	name, in string
}

// paramLedger counts how often each slot has been declared on one operation, so
// a repeat is numbered. The count follows the merged list, in order, and so
// never the iteration order of a map.
//
// A repeat in a list the source writes is load's to report, once, where it is
// written (parameterRepeats). One in another document's content, which that
// walk does not reach, is reported here, at the entry it was lowered from.
type paramLedger struct {
	mount   jsontext.Pointer
	seen    map[paramSlot]int
	firstAt map[paramSlot]jsontext.Pointer
}

// newParamLedger returns a ledger for the operation mounted at mount.
func newParamLedger(mount jsontext.Pointer) *paramLedger {
	return &paramLedger{mount: mount, seen: map[paramSlot]int{}, firstAt: map[paramSlot]jsontext.Pointer{}}
}

// next returns the ID of the parameter holding name in in, written by the list
// entry at entry, and the error a repeat raises there when c lowers another
// document's content. The first holder has the bare ID and no error.
func (l *paramLedger) next(c lowering.Ctx, name, in string, entry jsontext.Pointer) (ir.ParamID, []ir.Diagnostic) {
	slot := paramSlot{name: name, in: in}
	l.seen[slot]++
	occurrence := l.seen[slot]
	id := ids.Param(l.mount, name, in, occurrence)
	if occurrence == 1 {
		l.firstAt[slot] = entry
		return id, nil
	}
	if !c.RefScope().Foreign {
		return id, nil
	}
	return id, []ir.Diagnostic{c.DiagAt(ir.SeverityError, diag.DuplicateParameter, entry,
		"parameter %q in %q repeats the entry at %s", name, in, l.firstAt[slot])}
}
