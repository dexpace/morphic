// Package merge reconciles the properties more than one allOf branch declares:
// either folding the declarations into one or reporting that they disagree.
//
// It is the compiler's most intricate logic and the part least tied to the walk
// that drives it. The walk hands it each declaration through a PropIndex, and a
// Merger folds a field's declarations once the registry is whole, reading it
// through a function, so the conflict lattice can be exercised against a map
// and a recorder rather than a document.
package merge
