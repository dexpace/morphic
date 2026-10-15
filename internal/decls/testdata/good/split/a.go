// Package split declares one type's constants across four files, in an order
// that is neither alphabetical by name nor the order the loader finishes
// parsing the files in.
package split

// Level is a string enum.
type Level string

// Zulu and Alpha are declared in this order, so the first sorts after the second.
const (
	Zulu  Level = "zulu"
	Alpha Level = "alpha"
)
