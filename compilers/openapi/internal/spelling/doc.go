// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

// Package spelling relates the primitive kinds OpenAPI spells.
//
// It holds the table that maps a (type, format) pair to a kind, the table of
// pairings whose kind is written in a text of its own, and the relation
// between two kinds that the first table and ir.PrimKind.StrictlyWithin give.
// The value-domain half lives in ir (docs/ir/primitives.md); the spelling half
// is this package's, a string format's kind refining string. A narrowing must
// hold of every value and every JSON instance, so float32, float64 and float
// take only the integers that fit them exactly; a containment holding only by
// rounding is an overlap.
package spelling
