// Package compile holds what every spec compiler shares: the type registry and
// its source-coordinate map (invariant 3), diagnostic accumulation, naming on
// ir's canonical grammar (invariant 4) with the name minted for an unnamed
// entity, and the ID grammar.
//
// Architecture tests hold the boundary: only this package and ir write an
// ir.TypeRegistry or derive a canonical name, and no compiler builds an ID from
// a string.
//
// Document assembly stays with each compiler, as do recursion bounds and the
// derivation of an ID's path, which differ by format. Promoting an item here is
// additive; demoting one breaks every compiler, so borderline items start
// outside.
package compile
