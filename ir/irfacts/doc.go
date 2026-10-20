// Package irfacts derives what an ir.Document implies and does not state: the
// subtypes of each model, how each type is used across the API surface, and
// which types can hold themselves (ir-design §4.9). An [Index] builds each fact
// the first time it is asked for and keeps it, and nothing is written back, so
// no pass orders the derivations and none goes stale after a transform.
//
// The relations it indexes are defined in ir: [Index.IsSubtype] answers what
// ir.IsSubtype answers, for many questions at once. It imports only ir.
package irfacts
