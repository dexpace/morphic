package lowering

import "github.com/dexpace/morphic/compilers/openapi/internal/budget"

// Limits is the share of the compiler's resource budgets the lowering enforces:
// those measuring a construct the walk builds, not the source document the load
// phase measures first.
//
// It is separate from the public openapi.Limits, as load.Options is from
// openapi.Options: that type is a published contract, and most of it describes
// phases this one cannot see. The compiler projects one onto the other at
// entry, resolving each default and the public spelling of "unbounded" there
// (budget.FromOption), so the zero value bounds nothing.
type Limits struct {
	// MaxEnumMembers bounds the members of a single enum.
	MaxEnumMembers budget.Limit
}
