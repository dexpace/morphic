// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

// Package spelling relates the primitive kinds OpenAPI spells.
//
// It holds the table that maps a (type, format) pair to a kind, and the
// relation between two kinds that the table and ir.PrimKind.StrictlyWithin
// give. The value-domain half lives in ir (ir-design §4.1). This package adds
// the spelling half: a string format's kind refines string, derived from the
// table's string rows. A narrowing must hold of every value and every JSON
// instance, so float32, float64 and float take only the integers that fit them
// exactly; a containment that holds only by rounding is an overlap.
package spelling
