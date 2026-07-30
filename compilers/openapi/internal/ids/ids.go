// Package ids derives IR identifiers from OpenAPI source coordinates: the RFC
// 6901 pointer arithmetic that names a position, and the namespace each kind of
// node is addressed in.
//
// The path is OpenAPI's and stays here — a JSON Pointer is not a GraphQL
// structural path or a protobuf fully-qualified name, and nothing above this
// package can compute one. The grammar wrapped around it belongs to the
// framework, which is the only thing this package reaches besides ir.
package ids

import (
	"strconv"
	"strings"

	"github.com/dexpace/morphic/compilers/compile"
	"github.com/dexpace/morphic/ir"
)

// Ptr joins segments into an RFC 6901 JSON pointer. IDs are derived from these
// pointers (ir-design §3.1) and no other code in this package constructs one;
// the grammar wrapped around a pointer to make an ID belongs to the framework.
func Ptr(segments ...string) string {
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

// UnescapeSegment reverses RFC 6901 escaping: ~1 to /, then ~0 to ~. It recovers
// a component's on-wire name from a pointer segment (e.g. a schema named "A/B"
// is escaped to "A~1B" in the pointer).
func UnescapeSegment(s string) string {
	s = strings.ReplaceAll(s, "~1", "/")
	return strings.ReplaceAll(s, "~0", "~")
}

// The namespaces this compiler addresses. The framework spells the grammar
// around them — the kind prefix and the separators (compile.TypeID and friends);
// what is chosen here is which namespace a node belongs in, and the pointer
// arithmetic that produces its path.
const (
	// OpenAPISpace and AnonSpace both address source coordinates: a component
	// schema is named at its own pointer, an inline schema is anonymous at its
	// own pointer, and no pointer is both.
	OpenAPISpace compile.Space = "openapi"
	AnonSpace    compile.Space = "anon"
	// ComposedSpace holds the Models synthesized for distributed union variants
	// (§4.3) — nodes no schema in the source occupies. The branch pointer denotes
	// the branch schema, so a $ref naming it must keep resolving to the branch,
	// and the variant must not be reachable by any pointer a $ref can spell.
	// ForPointer yields only the openapi and anon spaces, so
	// resolveSchemaRef can never hand a composed ID to a reference; compile.Types
	// rejects the mistake of minting into a space that addresses coordinates.
	ComposedSpace compile.Space = "composed"
)

// NamedType returns the stable ID of a components-named schema at pointer.
func NamedType(pointer string) ir.TypeID { return compile.TypeID(OpenAPISpace, pointer) }

// AnonType returns the stable ID of a hoisted inline type at pointer.
func AnonType(pointer string) ir.TypeID { return compile.TypeID(AnonSpace, pointer) }

// ComposedType returns the stable ID of the Model synthesized for the
// distributed union variant at a branch pointer (§4.3).
func ComposedType(branchPointer string) ir.TypeID {
	return compile.TypeID(ComposedSpace, branchPointer)
}

// Op returns the stable ID of the operation at pointer.
func Op(pointer string) ir.OpID { return compile.OpID(OpenAPISpace, pointer) }

// Prop returns the stable ID of the property at pointer.
func Prop(pointer string) ir.PropID { return compile.PropID(OpenAPISpace, pointer) }

// Auth returns the stable ID of the named security scheme.
func Auth(name string) ir.AuthID {
	return compile.AuthID(OpenAPISpace, Ptr("components", "securitySchemes", name))
}

// Service returns the stable ID of the service for the given source index.
func Service(sourceIndex int) ir.ServiceID {
	return compile.ServiceID(OpenAPISpace, strconv.Itoa(sourceIndex))
}

// DeclarationHint returns the name hint a node hoisted under pointer should
// carry: the component's own name when pointer addresses a top-level component
// entry, else fallback. A $ref'd component lowers once, at its declaration, so
// a use-site-derived hint (the referencing operation's ID, a response header's
// map key) would name the one shared node after whichever reference happened to
// lower first — arbitrary, and emitter-visible via Naming.Hint (issue #107).
func DeclarationHint(pointer, fallback string) string {
	name, ok := componentEntryName(pointer)
	if !ok {
		return fallback
	}
	return name
}

// ComponentEntry splits a pointer that addresses a top-level component entry
// (/components/<kind>/<name>, no deeper path) into its kind and unescaped name.
// It is the one place that shape is parsed: componentEntryName drops the kind,
// and ComponentSchemaName narrows it to the schemas kind, which is the only one
// that earns a named TypeID.
func ComponentEntry(pointer string) (kind, name string, ok bool) {
	const prefix = "/components/"
	if !strings.HasPrefix(pointer, prefix) {
		return "", "", false
	}
	kind, name, found := strings.Cut(pointer[len(prefix):], "/")
	if !found || kind == "" || name == "" || strings.Contains(name, "/") {
		return "", "", false
	}
	return kind, UnescapeSegment(name), true
}

// componentEntryName returns the unescaped name of a top-level component entry
// of any kind.
func componentEntryName(pointer string) (string, bool) {
	_, name, ok := ComponentEntry(pointer)
	return name, ok
}

// ForPointer returns the stable TypeID for a schema hoisted at pointer:
// the named-component ID for a top-level component schema, the anonymous
// (hoisted-inline) ID otherwise.
func ForPointer(pointer string) ir.TypeID {
	if _, ok := ComponentSchemaName(pointer); ok {
		return NamedType(pointer)
	}
	return AnonType(pointer)
}

// ComponentSchemaName reports whether pointer addresses a top-level component
// schema (/components/schemas/<name> with no deeper path) and returns its name.
// Only this kind of component declares a named type in OpenAPI, which is why it
// alone gates NamedType.
func ComponentSchemaName(pointer string) (string, bool) {
	kind, name, ok := ComponentEntry(pointer)
	return name, ok && kind == "schemas"
}
