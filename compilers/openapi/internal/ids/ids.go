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
	var pointer jsontext.Pointer
	for _, tok := range tokens {
		pointer = pointer.AppendToken(tok)
	}
	return pointer
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
	// OpenAPISpace and AnonymousSpace both address source coordinates: a
	// component schema is named at its own pointer, an inline schema is
	// anonymous at its own pointer, and no pointer is both.
	OpenAPISpace   compile.Space = "openapi"
	AnonymousSpace compile.Space = "anonymous"
	// ComposedSpace holds what no schema or declaration in the source occupies:
	// distributed union variants and the Union a routed subtype composes
	// (docs/ir/models.md#composition-beside-a-union), the node a field's
	// declarations compose or meet to, the identity of a field another model
	// declares first, and a member its composition settles, beneath allOf at
	// properties and its name
	// (docs/compilers/openapi.md#allof-redeclared-property). A variant's path
	// ends in oneOf or anyOf and an index, a minted union's in that keyword, and
	// a field's in properties and a name, so none meet. ForPointer yields only
	// the openapi and anon spaces: no $ref resolves to a composed ID.
	ComposedSpace compile.Space = "composed"
)

// The spaces an operation group is addressed in. No OpenAPI construct is a
// group: a Tag Object is metadata about a name, and an operation may tag itself
// with a name no Tag Object declares. The grouping rule mints the group, so each
// rule takes a namespace of its own (docs/ir/identity.md), distinct from
// OpenAPISpace, which addresses source coordinates. The first three are named
// for the strategy that forms the group, the last two for the one group they
// hold.
const (
	// TagGroupSpace holds the groups formed by operation tag, keyed by tag name.
	TagGroupSpace compile.Space = "tags"
	// PathPrefixGroupSpace holds the groups formed by first path segment.
	PathPrefixGroupSpace compile.Space = "path-prefix"
	// ExtensionGroupSpace holds the groups formed by the group path a vendor
	// extension states, keyed by the path.
	ExtensionGroupSpace compile.Space = "extension"
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

// ExtensionGroup returns the stable ID of the group a vendor extension's group
// path names, outermost segment first. It is keyed by the path alone, not by the
// extension stating it, so a configuration can name the group, and the group a
// path nests in is its prefix's.
func ExtensionGroup(path []string) ir.GroupID {
	return compile.GroupID(ExtensionGroupSpace, string(Ptr(path...)))
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
func Namespaces(others ...DocumentIDNamespace) compile.Namespaces {
	ns := compile.Namespaces{
		ir.IDKindType:       {OpenAPISpace, AnonymousSpace, ComposedSpace},
		ir.IDKindOperation:  {OpenAPISpace},
		ir.IDKindProperty:   {OpenAPISpace, ComposedSpace},
		ir.IDKindAuth:       {OpenAPISpace, compile.UnresolvedSpace},
		ir.IDKindService:    {OpenAPISpace},
		ir.IDKindGroup:      {TagGroupSpace, PathPrefixGroupSpace, ExtensionGroupSpace, DefaultGroupSpace, WebhookGroupSpace},
		ir.IDKindParameter:  {OpenAPISpace},
		ir.IDKindEnumMember: {OpenAPISpace, AnonymousSpace, ComposedSpace},
		ir.IDKindServer:     {OpenAPISpace, DefaultServerSpace},
	}
	for _, documentIDNamespace := range others {
		ns[ir.IDKindType] = append(ns[ir.IDKindType], documentIDNamespace.space(OpenAPISpace), documentIDNamespace.space(AnonymousSpace), documentIDNamespace.space(ComposedSpace))
		ns[ir.IDKindProperty] = append(ns[ir.IDKindProperty], documentIDNamespace.space(OpenAPISpace), documentIDNamespace.space(ComposedSpace))
		ns[ir.IDKindEnumMember] = append(ns[ir.IDKindEnumMember], documentIDNamespace.space(OpenAPISpace), documentIDNamespace.space(AnonymousSpace),
			documentIDNamespace.space(ComposedSpace))
		ns[ir.IDKindServer] = append(ns[ir.IDKindServer], documentIDNamespace.space(OpenAPISpace))
		ns[ir.IDKindAuth] = append(ns[ir.IDKindAuth], documentIDNamespace.space(OpenAPISpace))
	}
	return ns
}

// DocumentIDNamespace is a document of the compile as the IDs of what it
// declares name it. The zero value is the source, whose IDs take the bare
// spaces; another is named by its label (In), which qualifies each space
// (compile.Space.Qualified), so one pointer in two documents derives two IDs,
// each still the pointer it records. Operations, parameters and services are
// named by where the source mounts or names them, so they take no Document; a
// server, and a security scheme a requirement key reaches outside the source's
// securitySchemes, is named where it is declared, so it does.
type DocumentIDNamespace struct {
	label string
}

// In returns the document labelled label: its location, as the source's
// directory reaches it. The empty label is the source's.
func In(label string) DocumentIDNamespace { return DocumentIDNamespace{label: label} }

// space returns space as d names it: space itself for the source, else
// qualified by d.
func (d DocumentIDNamespace) space(space compile.Space) compile.Space {
	if d.label == "" {
		return space
	}
	return space.Qualified(d.label)
}

// Coordinate returns the key the type registry interns the schema at pointer
// in d under: the pointer itself in the source, and the pointer behind d's
// qualifier elsewhere, which no pointer spells, so one pointer in two
// documents is two coordinates.
func (d DocumentIDNamespace) Coordinate(pointer jsontext.Pointer) string {
	if d.label == "" {
		return string(pointer)
	}
	return string(compile.Space("").Qualified(d.label)) + string(pointer)
}

// NamedType returns the stable ID of a components-named schema at pointer in d.
func (d DocumentIDNamespace) NamedType(pointer jsontext.Pointer) ir.TypeID {
	return compile.TypeID(d.space(OpenAPISpace), string(pointer))
}

// AnonymousType returns the stable ID of a hoisted inline type at pointer in d.
func (d DocumentIDNamespace) AnonymousType(pointer jsontext.Pointer) ir.TypeID {
	return compile.TypeID(d.space(AnonymousSpace), string(pointer))
}

// ComposedType returns the stable ID of the node synthesized at pointer in d:
// a distributed union variant's branch, the keyword writing the union a routed
// subtype composes (docs/ir/models.md#composition-beside-a-union), or the
// declaration naming a field whose declarations compose or meet to a node none
// of them wrote.
func (d DocumentIDNamespace) ComposedType(pointer jsontext.Pointer) ir.TypeID {
	return compile.TypeID(d.space(ComposedSpace), string(pointer))
}

// ComposedProperty returns the stable ID of a field the declaration at pointer
// in d names, where another model carries the field's first declaration, so
// that no declaration's own ID is taken.
func (d DocumentIDNamespace) ComposedProperty(pointer jsontext.Pointer) ir.PropertyID {
	return compile.PropertyID(d.space(ComposedSpace), string(pointer))
}

// Operation returns the stable ID of the operation at pointer.
func Operation(pointer jsontext.Pointer) ir.OperationID {
	return compile.OperationID(OpenAPISpace, string(pointer))
}

// Property returns the stable ID of the property at pointer in d.
func (d DocumentIDNamespace) Property(pointer jsontext.Pointer) ir.PropertyID {
	return compile.PropertyID(d.space(OpenAPISpace), string(pointer))
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
func Param(mount jsontext.Pointer, name, in string, occurrence int) ir.ParameterID {
	return compile.ParameterID(OpenAPISpace, string(mount+ParamPath(name, in, occurrence)))
}

// ParamSlot is what OpenAPI identifies a parameter by within one list: its
// location and its name, a header's in lower case, since HTTP compares field
// names in any case (RFC 9110 §5.1), so X-Trace and x-trace are one header.
// It merges and finds repeats; Param derives an ID from the name as written,
// numbering a repeat among the parameters spelled alike.
type ParamSlot struct {
	Name, In string
}

// SlotOf returns the slot of a parameter named name in location in.
func SlotOf(name, in string) ParamSlot {
	if in == "header" {
		name = strings.ToLower(name)
	}
	return ParamSlot{Name: name, In: in}
}

// ParamPath returns the path below a mount that Param mints the occurrence-th
// holder of name in location in from. A path item that mounts no operation
// keys each parameter it keeps verbatim by it, so a kept parameter and a
// lowered one spell a slot alike.
func ParamPath(name, in string, occurrence int) jsontext.Pointer {
	return numbered(Ptr("parameters", name, in), occurrence)
}

// ComposedMember returns the stable ID of a member of the enum minted at
// pointer in d (ComposedType): its key and the number a repeated value appends,
// tail, as they follow the path of the enum it was taken from.
func (d DocumentIDNamespace) ComposedMember(pointer jsontext.Pointer, tail string) ir.EnumMemberID {
	return compile.EnumMemberID(d.space(ComposedSpace), string(pointer)+tail)
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
func (d DocumentIDNamespace) Server(pointer jsontext.Pointer) ir.ServerID {
	return compile.ServerID(d.space(OpenAPISpace), string(pointer))
}

// DefaultServer returns the stable ID of the server OpenAPI supplies for a
// document that declares none.
func DefaultServer() ir.ServerID { return compile.ServerID(DefaultServerSpace, "") }

// Auth returns the stable ID of the named security scheme.
func Auth(name string) ir.AuthID {
	return compile.AuthID(OpenAPISpace, string(schemePointer(name)))
}

// Auth returns the stable ID of the Security Scheme Object at pointer in d, a
// position outside the source's securitySchemes a requirement key reaches.
func (d DocumentIDNamespace) Auth(pointer jsontext.Pointer) ir.AuthID {
	return compile.AuthID(d.space(OpenAPISpace), string(pointer))
}

// UnresolvedAuth returns the stable ID of the scheme a requirement key written
// in d that reaches no mechanism lowers to: in the one space ir reserves for
// that kind, at the pointer a declaration of key has, behind d's qualifier for
// another document (Coordinate), so one key in two documents is two schemes.
func (d DocumentIDNamespace) UnresolvedAuth(key string) ir.AuthID {
	return compile.AuthID(compile.UnresolvedSpace, d.Coordinate(schemePointer(key)))
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
// componentEntryName drops the kind.
func ComponentEntry(pointer jsontext.Pointer) (kind, name string, ok bool) {
	kind, name, ok = ComponentKey(pointer)
	if !ok || name == "" {
		return "", "", false
	}
	return kind, name, true
}

// componentsRoot is the pointer every component entry sits two tokens beneath.
const componentsRoot jsontext.Pointer = "/components"

// ComponentKey is the one place the /components/<kind>/<name> shape is
// parsed: it reads the kind and name off pointer's last two tokens, decoded, when
// the pointer sits two tokens beneath /components.
//
// It does not judge the name, so a caller that must tell "not that shape at all"
// from "that shape with an empty name" can. ComponentEntry folds the two together
// on purpose, and ComponentSchema narrows it to the schemas kind, the only one
// that earns a named TypeID, but the two are different facts about a document,
// and a diagnostic naming the wrong one is simply false.
func ComponentKey(pointer jsontext.Pointer) (kind, name string, ok bool) {
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

// componentEntryName returns the unescaped name of a top-level component entry
// of any kind.
func componentEntryName(pointer jsontext.Pointer) (string, bool) {
	_, name, ok := ComponentEntry(pointer)
	return name, ok
}

// ForPointer returns the stable TypeID for a schema hoisted at pointer in d:
// the named-component ID for a top-level component schema, the anonymous
// (hoisted-inline) ID otherwise.
func (d DocumentIDNamespace) ForPointer(pointer jsontext.Pointer) ir.TypeID {
	if schemaSpace(pointer) == OpenAPISpace {
		return d.NamedType(pointer)
	}
	return d.AnonymousType(pointer)
}

// schemaSpace returns the namespace of the type a schema at pointer lowers to:
// OpenAPISpace for a top-level component schema, the one kind that declares a
// named type, and AnonymousSpace for every other position.
func schemaSpace(pointer jsontext.Pointer) compile.Space {
	if _, state := ComponentSchema(pointer); state == NamedComponent {
		return OpenAPISpace
	}
	return AnonymousSpace
}

// ComponentState is what a pointer is to a document's component schemas. It is
// one answer rather than a name and a flag beside a second predicate, so no
// caller can report a component keyed "" in another's words (GitHub #405).
type ComponentState uint8

const (
	// NotComponent is a pointer addressing no top-level component schema.
	NotComponent ComponentState = iota
	// NamedComponent is a top-level component schema with a name, and from
	// componentlookup.Scope.ComponentRef, one the document declares.
	NamedComponent
	// UndeclaredComponent is a pointer shaped as a component schema's naming
	// none the document declares, which only componentlookup.Scope.ComponentRef
	// can tell.
	UndeclaredComponent
	// UnnamedComponent is the component schema keyed "", the position
	// /components/schemas/ addresses. It is a component schema, and an empty name
	// earns no named TypeID, so it hoists anonymously
	// (testdata/conformance/openapi/empty-names.yaml records that policy).
	UnnamedComponent
)

// ByName reports whether the state settles a reference by name, one way or the
// other: a declared component resolves, and an undeclared one dangles. In any
// other state the pointer is read as the position it is.
func (s ComponentState) ByName() bool { return s == NamedComponent || s == UndeclaredComponent }

// ComponentSchema classifies pointer against the top-level component schemas,
// and returns the decoded name of a NamedComponent.
func ComponentSchema(pointer jsontext.Pointer) (string, ComponentState) {
	kind, name, ok := ComponentKey(pointer)
	switch {
	case !ok || kind != "schemas":
		return "", NotComponent
	case name == "":
		return "", UnnamedComponent
	default:
		return name, NamedComponent
	}
}
