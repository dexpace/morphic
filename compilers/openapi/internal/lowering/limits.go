package lowering

// Limits is the share of the compiler's resource budgets the lowering enforces:
// the ones measuring a construct the walk builds, rather than the source
// document the load phase measures before any of it is built.
//
// It is a separate type from the compiler's public openapi.Limits for the reason
// load.Options is separate from openapi.Options — that type's shape is a
// published contract, and most of it describes phases this one cannot see. The
// compiler projects one onto the other at entry.
//
// Zero is unbounded in every field, which is the opposite of the public type's
// spelling and deliberate: the projection resolves defaults and translates the
// public spelling of "unbounded" before anything reaches here, so a budget still
// zero at this point is one no caller set.
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
