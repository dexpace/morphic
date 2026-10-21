// Package irfacts derives what an ir.Document implies and does not state: the
// subtypes of each model, the properties each type exposes, how each type is
// used, and which types can hold themselves (ir-design §4.9). An [Index] builds
// each index the first time a fact needs it, walking a relation instead while
// that costs less, and writes nothing back, so no pass orders the derivations
// and none goes stale after a transform.
//
// The relations it indexes are defined in ir: [Index.IsSubtype] and
// [Index.Exposes] answer what ir.IsSubtype and ir.ExposedProps answer, for many
// questions at once. It imports only ir.
package irfacts
