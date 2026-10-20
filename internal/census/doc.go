// Package census counts which facts of the IR compiled documents set: each
// member of its structs, the emptiness of a member whose empty value differs
// from its absence, and each value of its enumerations, a sealed sum's tags
// among them. The facts are read from the JSON shape the IR declares, never
// listed by hand. A report prints the runs, then one fact per line, zeros
// included:
//
//	documents <n>
//	no-document <n>
//	truncated <n>
//	member <docs> <instances> <Struct>.<json>
//	empty <docs> <instances> <Struct>.<json>
//	value <docs> <instances> <Type> <value> [undeclared]
package census
