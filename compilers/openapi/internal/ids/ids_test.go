package ids_test

import (
	"testing"

	"github.com/stretchr/testify/assert"

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
		want     string
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

// TestUnescapeSegment_ReversesPtr pins the other direction, which recovers a
// component's on-wire name from a pointer segment. The two must round-trip or a
// name containing a slash or a tilde comes back as a different name.
func TestUnescapeSegment_ReversesPtr(t *testing.T) {
	t.Parallel()
	for _, name := range []string{"User", "a~b", "a/b", "~1", "~0", "~01", "", "a~b/c"} {
		escaped := ids.Ptr(name)[1:] // Ptr writes the leading separator
		assert.Equal(t, name, ids.UnescapeSegment(escaped),
			"escaping %q produced %q, which does not come back", name, escaped)
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
		pointer    string
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

		{name: "a path inside an entry is not the entry", pointer: "/components/schemas/User/properties/id"},
		{name: "the kind alone", pointer: "/components/schemas"},
		{name: "no name after the kind", pointer: "/components/schemas/"},
		{name: "not under components", pointer: "/paths/~1x/get"},
		{name: "the empty pointer", pointer: ""},
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

	for _, pointer := range []string{
		"/components/headers/X-Rate", "/components/parameters/Sort",
		"/components/responses/NotFound", "/components/schemas/User/properties/id",
	} {
		_, ok := ids.ComponentSchemaName(pointer)
		assert.False(t, ok, "%q declares no named type", pointer)
	}
}

// TestForPointer_ChoosesTheNamespace pins the split ForPointer exists for: a
// component schema keeps its named ID, everything else is anonymous. The two
// namespaces are what stop a hoisted inline type colliding with a declared one.
func TestForPointer_ChoosesTheNamespace(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name    string
		pointer string
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
			assert.Equal(t, tc.want, ids.ForPointer(tc.pointer))
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
		"named":    string(ids.NamedType(pointer)),
		"anon":     string(ids.AnonType(pointer)),
		"composed": string(ids.ComposedType(pointer)),
		"op":       string(ids.Op(pointer)),
		"prop":     string(ids.Prop(pointer)),
		"auth":     string(ids.Auth("apiKey")),
		"service":  string(ids.Service(0)),
	}
	want := map[string]string{
		"named":    "t/openapi/components/schemas/User",
		"anon":     "t/anon/components/schemas/User",
		"composed": "t/composed/components/schemas/User",
		"op":       "op/openapi/components/schemas/User",
		"prop":     "p/openapi/components/schemas/User",
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

// TestDeclarationHint_PrefersTheComponentName pins why a hint is not taken from
// the use site: a $ref'd component lowers once, at its declaration, so a hint
// derived from whichever reference lowered first would name the one shared node
// arbitrarily.
func TestDeclarationHint_PrefersTheComponentName(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name     string
		pointer  string
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
