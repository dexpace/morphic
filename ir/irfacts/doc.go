// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

// Package irfacts derives what an ir.Document implies and does not state:
// subtypes, exposed properties, usage, recursion and each entity's name across
// revisions (docs/ir/derived-facts.md). An [Index] builds each index the first
// time a fact needs it, walking a relation instead while that costs less, and
// writes nothing back, so no pass orders the derivations and none goes stale
// after a transform.
//
// The relations it indexes are defined in ir: [Index.IsSubtype] and
// [Index.Exposes] answer what ir.IsSubtype and ir.ExposedProps answer, for many
// questions at once. It imports only ir.
package irfacts
