// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

// Package fragment is the one place the compiler reads a $ref fragment as a
// JSON pointer (GitHub #911), and tells an absolute reference, which no base
// moves, from a relative one.
//
// A fragment is percent-decoded and nothing else (RFC 3986 section 3.5,
// RFC 6901 section 6), so '+' names itself. The parser library's reading
// query-unescapes it, which reads '+' as a space, so Pointer asks the library
// of ForResolver's spelling, and every reader of a fragment as a pointer goes
// through Pointer.
package fragment
