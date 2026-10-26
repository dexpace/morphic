// Package budget is how the compiler bounds the work and the size of a
// compile. A Limit says how far a count may go, or that nothing bounds it, and
// a Budget counts what is spent against one (GitHub #785).
//
// Every bound the compile keeps counts through it, so "unbounded" has one
// spelling, the zero Limit, and no count overflows: each sum is held at
// math.MaxInt. What crossing a limit does stays with the site that counts,
// since a refusal, a warning and a silent stop are each some site's answer.
package budget
