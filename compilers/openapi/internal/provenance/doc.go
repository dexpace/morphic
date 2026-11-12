// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

// Package provenance places a finding or a lowered position in the input
// document that supplied it: a pointer through Doc, and a node of a decoded
// tree through Tree, each a value the package placing asks of directly.
//
// It is a leaf of the compiler's import graph, reaching ir and the YAML node
// alone, so the overlay that records an attribution, the scans and checks that
// place a raw node, and the lowerings that place a pointer all reach the one
// answer without reaching each other.
package provenance
