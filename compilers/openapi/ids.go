package openapi

import (
	"path"
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

// componentEntryName reports whether pointer addresses a top-level component
// entry of any kind (/components/<kind>/<name>, no deeper path) and returns its
// unescaped name. It is the any-kind counterpart of componentSchemaName, which
// answers the narrower question of whether a pointer names a *schema* component
// and so earns a named TypeID.
func componentEntryName(pointer string) (string, bool) {
	const prefix = "/components/"
	if !strings.HasPrefix(pointer, prefix) {
		return "", false
	}
	kind, name, found := strings.Cut(pointer[len(prefix):], "/")
	if !found || kind == "" || name == "" || strings.Contains(name, "/") {
		return "", false
	}
	return unescapeSegment(name), true
}

// internalPointer returns the same-document JSON pointer a $ref (or discriminator
// mapping) target addresses, and ok=false for a genuine cross-document reference,
// a bare schema name, or a malformed ref. A document part naming this same source
// file (an OpenAPI self-reference) is treated as internal — Milestone 1 interns
// only same-file targets; genuinely external ones are diagnosed and dropped.
func (l *lowerer) internalPointer(ref string) (string, bool) {
	doc, pointer, found := strings.Cut(ref, "#")
	if !found || pointer == "" {
		return "", false
	}
	if doc != "" && !l.sameFile(doc) {
		return "", false
	}
	return pointer, true
}

// sameFile reports whether a $ref document part names this compilation's own
// source file, so the reference resolves back into the same document. An exact
// path match is internal; so is a bare filename (no directory) equal to our own
// basename, since self-references are conventionally spelled with just the
// file's own name (e.g. `m.yaml#/...` inside m.yaml). A doc part that carries
// its own directory is matched in full, never on basename alone — otherwise
// `dir2/m.yaml` referenced from `dir1/m.yaml` would misread as a self-reference.
func (l *lowerer) sameFile(doc string) bool {
	self := l.source.Path
	if self == "" {
		return false
	}
	if doc == self {
		return true
	}
	return !strings.Contains(doc, "/") && doc == path.Base(self)
}

// internedID returns the TypeID a node was interned under at pointer, when one
// already exists there — either a previously hoisted sub-schema (via byPointer)
// or a node registered directly under its pointer-derived ID.
func (l *lowerer) internedID(pointer string) (ir.TypeID, bool) {
	if id, ok := l.byPointer[pointer]; ok {
		return id, true
	}
	id := typeIDForPointer(pointer)
	if _, ok := l.out.Types[id]; ok {
		return id, true
	}
	return "", false
}

// resolveComponentRef resolves an internal pointer addressing a top-level
// component schema to its stable named ID, but only when that component is
// declared. It returns handled=true once the pointer is classified as a
// component pointer (declared or not), so callers can stop; a declared
// component yields ok=true, an undeclared one ok=false (a dangling reference to
// drop). The ID is rebuilt from the component's canonical name — unescaped, then
// re-escaped by ptr — rather than from the incoming pointer text, so a
// non-canonically escaped reference (e.g. `A~B` for a component named "A~B",
// interned under `A~0B`) still resolves to the interned node instead of an
// unbacked ID.
func (l *lowerer) resolveComponentRef(pointer string) (id ir.TypeID, ok, handled bool) {
	name, isComponent := componentSchemaName(pointer)
	if !isComponent {
		return "", false, false
	}
	if l.schemas[name] {
		return namedTypeID(ptr("components", "schemas", name)), true, true
	}
	return "", false, true
}
