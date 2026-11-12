// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

// Package merge reconciles the properties more than one allOf branch declares:
// folding the declarations into one, composing the models they name, or
// reporting that they disagree.
//
// It is the compiler's most intricate logic and the part least tied to the walk
// that drives it. The walk hands it each declaration through a PropIndex, which
// folds a field's declarations once the registry is whole, through a Merger:
// the registry and the walk's answers as values, so the conflict lattice can be
// exercised against a map and a stub walk rather than a document.
package merge
