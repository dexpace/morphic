package compile

import (
	"strings"

	"github.com/dexpace/morphic/ir"
)

// A Space is the namespace an ID's path is addressed in: the format's own name
// for a node some source coordinate denotes ("openapi"), or a space of its own
// for a node a lowering mints ("composed").
//
// It is a named type rather than a string so a call cannot transpose the space
// and the path — two same-typed string parameters being exactly the argument
// order this package cannot check for a caller.
type Space string

// PrimSpace holds the primitive leaves.
//
// It is the one space deliberately shared across formats: every compiler must
// reach t/prim/string for the same leaf, or two documents lowered from different
// formats disagree about the identity of the same type. Every other space is one
// format's, and a space that is shared by accident rather than on purpose is what
// this distinction exists to make visible.
const PrimSpace Space = "prim"

// The kind prefix that opens an ID. They are spelled once here because a
// consumer switching on a prefix — a diagnostic renderer, an IR diff — reads
// every compiler's IDs, so a compiler with its own vocabulary breaks it
// (GitHub #162).
const (
	typeKind    = "t"
	opKind      = "op"
	propKind    = "p"
	authKind    = "auth"
	serviceKind = "s"
)

// TypeID returns the ID of the type at path within space.
//
// The path is the compiler's own derivation and stays there: a JSON Pointer, a
// GraphQL structural path and a protobuf fully-qualified name are different
// things, and nothing here can compute them. What the framework fixes is the
// grammar around the path — the kind prefix, the space, and the separator
// between them.
func TypeID(space Space, path string) ir.TypeID { return ir.TypeID(idFor(typeKind, space, path)) }

// OpID returns the ID of the operation at path within space.
func OpID(space Space, path string) ir.OpID { return ir.OpID(idFor(opKind, space, path)) }

// PropID returns the ID of the property at path within space.
func PropID(space Space, path string) ir.PropID { return ir.PropID(idFor(propKind, space, path)) }

// AuthID returns the ID of the security scheme at path within space.
func AuthID(space Space, path string) ir.AuthID { return ir.AuthID(idFor(authKind, space, path)) }

// ServiceID returns the ID of the service at path within space.
func ServiceID(space Space, path string) ir.ServiceID {
	return ir.ServiceID(idFor(serviceKind, space, path))
}

// PrimTypeID returns the shared ID of the primitive of kind k.
func PrimTypeID(k ir.PrimKind) ir.TypeID { return TypeID(PrimSpace, string(k)) }

// idFor joins a kind prefix, a space and a path with single separators.
//
// The path's leading separator is supplied here rather than assumed, so a
// compiler whose paths carry one and a compiler whose paths do not derive the
// same ID, and neither can produce "t/openapicomponents/schemas/User". A space
// with no path is an ID in its own right — the space names one node — and gets no
// trailing separator.
func idFor(kind string, space Space, path string) string {
	trimmed := strings.TrimPrefix(path, "/")
	if trimmed == "" {
		return kind + "/" + string(space)
	}
	return kind + "/" + string(space) + "/" + trimmed
}

// spaceOf returns the space id is addressed in — the segment after the kind
// prefix — or "" when there is none. Only an ID that was not built here, or one
// built with an empty Space, has none.
func spaceOf(id ir.TypeID) Space {
	parts := strings.SplitN(string(id), "/", 3)
	if len(parts) < 2 {
		return ""
	}
	return Space(parts[1])
}
