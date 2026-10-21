package compile

import (
	"context"
	"maps"
	"slices"

	"github.com/dexpace/morphic/ir"
)

// Promise records that a node is expected at coordinate: a reference names it
// and leaves building it to the lowering that owns it, so the node is built in
// its owner's context whichever lowering reached it first. build builds it
// where no lowering has once every one has returned (Fulfil).
//
// A coordinate promised again keeps its first build: a build lowers the
// coordinate in the context its own position gives it, not its referrer's, so
// any of them builds the same node. A nil build, or one promised once Fulfil
// has run, could never be taken, so both are refused.
func (t *Types) Promise(coordinate string, build func() []ir.Diagnostic) {
	switch {
	case t.fulfilled:
		t.refuse("promise rejected: the registry has already fulfilled its promises (at %q)", coordinate)
		return
	case coordinate == "" || build == nil:
		t.refuse("promise rejected: coordinate=%q build==nil=%v", coordinate, build == nil)
		return
	case t.promised[coordinate]:
		return
	}
	t.promised[coordinate] = true
	t.promises[coordinate] = build
}

// Fulfil builds each promised node no lowering built, in coordinate order, and
// returns what the builds reported. A compiler calls it after its last lowering
// returns and before Settle. A build may promise more, and those are built in a
// later round, so which node a build reaches first never decides another's
// context; each coordinate is queued once, which bounds the rounds by the
// coordinates promised.
//
// Like Settle, it checks ctx before each build and stops once it is done. A
// build leaving no node at its coordinate is refused: every reference holding
// that coordinate's ID would dangle.
func (t *Types) Fulfil(ctx context.Context) []ir.Diagnostic {
	defer func() { t.fulfilled = true }()
	var diags []ir.Diagnostic
	for len(t.promises) > 0 {
		round := t.promises
		t.promises = map[string]func() []ir.Diagnostic{}
		for _, coordinate := range slices.Sorted(maps.Keys(round)) {
			if ctx.Err() != nil {
				return diags
			}
			if _, built := t.byPointer[coordinate]; built {
				continue
			}
			diags = append(diags, round[coordinate]()...)
			if _, built := t.byPointer[coordinate]; !built {
				t.refuse("promise for %q built no node; the references naming it dangle", coordinate)
			}
		}
	}
	return diags
}
