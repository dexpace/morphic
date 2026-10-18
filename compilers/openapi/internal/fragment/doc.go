// Package fragment is the one place the compiler reads a $ref fragment and
// spells one for the resolver (GitHub #911).
//
// A fragment is percent-decoded and nothing else (RFC 3986 section 3.5,
// RFC 6901 section 6), so '+' names itself. The resolver query-unescapes it,
// which reads '+' as a space. Every reader of a fragment goes through Pointer,
// and load hands the resolver ForResolver's spelling, so the two agree by
// construction.
package fragment
