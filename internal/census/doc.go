// Package census counts which members of the IR's JSON surface compiled
// documents set, and which values of its closed enumerations occur, so an
// emitter's scope can be re-derived by a command instead of kept by hand.
//
// The universe of facts is derived from package ir, never listed. A report
// prints one fact per line, zeros included:
//
//	documents <n> truncated <n>
//	no-document <n>
//	member <docs> <instances> <Struct>.<json>
//	value <docs> <instances> <Type> <value> [undeclared]
package census
