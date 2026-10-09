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

// ParamID identifies a Parameter within the document. A parameter is scoped to
// the operation (or property, for field arguments) that carries it, so the ID
// names that one carrying and no other.
type ParamID string

// GroupID identifies an OperationGroup. A group is addressed by the name its
// source declares it under, so the ID names that one group and no other.
type GroupID string

// EnumMemberID identifies an EnumMember within the Enum that declares it. It is
// derived from the member's value rather than its position, so reordering or
// inserting members leaves every existing member's ID unchanged.
type EnumMemberID string

// The kind prefix that opens every synthetic ID. An ID is
// <kind>/<space>[/<path>]: the kind says what sort of entity it names, the space
// says whose coordinates the path is in, and the path is the compiler's own
// derivation from the defining occurrence.
//
// The vocabulary lives here because every consumer of a Document reads it, so a
// compiler with its own spelling breaks all of them. compilers/compile owns the
// grammar that assembles an ID from these; irverify holds every compiler to the
// shape fixed here (GitHub #141). Channels and messages have no prefix yet: no
// compiler mints one.
const (
	IDKindType    = "t"
	IDKindOp      = "op"
	IDKindProp    = "p"
	IDKindAuth    = "auth"
	IDKindService = "s"
	IDKindParam   = "param"
	IDKindGroup   = "g"
	IDKindMember  = "e"
)

// IDSpaceSynth is the space of a group the compiler synthesized because no
// declaration names it. A synthesized group's path is <format>/<rule>[/<key>],
// so it can never equal a declared group's ID, which lives in the format's own
// space (invariant 3, corollary).
const IDSpaceSynth = "synth"

// The rules a compiler synthesizes a group by. SynthRulePathPrefix is the only
// one that carries a key, the escaped path segment the group collects.
const (
	SynthRuleDefault    = "default"
	SynthRuleWebhooks   = "webhooks"
	SynthRulePathPrefix = "path-prefix"
)

// IDTagsSegment is the first path segment of a group declared by a tag: the
// path is tags/<escaped name>.
const IDTagsSegment = "tags"

// EscapeIDSegment returns name as one ID path segment. "~" becomes "~0" and "/"
// becomes "~1", so the result holds no separator, and the empty name becomes "~",
// which no escaped name can equal because every "~" in one is followed by 0 or 1.
// Distinct names therefore yield distinct segments.
func EscapeIDSegment(name string) string {
	if name == "" {
		return "~"
	}
	return strings.NewReplacer("~", "~0", "/", "~1").Replace(name)
}

// UnescapeIDSegment inverts EscapeIDSegment and reports false for a segment it
// could not have produced: an empty one, or a "~" not followed by 0 or 1.
func UnescapeIDSegment(seg string) (string, bool) {
	if seg == "" {
		return "", false
	}
	if seg == "~" {
		return "", true
	}
	var b strings.Builder
	for i := 0; i < len(seg); i++ {
		if seg[i] == '/' {
			return "", false
		}
		if seg[i] != '~' {
			b.WriteByte(seg[i])
			continue
		}
		i++
		switch {
		case i < len(seg) && seg[i] == '0':
			b.WriteByte('~')
		case i < len(seg) && seg[i] == '1':
			b.WriteByte('/')
		default:
			return "", false
		}
	}
	return b.String(), true
}

// IDSeparator separates an ID's kind, space and path segments.
const IDSeparator = "/"

// IDSpacePrim is the space the primitive leaves are addressed in.
//
// It is the one space that is not some format's own: every compiler must reach
// the same node for the same PrimKind, or two documents lowered from different
// formats disagree about the identity of the same type. A space shared by
// accident rather than on purpose is what naming it here makes visible.
const IDSpacePrim = "prim"

// PrimTypeID returns the shared TypeID of the primitive of kind k.
//
// It is the one ID this package can derive. Every other path is the compiler's
// own — a JSON Pointer, a GraphQL structural path and a protobuf
// fully-qualified name are different things and nothing here can compute one —
// so compilers/compile owns those. A primitive has no source position to derive
// from: its identity is its kind, which is an ir type, so the derivation belongs
// beside it (GitHub #73).
//
// That placement is what lets irverify hold every Document to this ID rather
// than only the ones this repository's compilers produce.
func PrimTypeID(k PrimKind) TypeID {
	return TypeID(IDKindType + IDSeparator + IDSpacePrim + IDSeparator + string(k))
}

// WellFormedID reports whether id has the shape kind requires: the kind prefix,
// a non-empty space, and an optional path, with no empty segment before the
// path. A space alone names one node and is an ID in its own right, so the path
// is optional.
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

// IDSpace returns the space segment of a well-formed id — the segment between
// the kind and the path — and whether id is well-formed at all. An ID that is
// not yields no space rather than a guess at one.
func IDSpace(kind, id string) (string, bool) {
	rest, ok := idRest(kind, id)
	if !ok {
		return "", false
	}
	space, _, _ := strings.Cut(rest, IDSeparator)
	return space, true
}

// IDPath returns the path segment of a well-formed id — everything after the
// kind and the space — and whether id carries one at all.
func IDPath(kind, id string) (string, bool) {
	rest, ok := idRest(kind, id)
	if !ok {
		return "", false
	}
	_, path, hasPath := strings.Cut(rest, IDSeparator)
	return path, hasPath
}

// idRest returns everything after a well-formed id's kind prefix.
func idRest(kind, id string) (string, bool) {
	if !WellFormedID(kind, id) {
		return "", false
	}
	return strings.TrimPrefix(id, kind+IDSeparator), true
}
