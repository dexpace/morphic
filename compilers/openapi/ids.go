package openapi

import (
	"strconv"
	"strings"

	"github.com/dexpace/morphic/ir"
)

// ptr joins segments into an RFC 6901 JSON pointer. IDs are derived from these
// pointers (ir-design §3.1); no other code may construct IDs or pointers.
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

// unescapeSegment reverses RFC 6901 escaping: ~1 to /, then ~0 to ~. It recovers
// a component's on-wire name from a pointer segment (e.g. a schema named "A/B"
// is escaped to "A~1B" in the pointer).
func unescapeSegment(s string) string {
	s = strings.ReplaceAll(s, "~1", "/")
	return strings.ReplaceAll(s, "~0", "~")
}

// namedTypeID returns the stable ID of a components-named schema at pointer.
func namedTypeID(pointer string) ir.TypeID { return ir.TypeID("t/openapi" + pointer) }

// anonTypeID returns the stable ID of a hoisted inline type at pointer.
func anonTypeID(pointer string) ir.TypeID { return ir.TypeID("t/anon" + pointer) }

// primTypeID returns the interned ID of primitive kind k.
func primTypeID(k ir.PrimKind) ir.TypeID { return ir.TypeID("t/prim/" + string(k)) }

// opID returns the stable ID of the operation at pointer.
func opID(pointer string) ir.OpID { return ir.OpID("op/openapi" + pointer) }

// propID returns the stable ID of the property at pointer.
func propID(pointer string) ir.PropID { return ir.PropID("p/openapi" + pointer) }

// authIDFor returns the stable ID of the named security scheme.
func authIDFor(name string) ir.AuthID {
	return ir.AuthID("auth/openapi" + ptr("components", "securitySchemes", name))
}

// serviceID returns the stable ID of the service for the given source index.
func serviceID(sourceIndex int) ir.ServiceID {
	return ir.ServiceID("s/openapi/" + strconv.Itoa(sourceIndex))
}

// declarationHint returns the name hint a node hoisted under pointer should
// carry: the component's own name when pointer addresses a top-level component
// entry, else fallback. A $ref'd component lowers once, at its declaration, so
// a use-site-derived hint (the referencing operation's ID, a response header's
// map key) would name the one shared node after whichever reference happened to
// lower first — arbitrary, and emitter-visible via Naming.Hint (issue #107).
func declarationHint(pointer, fallback string) string {
	name, ok := componentEntryName(pointer)
	if !ok {
		return fallback
	}
	return name
}

// componentEntry splits a pointer that addresses a top-level component entry
// (/components/<kind>/<name>, no deeper path) into its kind and unescaped name.
// It is the one place that shape is parsed: componentEntryName drops the kind,
// and componentSchemaName narrows it to the schemas kind, which is the only one
// that earns a named TypeID.
func componentEntry(pointer string) (kind, name string, ok bool) {
	const prefix = "/components/"
	if !strings.HasPrefix(pointer, prefix) {
		return "", "", false
	}
	kind, name, found := strings.Cut(pointer[len(prefix):], "/")
	if !found || kind == "" || name == "" || strings.Contains(name, "/") {
		return "", "", false
	}
	return kind, unescapeSegment(name), true
}

// componentEntryName returns the unescaped name of a top-level component entry
// of any kind.
func componentEntryName(pointer string) (string, bool) {
	_, name, ok := componentEntry(pointer)
	return name, ok
}
