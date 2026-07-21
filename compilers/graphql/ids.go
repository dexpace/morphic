package graphql

import (
	"strconv"
	"strings"

	"github.com/dexpace/morphic/ir"
)

// GraphQL has no JSON pointers, so IDs derive from a synthetic structural path
// built from the schema shape (ir-design §3.1). Type names are globally unique
// within a GraphQL schema, so a name-based path is stable and collision-free;
// list wrappers hoist under the position they appear in. No ID is ever derived
// from a display name that a rename could change — a GraphQL type name is its
// source identity, not a presentation choice.

// ptr joins segments into a slash path, escaping the RFC 6901 metacharacters so
// an exotic name can never forge a different path. IDs and provenance pointers
// derive from these paths; no other code constructs them.
func ptr(segments ...string) string {
	if len(segments) == 0 {
		return ""
	}
	var b strings.Builder
	for _, seg := range segments {
		b.WriteByte('/')
		b.WriteString(escapeSegment(seg))
	}
	return b.String()
}

// escapeSegment applies RFC 6901 escaping: ~ first, then /.
func escapeSegment(s string) string {
	s = strings.ReplaceAll(s, "~", "~0")
	return strings.ReplaceAll(s, "/", "~1")
}

// typePtr is the structural pointer of a named type definition.
func typePtr(name string) string { return ptr("types", name) }

// fieldPtr is the structural pointer of a field within a named type.
func fieldPtr(typeName, field string) string { return ptr("types", typeName, "fields", field) }

// opPtr is the structural pointer of a root-type field lowered to an operation,
// keyed by its operation kind ("query"|"mutation"|"subscription") and field.
func opPtr(kind, field string) string { return ptr(kind, field) }

// namedTypeID returns the stable ID of a named type at pointer.
func namedTypeID(pointer string) ir.TypeID { return ir.TypeID("t/graphql" + pointer) }

// anonTypeID returns the stable ID of a hoisted anonymous type (list wrapper) at
// pointer.
func anonTypeID(pointer string) ir.TypeID { return ir.TypeID("t/anon" + pointer) }

// primTypeID returns the interned ID of primitive kind k. Primitives are shared
// across formats under the same t/prim namespace the OpenAPI compiler uses.
func primTypeID(k ir.PrimKind) ir.TypeID { return ir.TypeID("t/prim/" + string(k)) }

// opID returns the stable ID of the operation at pointer.
func opID(pointer string) ir.OpID { return ir.OpID("op/graphql" + pointer) }

// propID returns the stable ID of the property at pointer.
func propID(pointer string) ir.PropID { return ir.PropID("p/graphql" + pointer) }

// serviceID returns the stable ID of the service for the given source index.
func serviceID(sourceIndex int) ir.ServiceID {
	return ir.ServiceID("s/graphql/" + strconv.Itoa(sourceIndex))
}
