// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package ids_test

import (
	"encoding/json/jsontext"
	"fmt"
	"slices"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dexpace/morphic/compilers/openapi/internal/ids"
	"github.com/dexpace/morphic/ir"
)

// TestPtr_EscapesPerRFC6901 pins the escaping every ID in this format is built
// on. A segment that escapes wrongly derives an ID for a coordinate the source
// never wrote, and two coordinates that should differ can collapse onto one.
func TestPtr_EscapesPerRFC6901(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name     string
		segments []string
		want     jsontext.Pointer
	}{
		{name: "plain", segments: []string{"components", "schemas", "User"}, want: "/components/schemas/User"},
		{name: "slash in segment", segments: []string{"paths", "/users/{id}", "get"}, want: "/paths/~1users~1{id}/get"},
		{name: "tilde in segment", segments: []string{"components", "schemas", "a~b"}, want: "/components/schemas/a~0b"},
		{name: "tilde before slash, so a ~1 in the source survives", segments: []string{"x", "~/"}, want: "/x/~0~1"},
		{name: "no segments is the whole document", segments: nil, want: ""},
		{name: "an empty segment is still a segment", segments: []string{""}, want: "/"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, tc.want, ids.Ptr(tc.segments...))
		})
	}
}

// TestScope_EscapesLikePtrWithoutTheLeadingSeparator pins the Unmodeled key
// scope: Ptr's escaping, joined without a leading separator because a scope is a
// relative path.
//
// The slash case is the one that matters. A scope addressing an object the
// document names — a form part, a callback — takes that name as one segment, and
// leaving a "/" in it unescaped let two such objects spell one key between them
// with the survivor following declaration order.
func TestScope_EscapesLikePtrWithoutTheLeadingSeparator(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name     string
		segments []string
		want     string
	}{
		{name: "plain", segments: []string{"encoding", "avatar"}, want: "encoding/avatar"},
		{name: "slash in a document-chosen name stays one segment",
			segments: []string{"encoding", "q/x-a"}, want: "encoding/q~1x-a"},
		{name: "tilde too", segments: []string{"callbacks", "a~b"}, want: "callbacks/a~0b"},
		{name: "tilde before slash, so a ~1 in the source survives",
			segments: []string{"callbacks", "~/"}, want: "callbacks/~0~1"},
		{name: "one segment takes no separator", segments: []string{"itemEncoding"}, want: "itemEncoding"},
		{name: "no segments is the unscoped key", segments: nil, want: ""},
		{name: "an empty segment is still a segment", segments: []string{"encoding", ""}, want: "encoding/"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, tc.want, ids.Scope(tc.segments...))
		})
	}
}

// TestScope_DistinguishesNamesThatDifferOnlyBySeparator is the property the
// escaping exists for, stated directly: two names one of which spells the
// other's scope plus a segment must not produce one key.
func TestScope_DistinguishesNamesThatDifferOnlyBySeparator(t *testing.T) {
	t.Parallel()
	plain := ids.Scope("encoding", "q") + "/x-a/x-b"
	slashed := ids.Scope("encoding", "q/x-a") + "/x-b"
	assert.NotEqual(t, plain, slashed,
		"a part named q/x-a must not land on the key a part named q writes")
}

// TestPtr_TokensRoundTripAndValidate pins that a pointer Ptr builds reads back
// token for token through jsontext.Pointer, which is how every component name is
// recovered, and is a valid RFC 6901 pointer. A name holding a slash or a tilde
// that did not come back would be read as a different name.
func TestPtr_TokensRoundTripAndValidate(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name   string
		tokens []string
	}{
		{name: "plain", tokens: []string{"User"}},
		{name: "tilde", tokens: []string{"a~b"}},
		{name: "slash", tokens: []string{"a/b"}},
		{name: "a name spelled like the escape of a slash", tokens: []string{"~1"}},
		{name: "a name spelled like the escape of a tilde", tokens: []string{"~0"}},
		{name: "tilde zero one", tokens: []string{"~01"}},
		{name: "empty token", tokens: []string{""}},
		{name: "tilde then slash", tokens: []string{"a~b/c"}},
		{name: "non-ASCII", tokens: []string{"é"}},
		{name: "multiple tokens including an empty one", tokens: []string{"a", "", "b~c"}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			p := ids.Ptr(tc.tokens...)
			assert.Equal(t, tc.tokens, slices.Collect(p.Tokens()),
				"escaping %q produced %q, which does not come back", tc.tokens, p)
			assert.True(t, p.IsValid(), "%q is not a valid RFC 6901 pointer", p)
		})
	}
}

// TestComponentEntry_ParsesOnlyATopLevelEntry pins the one place the
// /components/<kind>/<name> shape is read. A pointer misread as a component
// entry names a type the source did not declare; one misread as *not* an entry
// loses a named type to the anonymous namespace.
func TestComponentEntry_ParsesOnlyATopLevelEntry(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name       string
		pointer    jsontext.Pointer
		wantKind   string
		wantEntry  string
		wantIsAnOK bool
	}{
		{
			name: "a schema entry", pointer: "/components/schemas/User",
			wantKind: "schemas", wantEntry: "User", wantIsAnOK: true,
		},
		{
			name: "another kind", pointer: "/components/headers/X-Rate",
			wantKind: "headers", wantEntry: "X-Rate", wantIsAnOK: true,
		},
		{
			name: "an escaped name comes back unescaped", pointer: "/components/schemas/a~1b",
			wantKind: "schemas", wantEntry: "a/b", wantIsAnOK: true,
		},
		{
			name: "an escaped kind comes back unescaped too", pointer: "/components/a~1b/User",
			wantKind: "a/b", wantEntry: "User", wantIsAnOK: true,
		},

		{name: "a path inside an entry is not the entry", pointer: "/components/schemas/User/properties/id"},
		{name: "the kind alone", pointer: "/components/schemas"},
		{name: "no name after the kind", pointer: "/components/schemas/"},
		{name: "not under components", pointer: "/paths/~1x/get"},
		{name: "the empty pointer", pointer: ""},
		{name: "no leading separator is not a pointer at all", pointer: "components/schemas/User"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			kind, entry, ok := ids.ComponentEntry(tc.pointer)
			assert.Equal(t, tc.wantIsAnOK, ok, "pointer %q", tc.pointer)
			assert.Equal(t, tc.wantKind, kind)
			assert.Equal(t, tc.wantEntry, entry)
		})
	}
}

// TestComponentSchemaName_NarrowsToSchemas pins that only the schemas kind
// declares a named type. It is what gates NamedType, so admitting another kind
// would mint a named ID for a header or a parameter that no reference resolves
// to.
func TestComponentSchemaName_NarrowsToSchemas(t *testing.T) {
	t.Parallel()
	name, ok := ids.ComponentSchemaName("/components/schemas/User")
	assert.True(t, ok)
	assert.Equal(t, "User", name)

	for _, pointer := range []jsontext.Pointer{
		"/components/headers/X-Rate", "/components/parameters/Sort",
		"/components/responses/NotFound", "/components/schemas/User/properties/id",
	} {
		_, ok := ids.ComponentSchemaName(pointer)
		assert.False(t, ok, "%q declares no named type", pointer)
	}
}

// TestComponentSchemaNamedEmpty_SeparatesAnEmptyNameFromNoEntry pins the one
// distinction ComponentEntry deliberately throws away. Both answer "no named
// type", but they are different facts: a component schema keyed "" exists at
// /components/schemas/ and earns none, while the other pointers name no
// component-schema entry at all. Only a caller that can tell them apart can
// report the first without denying the schema the document plainly declares.
func TestComponentSchemaNamedEmpty_SeparatesAnEmptyNameFromNoEntry(t *testing.T) {
	t.Parallel()
	assert.True(t, ids.ComponentSchemaNamedEmpty("/components/schemas/"),
		`/components/schemas/ addresses the component schema keyed ""`)

	for _, pointer := range []jsontext.Pointer{
		"/components/schemas/User",           // a named entry
		"/components/headers/",               // an empty name of another kind
		"/components/schemas",                // the kind alone, no trailing token
		"/components/schemas//properties/id", // a position inside the empty-named schema
		"/components//",                      // no kind
		"/paths/~1x/get",                     // not under components
		"",                                   // the empty pointer
	} {
		assert.False(t, ids.ComponentSchemaNamedEmpty(pointer),
			`%q does not address the component schema keyed ""`, pointer)
	}
}

// TestForPointer_ChoosesTheNamespace pins the split ForPointer exists for: a
// component schema keeps its named ID, everything else is anonymous. The two
// namespaces are what stop a hoisted inline type colliding with a declared one.
func TestForPointer_ChoosesTheNamespace(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name    string
		pointer jsontext.Pointer
		want    ir.TypeID
	}{
		{name: "a component schema is named", pointer: "/components/schemas/User", want: "t/openapi/components/schemas/User"},
		{name: "an inline position is anonymous", pointer: "/components/schemas/User/properties/id", want: "t/anon/components/schemas/User/properties/id"},
		{name: "a component of another kind is anonymous", pointer: "/components/headers/X-Rate", want: "t/anon/components/headers/X-Rate"},
		{name: "a media-type schema is anonymous", pointer: "/paths/~1x/get/responses/200/content/application~1json/schema", want: "t/anon/paths/~1x/get/responses/200/content/application~1json/schema"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, tc.want, ids.Document{}.ForPointer(tc.pointer))
		})
	}
}

// TestDerivations_AreDistinctAndPointerShaped pins that each constructor writes
// its own kind and namespace. Two constructors agreeing would let a property and
// a type share an ID, and irverify holds every ID to the <kind>/<space>/<path>
// shape these produce.
func TestDerivations_AreDistinctAndPointerShaped(t *testing.T) {
	t.Parallel()
	const pointer = "/components/schemas/User"
	got := map[string]string{
		"named":    string(ids.Document{}.NamedType(pointer)),
		"anon":     string(ids.Document{}.AnonType(pointer)),
		"composed": string(ids.Document{}.ComposedType(pointer)),
		"op":       string(ids.Op(pointer)),
		"prop":     string(ids.Document{}.Prop(pointer)),
		"minted":   string(ids.Document{}.ComposedProp(pointer)),
		"auth":     string(ids.Auth("apiKey")),
		"service":  string(ids.Service(0)),
	}
	want := map[string]string{
		"named":    "t/openapi/components/schemas/User",
		"anon":     "t/anon/components/schemas/User",
		"composed": "t/composed/components/schemas/User",
		"op":       "op/openapi/components/schemas/User",
		"prop":     "p/openapi/components/schemas/User",
		"minted":   "p/composed/components/schemas/User",
		"auth":     "auth/openapi/components/securitySchemes/apiKey",
		"service":  "s/openapi/0",
	}
	assert.Equal(t, want, got)

	seen := map[string]string{}
	for kind, id := range got {
		if other, dup := seen[id]; dup {
			t.Errorf("%s and %s derive the same ID %q", kind, other, id)
		}
		seen[id] = kind
	}
}

// TestDocument_QualifiesEachSpaceByItsLabel pins how another document's IDs
// name it: each space qualified by its label as one segment, so a '/' or '~'
// in a label cannot be read as a further path step, and its coordinates
// behind that qualifier, so one pointer in two documents is two of each. The
// empty label is the source's, which takes the bare spaces.
func TestDocument_QualifiesEachSpaceByItsLabel(t *testing.T) {
	t.Parallel()
	const pointer = "/components/schemas/User"
	other := ids.In("dir/a~b.yaml")
	assert.Equal(t, map[string]string{
		"named":      "t/openapi@dir~1a~0b.yaml/components/schemas/User",
		"anon":       "t/anon@dir~1a~0b.yaml/components/schemas/User",
		"composed":   "t/composed@dir~1a~0b.yaml/components/schemas/User",
		"for":        "t/anon@dir~1a~0b.yaml/X",
		"prop":       "p/openapi@dir~1a~0b.yaml/components/schemas/User",
		"minted":     "p/composed@dir~1a~0b.yaml/components/schemas/User",
		"member":     "e/openapi@dir~1a~0b.yaml/components/schemas/User/s:a",
		"coordinate": "@dir~1a~0b.yaml/components/schemas/User",
	}, map[string]string{
		"named":      string(other.NamedType(pointer)),
		"anon":       string(other.AnonType(pointer)),
		"composed":   string(other.ComposedType(pointer)),
		"for":        string(other.ForPointer("/X")),
		"prop":       string(other.Prop(pointer)),
		"minted":     string(other.ComposedProp(pointer)),
		"member":     string(other.EnumMember(pointer, "s:a", 1)),
		"coordinate": other.Coordinate(pointer),
	})

	assert.Equal(t, ids.Document{}, ids.In(""), "the empty label is the source's")
	assert.Equal(t, pointer, ids.Document{}.Coordinate(pointer))
	assert.NotEqual(t, other.Coordinate(pointer), ids.In("dir").Coordinate(pointer))
}

// TestDeclarationHint_PrefersTheComponentName pins why a hint is not taken from
// the use site: a $ref'd component lowers once, at its declaration, so a hint
// derived from whichever reference lowered first would name the one shared node
// arbitrarily.
func TestDeclarationHint_PrefersTheComponentName(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name     string
		pointer  jsontext.Pointer
		fallback string
		want     string
	}{
		{
			name: "a component entry names itself", pointer: "/components/schemas/User",
			fallback: "listUsers_request", want: "User",
		},
		{
			name: "a component of any kind does", pointer: "/components/requestBodies/OrderBody",
			fallback: "submitOrder_request", want: "OrderBody",
		},
		{
			name:     "an inline position takes the fallback",
			pointer:  "/paths/~1x/post/requestBody/content/application~1json/schema",
			fallback: "submitOrder_request", want: "submitOrder_request",
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, tc.want, ids.DeclarationHint(tc.pointer, tc.fallback))
		})
	}
}

// TestGroupIDs_SpellEachRule pins the spelling of every group ID, written out
// rather than derived so a test cannot agree with a change to the derivation.
// The empty key is the odd one: a space naming a single node takes no trailing
// separator, so the tag "" is "g/tags" and not a malformed "g/tags/".
func TestGroupIDs_SpellEachRule(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		got  ir.GroupID
		want ir.GroupID
	}{
		{name: "a tag", got: ids.TagGroup("pets"), want: "g/tags/pets"},
		{name: "a tag with a slash", got: ids.TagGroup("a/b"), want: "g/tags/a~1b"},
		{name: "a tag with a tilde", got: ids.TagGroup("a~b"), want: "g/tags/a~0b"},
		{name: "the tag named by the empty string", got: ids.TagGroup(""), want: "g/tags"},
		{name: "a path prefix", got: ids.PathPrefixGroup("users"), want: "g/path-prefix/users"},
		{name: "a path prefix with a template", got: ids.PathPrefixGroup("{id}"), want: "g/path-prefix/{id}"},
		{name: "a path prefix with a tilde", got: ids.PathPrefixGroup("a~b"), want: "g/path-prefix/a~0b"},
		{name: "a path prefix with a slash", got: ids.PathPrefixGroup("a/b"), want: "g/path-prefix/a~1b"},
		{name: "the root path's prefix", got: ids.PathPrefixGroup(""), want: "g/path-prefix"},
		{name: "untagged operations", got: ids.DefaultGroup(), want: "g/default"},
		{name: "webhook operations", got: ids.WebhookGroup(), want: "g/webhooks"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, tc.want, tc.got)
			assert.True(t, ir.WellFormedID(ir.IDKindGroup, string(tc.got)), "%q must be a well-formed group ID", tc.got)
		})
	}
}

// TestGroupIDs_NeverCollide holds the property the IDs exist for: groups that
// differ in the rule that formed them or the key it read have different IDs.
// The keys include names that look like the synthesized groups ("default",
// "webhooks") and every string up to length four over the characters RFC 6901
// escapes plus the digits an escape is spelled with: a tag "a/b" and a tag
// "a~1b" collide if "~" is left unescaped, which a hand-picked list tends to miss.
func TestGroupIDs_NeverCollide(t *testing.T) {
	t.Parallel()
	exhaustive := stringsOver([]string{"a", "/", "~", "0", "1"}, 4)
	require.Len(t, exhaustive, 781, "5^0 + … + 5^4 strings; a shorter list would pass this test vacuously")
	keys := slices.Concat([]string{"default", "webhooks", "tags", "path-prefix"}, exhaustive)

	rules := []struct {
		name string
		id   func(key string) ir.GroupID
	}{
		{name: "tag", id: ids.TagGroup},
		{name: "path prefix", id: ids.PathPrefixGroup},
	}
	seen := map[ir.GroupID]string{
		ids.DefaultGroup(): "the default group",
		ids.WebhookGroup(): "the webhook group",
	}
	for _, key := range keys {
		for _, rule := range rules {
			id := rule.id(key)
			label := fmt.Sprintf("%s %q", rule.name, key)
			prior, taken := seen[id]
			require.Falsef(t, taken, "%s and %s are both %q", label, prior, id)
			seen[id] = label
			require.Truef(t, ir.WellFormedID(ir.IDKindGroup, string(id)), "%s: %q", label, id)
		}
	}
}

// stringsOver returns every string of length zero to maxLen over the alphabet,
// each once, shortest first.
func stringsOver(alphabet []string, maxLen int) []string {
	total, width := 1, 1
	for range maxLen {
		width *= len(alphabet)
		total += width
	}
	all := make([]string, 1, total)
	for parent := 0; len(all) < total; parent++ {
		for _, letter := range alphabet {
			all = append(all, all[parent]+letter)
		}
	}
	return all
}

// TestNamespaces_NameEveryNamespaceThisCompilerMints pins the vocabulary the
// document declares, written out so a namespace cannot be added to a derivation
// without someone deciding it belongs here. The compiler's own corpus test holds
// the same list to what the corpus really uses.
func TestNamespaces_NameEveryNamespaceThisCompilerMints(t *testing.T) {
	t.Parallel()
	assert.Equal(t, map[string][]string{
		ir.IDKindType:       {"anon", "composed", "openapi"},
		ir.IDKindOp:         {"openapi"},
		ir.IDKindProp:       {"composed", "openapi"},
		ir.IDKindAuth:       {"openapi"},
		ir.IDKindService:    {"openapi"},
		ir.IDKindGroup:      {"default", "path-prefix", "tags", "webhooks"},
		ir.IDKindParam:      {"openapi"},
		ir.IDKindEnumMember: {"anon", "openapi"},
		ir.IDKindServer:     {"default", "openapi"},
	}, ids.Namespaces().Declaration())
}

// TestNamespaces_DeclareEachOtherDocumentsSpaces pins that a compile reading
// other documents declares, for each, the type, property, enum member and
// server spaces its IDs are minted in, and no operation, parameter, security
// scheme or service space, which the source's mounts and names alone take.
func TestNamespaces_DeclareEachOtherDocumentsSpaces(t *testing.T) {
	t.Parallel()
	assert.Equal(t, map[string][]string{
		ir.IDKindType: {"anon", "anon@a.yaml", "anon@b~1c.yaml", "composed", "composed@a.yaml",
			"composed@b~1c.yaml", "openapi", "openapi@a.yaml", "openapi@b~1c.yaml"},
		ir.IDKindOp:         {"openapi"},
		ir.IDKindProp:       {"composed", "composed@a.yaml", "composed@b~1c.yaml", "openapi", "openapi@a.yaml", "openapi@b~1c.yaml"},
		ir.IDKindAuth:       {"openapi"},
		ir.IDKindService:    {"openapi"},
		ir.IDKindGroup:      {"default", "path-prefix", "tags", "webhooks"},
		ir.IDKindParam:      {"openapi"},
		ir.IDKindEnumMember: {"anon", "anon@a.yaml", "anon@b~1c.yaml", "openapi", "openapi@a.yaml", "openapi@b~1c.yaml"},
		ir.IDKindServer:     {"default", "openapi", "openapi@a.yaml", "openapi@b~1c.yaml"},
	}, ids.Namespaces(ids.In("a.yaml"), ids.In("b/c.yaml")).Declaration())
}

// TestServer_NamedWhereDeclared pins a server's ID: the pointer of its Server
// Object in the document declaring it, that document's qualifier included, and
// the one server no position writes minted in a space no pointer reaches.
func TestServer_NamedWhereDeclared(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		got  ir.ServerID
		want ir.ServerID
	}{
		{name: "the document's own", got: ids.In("").Server(ids.Ptr("servers", "0")), want: "server/openapi/servers/0"},
		{name: "a path item's", got: ids.In("").Server(ids.Ptr("paths", "/a", "servers", "1")),
			want: "server/openapi/paths/~1a/servers/1"},
		{name: "an operation's", got: ids.In("").Server(ids.Ptr("paths", "/a", "get", "servers", "0")),
			want: "server/openapi/paths/~1a/get/servers/0"},
		{name: "another document's", got: ids.In("b/c.yaml").Server(ids.Ptr("paths", "/a", "servers", "0")),
			want: "server/openapi@b~1c.yaml/paths/~1a/servers/0"},
		{name: "the default", got: ids.DefaultServer(), want: "server/default"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, tc.want, tc.got)
			assert.True(t, ir.WellFormedID(ir.IDKindServer, string(tc.got)), "%q must be a well-formed server ID", tc.got)
		})
	}
}

// TestParam_ScopesByOperationNameAndLocation pins the spelling of a parameter ID
// and what it is scoped by, written out rather than derived. The mount, the name
// and the location each change it; the name is one escaped token, so a slash in
// it never reads as two segments. A repeat appends its occurrence number.
func TestParam_ScopesByOperationNameAndLocation(t *testing.T) {
	t.Parallel()
	const mount = jsontext.Pointer("/paths/~1items/get")
	tests := []struct {
		name string
		got  ir.ParamID
		want ir.ParamID
	}{
		{name: "a query parameter", got: ids.Param(mount, "id", "query", 1), want: "param/openapi/paths/~1items/get/parameters/id/query"},
		{name: "the same name in the path", got: ids.Param(mount, "id", "path", 1), want: "param/openapi/paths/~1items/get/parameters/id/path"},
		{name: "another operation", got: ids.Param("/paths/~1items/post", "id", "query", 1), want: "param/openapi/paths/~1items/post/parameters/id/query"},
		{name: "a slash in the name", got: ids.Param(mount, "a/b", "header", 1), want: "param/openapi/paths/~1items/get/parameters/a~1b/header"},
		{name: "a tilde in the name", got: ids.Param(mount, "a~b", "header", 1), want: "param/openapi/paths/~1items/get/parameters/a~0b/header"},
		{name: "a repeat", got: ids.Param(mount, "id", "cookie", 2), want: "param/openapi/paths/~1items/get/parameters/id/cookie/2"},
		{name: "a third occurrence", got: ids.Param(mount, "id", "cookie", 3), want: "param/openapi/paths/~1items/get/parameters/id/cookie/3"},
	}
	seen := map[ir.ParamID]string{}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, tc.want, tc.got)
			assert.True(t, ir.WellFormedID(ir.IDKindParam, string(tc.got)), "%q must be a well-formed param ID", tc.got)
		})
		require.NotContains(t, seen, tc.got, "%s and %s share an ID", tc.name, seen[tc.got])
		seen[tc.got] = tc.name
	}
}

// TestEnumMember_LivesInItsEnumsSpaceAndPath pins the namespace of a member to
// that of its enum's TypeID, for a named enum and an anonymous one, and its path
// to the enum's pointer followed by one escaped key, and by the occurrence number
// for a repeat. A key spelling a slash and a number is not a repeat.
func TestEnumMember_LivesInItsEnumsSpaceAndPath(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name       string
		pointer    jsontext.Pointer
		key        string
		occurrence int
		want       ir.EnumMemberID
		enum       ir.TypeID
	}{
		{"a component schema", "/components/schemas/E", "s:a", 1, "e/openapi/components/schemas/E/s:a", "t/openapi/components/schemas/E"},
		{"an inline schema", "/components/schemas/S/properties/p", "s:a", 1, "e/anon/components/schemas/S/properties/p/s:a", "t/anon/components/schemas/S/properties/p"},
		{"a key holding a slash", "/components/schemas/E", "s:a/b", 1, "e/openapi/components/schemas/E/s:a~1b", "t/openapi/components/schemas/E"},
		{"a key holding a tilde", "/components/schemas/E", "s:a~b", 1, "e/openapi/components/schemas/E/s:a~0b", "t/openapi/components/schemas/E"},
		{"a repeat", "/components/schemas/E", "s:a", 2, "e/openapi/components/schemas/E/s:a/2", "t/openapi/components/schemas/E"},
		{"a third occurrence", "/components/schemas/E", "s:a", 3, "e/openapi/components/schemas/E/s:a/3", "t/openapi/components/schemas/E"},
		{"a key spelling a repeat", "/components/schemas/E", "s:a/2", 1, "e/openapi/components/schemas/E/s:a~12", "t/openapi/components/schemas/E"},
	}
	seen := map[ir.EnumMemberID]string{}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got := ids.Document{}.EnumMember(tc.pointer, tc.key, tc.occurrence)
			assert.Equal(t, tc.want, got)
			assert.True(t, ir.WellFormedID(ir.IDKindEnumMember, string(got)))
			memberSpace, _ := ir.IDSpace(ir.IDKindEnumMember, string(got))
			enumSpace, _ := ir.IDSpace(ir.IDKindType, string(ids.Document{}.ForPointer(tc.pointer)))
			assert.Equal(t, enumSpace, memberSpace, "a member lives in its enum's own namespace")
			assert.Equal(t, tc.enum, ids.Document{}.ForPointer(tc.pointer))
		})
		require.NotContains(t, seen, tc.want, "%s and %s share an ID", tc.name, seen[tc.want])
		seen[tc.want] = tc.name
	}
}
