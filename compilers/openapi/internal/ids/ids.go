// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package ids

import (
	"encoding/json/jsontext"
	"strconv"
	"strings"

	"github.com/dexpace/morphic/compilers/compile"
	"github.com/dexpace/morphic/ir"
)

// Ptr joins tokens into an RFC 6901 JSON pointer, escaping each one. IDs are
// derived from the pointers it builds (docs/ir/identity.md); the grammar
// wrapped around a pointer to make an ID belongs to the framework.
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
// (docs/ir/unmodeled.md#keys).
//
// It exists for the scopes holding a segment the document chooses — a form
// part's name, a callback's — where an unescaped "/" makes one segment read as
// two. Two parts named "q" and "q/x-a" then wrote one key between them and the
// surviving entry followed declaration order, silently, which is what
// docs/ir/models.md#composition-beside-a-union forbids a minted node and
// docs/ir/unmodeled.md#keys promises a scoped key.
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
	// ComposedSpace holds what no schema or declaration in the source occupies:
	// the Models synthesized for distributed union variants
	// (docs/ir/models.md#composition-beside-a-union) and for a field redeclared
	// as several models, and the identity of a field whose first declaration
	// another model carries (docs/compilers/openapi.md#allof-redeclared-property).
	// A $ref naming the pointer must keep resolving to what is written there. A
	// variant's path ends in oneOf or anyOf and an index, and a field's in
	// properties and a name, so the two never meet. ForPointer yields only the
	// openapi and anon spaces, so resolveSchemaRef can never hand a composed ID
	// to a reference.
	ComposedSpace compile.Space = "composed"
)

// The spaces an operation group is addressed in. No OpenAPI construct is a
// group: a Tag Object is metadata about a name, and an operation may tag itself
// with a name no Tag Object declares. The grouping rule mints the group, so each
// rule takes a namespace of its own (docs/ir/identity.md), distinct from
// OpenAPISpace, which addresses source coordinates. The first two are named for
// the strategy that forms the group, the last two for the one group they hold.
const (
	// TagGroupSpace holds the groups formed by operation tag, keyed by tag name.
	TagGroupSpace compile.Space = "tags"
	// PathPrefixGroupSpace holds the groups formed by first path segment.
	PathPrefixGroupSpace compile.Space = "path-prefix"
	// DefaultGroupSpace names the one group holding untagged operations.
	DefaultGroupSpace compile.Space = "default"
	// WebhookGroupSpace names the one group holding webhook operations.
	WebhookGroupSpace compile.Space = "webhooks"
)

// DefaultServerSpace names the one server OpenAPI supplies, at "/", for a
// document that declares none. No position in the source writes it, so it is
// minted rather than found, in a space of its own (docs/ir/identity.md), apart
// from OpenAPISpace, where a declared server is addressed by its pointer.
const DefaultServerSpace compile.Space = "default"

// TagGroup returns the stable ID of the group of operations whose first tag is
// name. It is keyed by the name, not by a Tag Object's position, so declaring
// the tag, reordering the tag list or repeating a declaration leaves it alone.
func TagGroup(name string) ir.GroupID {
	return compile.GroupID(TagGroupSpace, string(Ptr(name)))
}

// PathPrefixGroup returns the stable ID of the group of operations whose path
// starts with segment. The root path has no segment, so its key is empty.
func PathPrefixGroup(segment string) ir.GroupID {
	return compile.GroupID(PathPrefixGroupSpace, string(Ptr(segment)))
}

// DefaultGroup returns the stable ID of the group holding untagged operations.
func DefaultGroup() ir.GroupID { return compile.GroupID(DefaultGroupSpace, "") }

// WebhookGroup returns the stable ID of the group holding webhook operations.
func WebhookGroup() ir.GroupID { return compile.GroupID(WebhookGroupSpace, "") }

// Namespaces returns the namespaces this compiler mints IDs in, by kind: what the
// document declares in IDSpaces. It names the constants the derivations below use,
// so a namespace cannot be minted in without being one of these, and the
// compiler's tests hold the list to the namespaces its corpus really uses. Each
// of others adds the spaces Document qualifies by it.
func Namespaces(others ...Document) compile.Namespaces {
	ns := compile.Namespaces{
		ir.IDKindType:       {OpenAPISpace, AnonSpace, ComposedSpace},
		ir.IDKindOp:         {OpenAPISpace},
		ir.IDKindProp:       {OpenAPISpace, ComposedSpace},
		ir.IDKindAuth:       {OpenAPISpace, compile.UnresolvedSpace},
		ir.IDKindService:    {OpenAPISpace},
		ir.IDKindGroup:      {TagGroupSpace, PathPrefixGroupSpace, DefaultGroupSpace, WebhookGroupSpace},
		ir.IDKindParam:      {OpenAPISpace},
		ir.IDKindEnumMember: {OpenAPISpace, AnonSpace},
		ir.IDKindServer:     {OpenAPISpace, DefaultServerSpace},
	}
	for _, d := range others {
		ns[ir.IDKindType] = append(ns[ir.IDKindType], d.space(OpenAPISpace), d.space(AnonSpace), d.space(ComposedSpace))
		ns[ir.IDKindProp] = append(ns[ir.IDKindProp], d.space(OpenAPISpace), d.space(ComposedSpace))
		ns[ir.IDKindEnumMember] = append(ns[ir.IDKindEnumMember], d.space(OpenAPISpace), d.space(AnonSpace))
		ns[ir.IDKindServer] = append(ns[ir.IDKindServer], d.space(OpenAPISpace))
	}
	return ns
}

// Document is a document of the compile as the IDs of what it declares name
// it. The zero value is the source, whose IDs take the bare spaces; another is
// named by its label (In), which qualifies each space (compile.Space.Qualified),
// so one pointer in two documents derives two IDs, each still the pointer it
// records. Operations, parameters, security schemes and services are named by
// where the source mounts or names them, so they take no Document; a server is
// named where it is declared, so it does.
type Document struct {
	label string
}

// In returns the document labelled label: its location, as the source's
// directory reaches it. The empty label is the source's.
func In(label string) Document { return Document{label: label} }

// space returns s as d names it: s itself for the source, else qualified by d.
func (d Document) space(s compile.Space) compile.Space {
	if d.label == "" {
		return s
	}
	return s.Qualified(d.label)
}

// Coordinate returns the key the type registry interns the schema at pointer
// in d under: the pointer itself in the source, and the pointer behind d's
// qualifier elsewhere, which no pointer spells, so one pointer in two
// documents is two coordinates.
func (d Document) Coordinate(pointer jsontext.Pointer) string {
	if d.label == "" {
		return string(pointer)
	}
	return string(compile.Space("").Qualified(d.label)) + string(pointer)
}

// NamedType returns the stable ID of a components-named schema at pointer in d.
func (d Document) NamedType(pointer jsontext.Pointer) ir.TypeID {
	return compile.TypeID(d.space(OpenAPISpace), string(pointer))
}

// AnonType returns the stable ID of a hoisted inline type at pointer in d.
func (d Document) AnonType(pointer jsontext.Pointer) ir.TypeID {
	return compile.TypeID(d.space(AnonSpace), string(pointer))
}

// ComposedType returns the stable ID of the Model synthesized at pointer in d:
// a distributed union variant's branch
// (docs/ir/models.md#composition-beside-a-union), or the declaration naming a
// field redeclared as several models.
func (d Document) ComposedType(pointer jsontext.Pointer) ir.TypeID {
	return compile.TypeID(d.space(ComposedSpace), string(pointer))
}

// ComposedProp returns the stable ID of a field the declaration at pointer in d
// names, where another model carries the field's first declaration, so that no
// declaration's own ID is taken.
func (d Document) ComposedProp(pointer jsontext.Pointer) ir.PropID {
	return compile.PropID(d.space(ComposedSpace), string(pointer))
}

// Op returns the stable ID of the operation at pointer.
func Op(pointer jsontext.Pointer) ir.OpID {
	return compile.OpID(OpenAPISpace, string(pointer))
}

// Prop returns the stable ID of the property at pointer in d.
func (d Document) Prop(pointer jsontext.Pointer) ir.PropID {
	return compile.PropID(d.space(OpenAPISpace), string(pointer))
}

// Param returns the stable ID of the occurrence-th parameter, counting from one,
// to hold name in location in on the operation mounted at mount.
//
// The mount is the pointer the operation lowers at, so a path item $ref'd from
// two paths gives each mount its own parameter IDs. The name is one escaped
// token and in is the source's location, so a name and a location cannot be
// confused, and reordering a list without repeats renames nothing. A repeat's
// number follows the location (numbered) and equals no other parameter's ID:
// that would take an operation mounted at another's parameters list.
func Param(mount jsontext.Pointer, name, in string, occurrence int) ir.ParamID {
	return compile.ParamID(OpenAPISpace, string(numbered(mount+Ptr("parameters", name, in), occurrence)))
}

// EnumMember returns the stable ID of the occurrence-th member, counting from
// one, of the enum at enumPointer in d to hold key.
//
// It lives in the namespace of the enum's own TypeID, which schemaSpace chooses
// for both, so the member sits under the type that declares it. The key is one
// escaped token, built by the caller from the member's value, never from its
// name or position. A repeat's number follows the key (numbered) and is never
// a key itself, since a key opens with a kind tag and a colon.
func (d Document) EnumMember(enumPointer jsontext.Pointer, key string, occurrence int) ir.EnumMemberID {
	return compile.EnumMemberID(d.space(schemaSpace(enumPointer)), string(numbered(enumPointer+Ptr(key), occurrence)))
}

// numbered returns the path of the occurrence-th holder of the identity at
// path: path itself for the first, and path with the occurrence appended as one
// more token for each repeat. A source that repeats nothing carries no number,
// and reordering it renames nothing.
func numbered(path jsontext.Pointer, occurrence int) jsontext.Pointer {
	if occurrence < 2 {
		return path
	}
	return path + Ptr(strconv.Itoa(occurrence))
}

// Server returns the stable ID of the server at pointer in d: the position of
// its Server Object in the list that declares it. A path item mounted twice
// declares its servers once, so both mounts name the same IDs.
func (d Document) Server(pointer jsontext.Pointer) ir.ServerID {
	return compile.ServerID(d.space(OpenAPISpace), string(pointer))
}

// DefaultServer returns the stable ID of the server OpenAPI supplies for a
// document that declares none.
func DefaultServer() ir.ServerID { return compile.ServerID(DefaultServerSpace, "") }

// Auth returns the stable ID of the named security scheme.
func Auth(name string) ir.AuthID {
	return compile.AuthID(OpenAPISpace, string(schemePointer(name)))
}

// UnresolvedAuth returns the stable ID of the scheme a name that reaches no
// mechanism lowers to: the pointer a declaration of it has, in a namespace of its
// own, so neither a reference nor an entry at that pointer reaches it.
func UnresolvedAuth(name string) ir.AuthID {
	return compile.AuthID(compile.UnresolvedSpace, string(schemePointer(name)))
}

// schemePointer returns where the security scheme called name is declared.
func schemePointer(name string) jsontext.Pointer {
	return Ptr("components", "securitySchemes", name)
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

// ForPointer returns the stable TypeID for a schema hoisted at pointer in d:
// the named-component ID for a top-level component schema, the anonymous
// (hoisted-inline) ID otherwise.
func (d Document) ForPointer(pointer jsontext.Pointer) ir.TypeID {
	if schemaSpace(pointer) == OpenAPISpace {
		return d.NamedType(pointer)
	}
	return d.AnonType(pointer)
}

// schemaSpace returns the namespace of the type a schema at pointer lowers to:
// OpenAPISpace for a top-level component schema, the one kind that declares a
// named type, and AnonSpace for every other position.
func schemaSpace(pointer jsontext.Pointer) compile.Space {
	if _, ok := ComponentSchemaName(pointer); ok {
		return OpenAPISpace
	}
	return AnonSpace
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
