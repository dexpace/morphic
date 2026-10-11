package lowering

// Limits is the share of the compiler's resource budgets the lowering enforces:
// those measuring a construct the walk builds, not the source document the load
// phase measures first.
//
// It is separate from the public openapi.Limits, as load.Options is from
// openapi.Options: that type is a published contract, and most of it describes
// phases this one cannot see. The compiler projects one onto the other at
// entry.
//
// Zero is unbounded in every field, the opposite of the public spelling. The
// projection resolves defaults and translates "unbounded" first, so a zero here
// is a budget no caller set.
type Limits struct {
	// MaxEnumMembers bounds the members of a single enum.
	MaxEnumMembers int
}

// EnumMembersExceeded reports whether an enum declaring n members is past the
// budget. It is a predicate rather than a read of the field so that "zero is
// unbounded" is decided here once, and not restated by each site that asks.
func (l Limits) EnumMembersExceeded(n int) bool {
	return l.MaxEnumMembers > 0 && n > l.MaxEnumMembers
}
