package openapi

import (
	"strconv"
	"strings"

	"github.com/dexpace/morphic/compilers/compile"
	"github.com/dexpace/morphic/ir"
)

// ptr joins segments into an RFC 6901 JSON pointer. IDs are derived from these
// pointers (ir-design §3.1) and no other code in this package constructs one;
// the grammar wrapped around a pointer to make an ID belongs to the framework.
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

// The namespaces this compiler addresses. The framework spells the grammar
// around them — the kind prefix and the separators (compile.TypeID and friends);
// what is chosen here is which namespace a node belongs in, and the pointer
// arithmetic that produces its path.
const (
	// openapiSpace and anonSpace both address source coordinates: a component
	// schema is named at its own pointer, an inline schema is anonymous at its
	// own pointer, and no pointer is both.
	openapiSpace compile.Space = "openapi"
	anonSpace    compile.Space = "anon"
	// composedSpace holds the Models synthesized for distributed union variants
	// (§4.3) — nodes no schema in the source occupies. The branch pointer denotes
	// the branch schema, so a $ref naming it must keep resolving to the branch,
	// and the variant must not be reachable by any pointer a $ref can spell.
	// typeIDForPointer yields only the openapi and anon spaces, so
	// resolveSchemaRef can never hand a composed ID to a reference; compile.Types
	// rejects the mistake of minting into a space that addresses coordinates.
	composedSpace compile.Space = "composed"
)

// namedTypeID returns the stable ID of a components-named schema at pointer.
func namedTypeID(pointer string) ir.TypeID { return compile.TypeID(openapiSpace, pointer) }

// anonTypeID returns the stable ID of a hoisted inline type at pointer.
func anonTypeID(pointer string) ir.TypeID { return compile.TypeID(anonSpace, pointer) }

// composedTypeID returns the stable ID of the Model synthesized for the
// distributed union variant at a branch pointer (§4.3).
func composedTypeID(branchPointer string) ir.TypeID {
	return compile.TypeID(composedSpace, branchPointer)
}

// opID returns the stable ID of the operation at pointer.
func opID(pointer string) ir.OpID { return compile.OpID(openapiSpace, pointer) }

// propID returns the stable ID of the property at pointer.
func propID(pointer string) ir.PropID { return compile.PropID(openapiSpace, pointer) }

// authIDFor returns the stable ID of the named security scheme.
func authIDFor(name string) ir.AuthID {
	return compile.AuthID(openapiSpace, ptr("components", "securitySchemes", name))
}

// serviceID returns the stable ID of the service for the given source index.
func serviceID(sourceIndex int) ir.ServiceID {
	return compile.ServiceID(openapiSpace, strconv.Itoa(sourceIndex))
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
