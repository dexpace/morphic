// Package shared sits outside every scope the rules hold, and declares the
// variables the pass plants write from another package.
package shared

// Count and Table are written by a plant in another package.
var (
	Count int
	Table = map[string]int{}
)
