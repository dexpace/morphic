// Package ids builds the RFC 6901 pointer that names a position — a
// jsontext.Pointer from construction on, read back through its methods, never
// split on '/' — and derives IR identifiers and namespaces from it.
//
// The path is OpenAPI's and stays here — a JSON Pointer is not a GraphQL
// structural path or a protobuf fully-qualified name, and nothing above this
// package can compute one. The grammar wrapped around it belongs to the
// framework, which is the only thing this package reaches besides ir.
package ids

import (
	"encoding/json/jsontext"
	"strconv"
	"strings"

	"github.com/dexpace/morphic/compilers/compile"
	"github.com/dexpace/morphic/ir"
)

// Ptr joins tokens into an RFC 6901 JSON pointer, escaping each one. IDs are
// derived from the pointers it builds (ir-design §3.1); the grammar wrapped
// around a pointer to make an ID belongs to the framework.
//
// A position beneath another is base + Ptr(tokens...): joining two valid
// pointers yields a valid pointer, so extending one never takes it apart.
//
// AppendToken rewrites a byte that is not UTF-8 to U+FFFD, which would give two
// keys differing only there one pointer. No token read from a document can carry
// one: a source that is not UTF-8 is refused before anything lowers.
func Ptr(tokens ...string) jsontext.Pointer {
	var p jsontext.Pointer
	for _, tok := range tokens {
		p = p.AppendToken(tok)
	}
	return p
}

// Scope joins segments into an Unmodeled key scope: Ptr's escaping without the
// leading separator, since a scope is a relative path rather than a pointer
// (ir-design §12).
//
// It exists for the scopes holding a segment the document chooses — a form
// part's name, a callback's — where an unescaped "/" makes one segment read as
// two. Two parts named "q" and "q/x-a" then wrote one key between them and the
// surviving entry followed declaration order, silently, which is what §4.3
// forbids a minted node and §12 promises a scoped key.
func Scope(segments ...string) string {
	return strings.TrimPrefix(string(Ptr(segments...)), "/")
}

// The namespaces this compiler addresses. The framework spells the grammar
// around them — the kind prefix and the separators (compile.TypeID and friends);
// what is chosen here is which namespace a node belongs in, and the pointer that
// is its path.
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
func NamedType(pointer jsontext.Pointer) ir.TypeID {
	return compile.TypeID(OpenAPISpace, string(pointer))
}

// AnonType returns the stable ID of a hoisted inline type at pointer.
func AnonType(pointer jsontext.Pointer) ir.TypeID {
	return compile.TypeID(AnonSpace, string(pointer))
}

// ComposedType returns the stable ID of the Model synthesized for the
// distributed union variant at a branch pointer (§4.3).
func ComposedType(branchPointer jsontext.Pointer) ir.TypeID {
	return compile.TypeID(ComposedSpace, string(branchPointer))
}

// Op returns the stable ID of the operation at pointer.
func Op(pointer jsontext.Pointer) ir.OpID {
	return compile.OpID(OpenAPISpace, string(pointer))
}

// Prop returns the stable ID of the property at pointer.
func Prop(pointer jsontext.Pointer) ir.PropID {
	return compile.PropID(OpenAPISpace, string(pointer))
}

// Auth returns the stable ID of the named security scheme.
func Auth(name string) ir.AuthID {
	return compile.AuthID(OpenAPISpace, string(Ptr("components", "securitySchemes", name)))
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
func DeclarationHint(pointer jsontext.Pointer, fallback string) string {
	name, ok := componentEntryName(pointer)
	if !ok {
		return fallback
	}
	return name
}

// ComponentEntry returns the kind and name of the top-level component entry
// pointer addresses (/components/<kind>/<name>, no deeper path), both decoded,
// and ok=false for any other pointer or for an entry keyed "".
// componentEntryName drops the kind, and ComponentSchemaName narrows it to the
// schemas kind, which is the only one that earns a named TypeID.
func ComponentEntry(pointer jsontext.Pointer) (kind, name string, ok bool) {
	kind, name, ok = componentEntrySplit(pointer)
	if !ok || name == "" {
		return "", "", false
	}
	return kind, name, true
}

// componentsRoot is the pointer every component entry sits two tokens beneath.
const componentsRoot jsontext.Pointer = "/components"

// componentEntrySplit is the one place the /components/<kind>/<name> shape is
// parsed: it reads the kind and name off pointer's last two tokens, decoded, when
// the pointer sits two tokens beneath /components.
//
// It does not judge the name, so a caller that must tell "not that shape at all"
// from "that shape with an empty name" can. ComponentEntry folds the two together
// on purpose — an entry keyed "" earns no named TypeID either way — but the two
// are different facts about a document, and a diagnostic naming the wrong one is
// simply false.
func componentEntrySplit(pointer jsontext.Pointer) (kind, name string, ok bool) {
	section := pointer.Parent()
	if section.Parent() != componentsRoot {
		return "", "", false
	}
	kind = section.LastToken()
	if kind == "" {
		return "", "", false
	}
	return kind, pointer.LastToken(), true
}

// ComponentSchemaNamedEmpty reports whether pointer addresses the top-level
// component schema keyed "" — the position /components/schemas/ addresses.
//
// It exists because that schema is a component schema that ComponentSchemaName
// still refuses: an empty name earns no named TypeID, so the schema hoists
// anonymously (testdata/conformance/openapi/empty-names.yaml records that
// policy). A caller that reports the refusal needs the distinction to word it
// truthfully, since a reader who follows the pointer finds a component schema
// sitting exactly where a "not a component schema" message denies one is.
func ComponentSchemaNamedEmpty(pointer jsontext.Pointer) bool {
	kind, name, ok := componentEntrySplit(pointer)
	return ok && kind == "schemas" && name == ""
}

// componentEntryName returns the unescaped name of a top-level component entry
// of any kind.
func componentEntryName(pointer jsontext.Pointer) (string, bool) {
	_, name, ok := ComponentEntry(pointer)
	return name, ok
}

// ForPointer returns the stable TypeID for a schema hoisted at pointer:
// the named-component ID for a top-level component schema, the anonymous
// (hoisted-inline) ID otherwise.
func ForPointer(pointer jsontext.Pointer) ir.TypeID {
	if _, ok := ComponentSchemaName(pointer); ok {
		return NamedType(pointer)
	}
	return AnonType(pointer)
}

// ComponentSchemaName reports whether pointer addresses a top-level component
// schema (/components/schemas/<name> with no deeper path) and returns its name.
// Only this kind of component declares a named type in OpenAPI, which is why it
// alone gates NamedType.
//
// It answers false for two unlike documents: one that declares no such entry,
// and one that declares it keyed "". Reporting the refusal as "not a component
// schema" is false for the second, since the pointer addresses exactly that —
// ComponentSchemaNamedEmpty separates them for a caller that has to say why.
func ComponentSchemaName(pointer jsontext.Pointer) (string, bool) {
	kind, name, ok := ComponentEntry(pointer)
	return name, ok && kind == "schemas"
}
