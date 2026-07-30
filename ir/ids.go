package ir

import "strings"

// IDs are opaque to consumers but constructed deterministically by compilers
// from the source pointer of the defining occurrence (ir-design §3.1). They are
// never derived from display names and never rewritten by renames.

// TypeID identifies a TypeDef in Document.Types,
// e.g. "t/openapi/components/schemas/User".
type TypeID string

// OpID identifies an Operation, same construction as TypeID.
type OpID string

// ServiceID identifies a Service.
type ServiceID string

// ChannelID identifies a Channel in Document.Channels.
type ChannelID string

// MessageID identifies a Message in Document.Messages.
type MessageID string

// AuthID identifies an AuthScheme in Document.Auth.
type AuthID string

// PropID identifies a Property within the document.
type PropID string

// The kind prefix that opens every synthetic ID. An ID is
// <kind>/<space>[/<path>]: the kind says what sort of entity it names, the space
// says whose coordinates the path is in, and the path is the compiler's own
// derivation from the defining occurrence.
//
// The vocabulary lives here rather than in any one compiler because every
// consumer of a Document reads it — a diagnostic renderer, an IR diff, the
// structural verifier — so a compiler with its own spelling breaks all of them.
// compilers/compile owns the grammar that assembles an ID from these; what an ID
// looks like is fixed here, which is what lets irverify hold every compiler to it
// rather than only the one under test (GitHub #141).
//
// Channels and messages have no prefix yet: no compiler mints one, and inventing
// a spelling before a compiler needs it would fix the wrong thing.
const (
	IDKindType    = "t"
	IDKindOp      = "op"
	IDKindProp    = "p"
	IDKindAuth    = "auth"
	IDKindService = "s"
)

// IDSeparator separates an ID's kind, space and path segments.
const IDSeparator = "/"

// WellFormedID reports whether id has the shape kind requires: the kind prefix,
// a non-empty space, and an optional path, with no empty segment before the
// path. A space with no path is an ID in its own right — the space names one
// node — which is why the path is optional.
//
// Shape alone cannot catch every malformed ID. One that lost the separator
// between its space and its path ("t/anonaddr") reads as a space named
// "anonaddr" and is indistinguishable from a legitimate one here; what catches
// that is the path agreeing with the provenance pointer it was derived from,
// which irverify checks alongside this.
func WellFormedID(kind, id string) bool {
	rest, ok := strings.CutPrefix(id, kind+IDSeparator)
	if !ok || rest == "" {
		return false
	}
	space, path, hasPath := strings.Cut(rest, IDSeparator)
	if space == "" {
		return false
	}
	return !hasPath || path != ""
}

// IDPath returns the path segment of a well-formed id — everything after the
// kind and the space — and whether id carries one at all.
func IDPath(kind, id string) (string, bool) {
	if !WellFormedID(kind, id) {
		return "", false
	}
	rest := strings.TrimPrefix(id, kind+IDSeparator)
	_, path, hasPath := strings.Cut(rest, IDSeparator)
	return path, hasPath
}
