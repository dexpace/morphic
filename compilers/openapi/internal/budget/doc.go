// Package budget counts the work and the size of a compile against their
// limits. A Limit says how far a count may go, or that nothing bounds it, and
// a Budget counts what is spent against one (GitHub #785).
//
// Every count the compile holds to a limit counts through it, so "unbounded"
// has one spelling, the zero Limit, and no sum passes math.MaxInt. A depth, a
// chain's hops or a constant compared beside the one loop it bounds needs
// neither and stays beside its check. What crossing a limit does stays with
// the site that counts.
package budget
