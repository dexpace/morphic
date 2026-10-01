package schema

import (
	"encoding/json/jsontext"
	"strconv"
	"strings"
	"testing"

	oas3 "github.com/speakeasy-api/openapi/jsonschema/oas3"
	soa "github.com/speakeasy-api/openapi/openapi"
	"github.com/speakeasy-api/openapi/references"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	yaml "gopkg.in/yaml.v3"

	"github.com/dexpace/morphic/compilers/compile"
	"github.com/dexpace/morphic/compilers/openapi/internal/diag"
	"github.com/dexpace/morphic/compilers/openapi/internal/ids"
	"github.com/dexpace/morphic/compilers/openapi/internal/lowering"
	"github.com/dexpace/morphic/compilers/openapi/internal/openapitest"
	"github.com/dexpace/morphic/compilers/openapi/internal/overlay"
	"github.com/dexpace/morphic/ir"
)

func TestPropIDByName_NotFound(t *testing.T) {
	t.Parallel()
	m := &ir.Model{Properties: []ir.Property{{ID: "p1", Name: ir.Naming{Source: "a"}}}}
	_, ok := propIDByName(m, "missing")
	assert.False(t, ok)
	id, ok := propIDByName(m, "a")
	assert.True(t, ok)
	assert.Equal(t, ir.PropID("p1"), id)
}

// TestTargetHint_Shapes pins what a reference suggests from its text alone:
// each row is unresolved, so the walk reads the pointer, never the schema
// behind it. A fragment spelling a pointer is decoded at both layers (GitHub
// #505) and named by positionHint beneath /components/schemas, so a branch or
// structural position is suggested as the node it holds (GitHub #521);
// elsewhere by a branch's positional hint or the pointer's last token. Any
// other fragment (non-UTF-8 included, GitHub #520) is named by the text after
// its last '/', percent-decoded when that is valid UTF-8, else as written.
func TestTargetHint_Shapes(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		ref  string
		want string
	}{
		{name: "plain component", ref: "#/components/schemas/Pet", want: "Pet"},
		{name: "RFC 6901 escape decodes", ref: "#/components/schemas/Cat~1Dog", want: "Cat/Dog"},
		{name: "percent escape decodes", ref: "#/components/schemas/Fish%2DTank", want: "Fish-Tank"},
		{name: "another document, still a pointer", ref: "other.yaml#/components/schemas/Foo", want: "Foo"},
		{name: `a lone slash names the member keyed ""`, ref: "#/", want: ""},
		{name: "a oneOf branch is suggested as the variant, not its ordinal",
			ref: "#/components/schemas/X/oneOf/0", want: "variant_0"},
		{name: "a structural position is suggested by its role, not the keyword",
			ref: "#/components/schemas/A/items", want: compile.SubHint("A", "item")},
		{name: "a property whose key reads like a keyword is suggested by the key",
			ref: "#/components/schemas/A/properties/items", want: "items"},
		{name: "a branch outside components is suggested as the variant too",
			ref: "#/paths/~1x/get/responses/200/content/application~1json/schema/oneOf/0", want: "variant_0"},
		{name: "any other position outside components is suggested by its last token",
			ref: "#/paths/~1x/get/responses/200/content/application~1json/schema/items", want: "items"},
		{name: "no fragment at all", ref: "bare", want: "bare"},
		{name: "another document, no fragment", ref: "other.yaml", want: "other.yaml"},
		{name: "a document in a directory, no fragment", ref: "./schemas/Pet.yaml", want: "Pet.yaml"},
		{name: "a $anchor is not a pointer", ref: "#anchor", want: "#anchor"},
		{name: "a fragment that is not UTF-8 is not a pointer", ref: "#/components/schemas/%FF", want: "%FF"},
		{name: "a percent escape in a reference with no fragment decodes",
			ref: "./Fish%2DTank.yaml", want: "Fish-Tank.yaml"},
		{name: "an invalid percent escape is kept as written",
			ref: "./bad%ZZ.yaml", want: "bad%ZZ.yaml"},
		{name: "a percent escape decoding to non-UTF-8 bytes is kept as written",
			ref: "./x%FF.yaml", want: "x%FF.yaml"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, tc.want, targetHint(oas3.NewJSONSchemaFromReference(references.Reference(tc.ref))))
		})
	}
}

// TestTargetHint_FollowsABranchToItsTarget attaches resolution info with
// oas3.NewReferencedScheme instead of driving a compile, to pin where
// targetHint's walk goes on and where it stops. A reference to a composition
// branch holding a $ref (at, holding to) is named after that branch's target
// (GitHub #521), inside a component or not, and the walk ends at a component or
// document path; a target that names nothing leaves the branch's own name. Any
// other position holding a $ref is named for the position: as its declaration
// names it beneath a component, by the pointer's last token elsewhere
// (GitHub #729).
func TestTargetHint_FollowsABranchToItsTarget(t *testing.T) {
	t.Parallel()
	const under = "#/paths/~1x/get/responses/200/content/application~1json/schema"
	tests := []struct {
		name string
		at   string
		to   string
		want string
	}{
		{"a branch beneath a component", "#/components/schemas/X/oneOf/0", "#/components/schemas/Y", "Y"},
		{"a branch elsewhere", under + "/oneOf/0", "#/components/schemas/Y", "Y"},
		{"a branch holding a reference to another document", "#/components/schemas/X/oneOf/0",
			"./Fish%2DTank.yaml", "Fish-Tank.yaml"},
		{"a branch whose target names nothing", "#/components/schemas/X/oneOf/0", "#/components/schemas/", "variant_0"},
		{"a branch whose other document names nothing", "#/components/schemas/X/oneOf/0", "./schemas/", "variant_0"},
		{"any other position beneath a component", "#/components/schemas/X/items", "#/components/schemas/Y",
			compile.SubHint("X", "item")},
		{"any other position elsewhere", under + "/items", "#/components/schemas/Y", "items"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			to := references.Reference(tc.to)
			target := oas3.NewJSONSchemaFromSchema[oas3.Concrete](&oas3.Schema{Ref: &to})
			outer := oas3.NewReferencedScheme(t.Context(), references.Reference(tc.at), target)
			assert.Equal(t, tc.want, targetHint(outer))
		})
	}
}

// TestTargetHint_StopsAtTheHopCap pins maxTargetHintHops at its boundary,
// through a compile because only the resolver attaches a resolution to every
// branch of a long chain. Each branch holds a $ref to the next and the last to
// T. A variant naming the first branch is named after T while the walk reaches
// T within the cap, and keeps the last branch's name once it cannot. The node
// that variant holds starts its own walk a hop further on, so it still reaches
// T: past the cap the two are named apart, as the cap's doc says.
func TestTargetHint_StopsAtTheHopCap(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name     string
		branches int
		variant  string
	}{
		{"T within reach", maxTargetHintHops - 1, "t"},
		{"T one hop past the cap", maxTargetHintHops, "variant_0"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			var spec strings.Builder
			spec.WriteString("    Choice: {oneOf: [{$ref: '#/components/schemas/S0/oneOf/0'}, {type: integer}]}\n")
			for i := range tc.branches {
				next := "#/components/schemas/T"
				if i+1 < tc.branches {
					next = "#/components/schemas/S" + strconv.Itoa(i+1) + "/oneOf/0"
				}
				spec.WriteString("    S" + strconv.Itoa(i) + ": {oneOf: [{$ref: '" + next + "'}, {type: integer}]}\n")
			}
			spec.WriteString("    T: {type: object, properties: {t: {type: string}}}\n")
			doc, diags := lowerSpec(t, openapitest.ComponentSpec(spec.String()))
			openapitest.RequireNoErrorDiags(t, diags)

			choice, ok := doc.Types[ids.ForPointer(ids.Ptr("components", "schemas", "Choice"))].(*ir.Union)
			require.True(t, ok, "Choice should be a union")
			variant := choice.Variants[0]
			assert.Equal(t, tc.variant, variant.Name.Hint)
			node, ok := doc.Types[variant.Type.Target]
			require.True(t, ok, "the variant's target %s is interned", variant.Type.Target)
			assert.Equal(t, "t", node.Common().Name.Hint, "the node the variant holds reaches T either way")
		})
	}
}

// TestSubSchemaHint_Shapes pins the name subSchemaHint gives a position, one
// row per answer ownHint gives, each for a schema that is a $ref and for one
// that is not. A branch holding a $ref takes its target's name, falling back to
// its own when the target names nothing (an empty-named component); every other
// position is named for where it is, whatever it holds. Beneath
// /components/schemas that is positionHint's name; elsewhere it is a branch's
// ordinal or the pointer's last token.
//
// The $ref rows resolve nothing, so a target is named from its pointer alone.
func TestSubSchemaHint_Shapes(t *testing.T) {
	t.Parallel()
	const under = "/paths/~1x/get/responses/200/content/application~1json/schema"
	ref := func(to string) *oas3.JSONSchema[oas3.Referenceable] {
		return oas3.NewJSONSchemaFromReference(references.Reference(to))
	}
	inline := oas3.NewJSONSchemaFromSchema[oas3.Referenceable](&oas3.Schema{})
	tests := []struct {
		name    string
		decl    *oas3.JSONSchema[oas3.Referenceable]
		pointer jsontext.Pointer
		want    string
	}{
		{"a position beneath a component, inline", inline, "/components/schemas/A/items", compile.SubHint("A", "item")},
		{"a position beneath a component, holding a $ref", ref("#/components/schemas/T"),
			"/components/schemas/A/items", compile.SubHint("A", "item")},
		{"a branch beneath a component, inline", inline, "/components/schemas/A/oneOf/0", "variant_0"},
		{"a branch beneath a component, holding a $ref", ref("#/components/schemas/T"),
			"/components/schemas/A/oneOf/0", "T"},
		{"a branch holding a $ref to a target that names nothing", ref("#/components/schemas/"),
			"/components/schemas/A/oneOf/0", "variant_0"},
		{"a branch elsewhere, inline", inline, under + "/oneOf/0", "variant_0"},
		{"a branch elsewhere, holding a $ref", ref("#/components/schemas/T"), under + "/oneOf/0", "T"},
		{"any other position elsewhere, inline", inline, under + "/items", "items"},
		{"any other position elsewhere, holding a $ref", ref("#/components/schemas/T"), under + "/items", "items"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, tc.want, subSchemaHint(tc.decl, tc.pointer))
		})
	}
}

func TestMappingTargetID(t *testing.T) {
	t.Parallel()
	l := &lowerer{
		ctx: lowering.New(0, openapitest.DocDeclaring("Cat", "Dog", "A/B"), ir.SourceInfo{}, "", lowering.Limits{}, lowering.StreamingMedia{}, lowering.ExtensionPromotions{}, overlay.Origin{}),
		out: &ir.Document{Types: ir.TypeRegistry{}},
	}
	// A $ref to a declared component.
	id, ok := mappingTargetID(l.ctx, l.types, "#/components/schemas/Cat")
	require.True(t, ok)
	assert.Equal(t, ids.NamedType("/components/schemas/Cat"), id)
	// A bare schema name.
	id, ok = mappingTargetID(l.ctx, l.types, "Dog")
	require.True(t, ok)
	assert.Equal(t, ids.NamedType(ids.Ptr("components", "schemas", "Dog")), id)
	// A bare name that contains '/' but names an existing schema must resolve, not
	// dangle as a misclassified external $ref (issue #14, f07).
	id, ok = mappingTargetID(l.ctx, l.types, "A/B")
	require.True(t, ok)
	assert.Equal(t, ids.NamedType(ids.Ptr("components", "schemas", "A/B")), id)
	// An undeclared component and a genuine external ref are dropped, never
	// synthesized into a dangling ID.
	_, ok = mappingTargetID(l.ctx, l.types, "#/components/schemas/Ghost")
	assert.False(t, ok, "undeclared component target dropped")
	_, ok = mappingTargetID(l.ctx, l.types, "a.yaml#/A")
	assert.False(t, ok, "external target dropped")
	// A declared but empty-named component ("") is interned anonymously, so its
	// bare mapping name must resolve to that anon ID, not an unbacked ids.NamedType
	// (issue #14, f31). It gets a context of its own rather than being added to the
	// one above: the declared set is derived from the document now, so saying "and
	// also this one" means saying it to a document.
	empty := lowering.New(0, openapitest.DocDeclaring(""), ir.SourceInfo{}, "", lowering.Limits{}, lowering.StreamingMedia{}, lowering.ExtensionPromotions{}, overlay.Origin{})
	id, ok = mappingTargetID(empty, l.types, "")
	require.True(t, ok)
	assert.Equal(t, ids.AnonType(ids.Ptr("components", "schemas", "")), id)
	assert.NotEqual(t, ids.NamedType(ids.Ptr("components", "schemas", "")), id)
}

func TestDiscriminatorDefault_ResolvesDeclaredComponent(t *testing.T) {
	t.Parallel()
	l := newRawLowerer(openapitest.DocDeclaring("Cat"))
	d := &oas3.Discriminator{PropertyName: "kind", DefaultMapping: new("Cat")}

	id, diags := discriminatorDefault(l.ctx, l.types, d, "/components/schemas/Pet")
	assert.Equal(t, ids.NamedType("/components/schemas/Cat"), id)
	assert.Empty(t, diags, "a resolvable defaultMapping produces no diagnostic")
}

func TestDiscriminatorDefault_DroppedWhenUnresolved(t *testing.T) {
	t.Parallel()
	l := newRawLowerer(&soa.OpenAPI{})
	// "Missing" is neither a declared component nor an internal pointer, so the
	// defaultMapping does not resolve and is dropped with one error diagnostic.
	d := &oas3.Discriminator{PropertyName: "kind", DefaultMapping: new("Missing")}

	id, diags := discriminatorDefault(l.ctx, l.types, d, "/components/schemas/Pet")
	assert.Empty(t, id, "an unresolved defaultMapping yields no target")
	require.Len(t, diags, 1)
	assert.Equal(t, diag.UnresolvedRef, diags[0].Code)
}

func TestDiscriminatorDefault_EmptyIsNoOp(t *testing.T) {
	t.Parallel()
	l := newRawLowerer(&soa.OpenAPI{})
	id, diags := discriminatorDefault(l.ctx, l.types, &oas3.Discriminator{PropertyName: "kind"}, "/components/schemas/Pet")
	assert.Empty(t, id)
	assert.Empty(t, diags)
}

// TestRawMappingKeys_OnlyEnumeratesAMapping pins the shape guards on the branch
// residue's key reader. It is handed whatever the source wrote at a branch, so a
// sequence or a bare scalar reaches it as readily as a mapping, and the residue
// derivation depends on it reporting no keys rather than guessing at some.
//
// The alias and `<<` rows are the same guard read the other way: a node written
// by reference stands for keys, so answering "none" there would report a branch
// that declares plenty as one the merge consumed entirely.
func TestRawMappingKeys_OnlyEnumeratesAMapping(t *testing.T) {
	t.Parallel()
	var doc yaml.Node
	require.NoError(t, yaml.Unmarshal([]byte("a: 1\nb: 2\n"), &doc))
	require.Equal(t, yaml.DocumentNode, doc.Kind)

	cases := []struct {
		name string
		node *yaml.Node
		want []string
	}{
		{"a nil node has no keys", nil, nil},
		{"a sequence is not a mapping", openapitest.YAMLNode(t, "- a\n- b\n"), nil},
		{"a bare scalar is not a mapping", openapitest.YAMLNode(t, "plain"), nil},
		{"a mapping yields its keys in source order", openapitest.YAMLNode(t, "b: 1\na: 2\n"), []string{"b", "a"}},
		{"a document unwraps to the mapping inside it", &doc, []string{"a", "b"}},
		{"an alias yields the anchored mapping's keys",
			useValue(t, "anchor: &a {b: 1, c: 2}\nuse: *a\n"), []string{"b", "c"}},
		{"an alias to a scalar is still not a mapping",
			useValue(t, "anchor: &a plain\nuse: *a\n"), nil},
		{"a merge key yields the keys it merges in",
			useValue(t, "anchor: &a {b: 1, c: 2}\nuse: {<<: *a}\n"), []string{"b", "c"}},
		{"an explicit key beats the one it merges over",
			useValue(t, "anchor: &a {b: 1, c: 2}\nuse: {c: 9, <<: *a}\n"), []string{"c", "b"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, tc.want, rawMappingKeys(tc.node))
		})
	}
}

// TestEnumMemberForm_ClassifiesEveryValueKind pins the classification arm by arm,
// including the kinds OpenAPI cannot produce. ValueKind is IR-wide, and
// TestEnumMemberForm_NamesEveryValueKind requires every member of it to be named
// here — this is what says the naming is a decision rather than a formality.
func TestEnumMemberForm_ClassifiesEveryValueKind(t *testing.T) {
	t.Parallel()
	admissible := []struct {
		name string
		val  ir.Value
		prim ir.PrimKind
		text string
	}{
		{"string", ir.Value{Kind: ir.ValueString, Str: "s"}, ir.PrimString, "s"},
		{"symbol", ir.Value{Kind: ir.ValueSymbol, Str: "ok"}, ir.PrimString, "ok"},
		{"number", ir.Value{Kind: ir.ValueNumber, Num: ir.BigVal("1.5")}, ir.PrimNumber, "1.5"},
		{"bool", ir.Value{Kind: ir.ValueBool, Bool: true}, ir.PrimBool, "true"},
		{"bytes", ir.Value{Kind: ir.ValueBytes, Bytes: []byte("hi")}, ir.PrimBytes, "aGk="},
	}
	for _, tc := range admissible {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			prim, text, ok := enumMemberForm(tc.val)
			require.True(t, ok, "%s is admissible as an enum member", tc.name)
			assert.Equal(t, tc.prim, prim)
			assert.Equal(t, tc.text, text)
		})
	}

	refused := []ir.ValueKind{
		ir.ValueNull, ir.ValueList, ir.ValueObject, ir.ValueRefKind, ir.ValueCtor,
		ir.ValueKind("a kind ir does not declare"),
	}
	for _, kind := range refused {
		t.Run(string(kind), func(t *testing.T) {
			t.Parallel()
			prim, text, ok := enumMemberForm(ir.Value{Kind: kind})
			assert.False(t, ok, "%q has no place in an Enum", kind)
			assert.Empty(t, prim, "a refused kind asserts no primitive")
			assert.Empty(t, text)
		})
	}
}

// TestBranchPointerHint_Shapes pins which pointers name a composition branch.
//
// The parse decides whether hoistSubSchema answers what the composition would,
// so a pointer it misreads either reintroduces the disagreement (GitHub #181) or
// invents a variant hint for something that is not a branch. The rows that are
// not branches matter as much as the rows that are: `oneOf` is a legal property
// name, and a schema keyword taking numbered children is not necessarily a
// composition.
func TestBranchPointerHint_Shapes(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name    string
		pointer jsontext.Pointer
		want    string
	}{
		{name: "a oneOf branch", pointer: "/components/schemas/S/oneOf/0", want: "variant_0"},
		{name: "an anyOf branch", pointer: "/components/schemas/S/anyOf/12", want: "variant_12"},
		{name: "an allOf branch", pointer: "/components/schemas/S/allOf/1", want: "variant_1"},
		{
			name:    "a branch of a schema reached through a property named oneOf",
			pointer: "/components/schemas/S/properties/oneOf/anyOf/0", want: "variant_0",
		},

		{name: "prefixItems takes indices but composes nothing", pointer: "/components/schemas/S/prefixItems/0"},
		{name: "items is not indexed at all", pointer: "/components/schemas/S/items"},
		{name: "a property named oneOf is not a branch", pointer: "/components/schemas/S/properties/oneOf"},
		{name: "a non-numeric child of a composition keyword", pointer: "/components/schemas/S/oneOf/x"},
		{name: "a negative index is not a segment the compiler writes", pointer: "/components/schemas/S/oneOf/-1"},
		{name: "an empty index", pointer: "/components/schemas/S/oneOf/"},
		{name: "a bare segment", pointer: "oneOf"},
		{name: "the empty pointer", pointer: ""},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got, ok := branchPointerHint(tc.pointer)
			assert.Equal(t, tc.want != "", ok, "pointer %q", tc.pointer)
			assert.Equal(t, tc.want, got, "pointer %q", tc.pointer)
		})
	}
}

// TestBranchHint_AgreesWithThePointerWalk states the property the two
// derivations exist to share, at the functions rather than through a compile: for
// an inline branch, what the composition names the node and what a pointer walk
// names it are the same string. Whichever lowering arrives first, the node ends
// up with the same hint.
func TestBranchHint_AgreesWithThePointerWalk(t *testing.T) {
	t.Parallel()
	for i := range 3 {
		inline := oas3.NewJSONSchemaFromSchema[oas3.Referenceable](&oas3.Schema{})
		pointer := jsontext.Pointer("/components/schemas/Host/oneOf/" + strconv.Itoa(i))

		fromComposition := branchHint(inline, i)
		fromPointer, ok := branchPointerHint(pointer)
		require.True(t, ok, "the branch pointer is recognized as one")
		assert.Equal(t, fromComposition, fromPointer,
			"branch %d must be named the same whichever lowering interns it first", i)
		assert.Equal(t, fromComposition, subSchemaHint(inline, pointer),
			"and subSchemaHint is the path that actually asks")
	}
}

// TestPositionHint_Shapes pins the left-to-right walk from a component down:
// each step either recomposes the enclosing hint by role (items,
// additionalProperties, contentSchema, a patternProperties entry, a prefixItems
// slot, a composition branch) or, for a keyed map (properties, $defs,
// definitions, dependentSchemas, dependencies), takes the key itself. A key is
// never read as a keyword: a property spelled "items" is the key, and only an
// "items" met as a keyword token gets the role (GitHub #518). A branch ordinal
// names "variant_0", never "0". The walk answers only at or beneath a component
// schema; elsewhere it declines.
func TestPositionHint_Shapes(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name    string
		pointer jsontext.Pointer
		want    string
		branch  bool
		wantOK  bool
	}{
		{
			name: "a bare component", pointer: "/components/schemas/Foo",
			want: "Foo", wantOK: true,
		},
		{
			name: "items under a component", pointer: "/components/schemas/Foo/items",
			want: compile.SubHint("Foo", "item"), wantOK: true,
		},
		{
			name:    "nested roles",
			pointer: "/components/schemas/Foo/items/additionalProperties",
			want:    compile.SubHint(compile.SubHint("Foo", "item"), "value"), wantOK: true,
		},
		{
			name:    "a property literally named items is not the items keyword",
			pointer: "/components/schemas/Foo/properties/items",
			want:    "items", wantOK: true,
		},
		{
			name:    "a property named items holding an array names its own items by role",
			pointer: "/components/schemas/Foo/properties/items/items",
			want:    compile.SubHint("items", "item"), wantOK: true,
		},
		{
			name:    "a property literally named properties holding items names its own items by role",
			pointer: "/components/schemas/Foo/properties/properties/items",
			want:    compile.SubHint("properties", "item"), wantOK: true,
		},
		{
			name:    "additionalProperties holding an object whose own additionalProperties is a role",
			pointer: "/components/schemas/Foo/properties/additionalProperties/additionalProperties",
			want:    compile.SubHint("additionalProperties", "value"), wantOK: true,
		},
		{
			name:    "a patternProperties entry whose pattern holds ~1",
			pointer: "/components/schemas/Foo/patternProperties/a~1b",
			want:    compile.SubHint("Foo", "pattern"), wantOK: true,
		},
		{
			name:    "a patternProperties entry keyed items is not the items keyword",
			pointer: "/components/schemas/Foo/patternProperties/items",
			want:    compile.SubHint("Foo", "pattern"), wantOK: true,
		},
		{
			name:    "an items entry inside a patternProperties entry keyed items",
			pointer: "/components/schemas/Foo/patternProperties/items/items",
			want:    compile.SubHint(compile.SubHint("Foo", "pattern"), "item"), wantOK: true,
		},
		{
			name:    "a $defs entry keyed items is not the items keyword",
			pointer: "/components/schemas/Foo/$defs/items",
			want:    "items", wantOK: true,
		},
		{
			name:    "a dependentSchemas entry keyed items is not the items keyword",
			pointer: "/components/schemas/Foo/dependentSchemas/items",
			want:    "items", wantOK: true,
		},
		{
			name:    "a definitions entry keyed items is not the items keyword",
			pointer: "/components/schemas/Foo/definitions/items",
			want:    "items", wantOK: true,
		},
		{
			name:    "a dependencies entry keyed items is not the items keyword",
			pointer: "/components/schemas/Foo/dependencies/items",
			want:    "items", wantOK: true,
		},
		{
			name:    "a contentSchema",
			pointer: "/components/schemas/Foo/contentSchema",
			want:    compile.SubHint("Foo", "content"), wantOK: true,
		},
		{
			name:    "a prefixItems slot",
			pointer: "/components/schemas/Foo/prefixItems/2",
			want:    compile.SubHint("Foo", "2"), wantOK: true,
		},
		{
			name:    "a prefixItems member that is no slot is named after its own token",
			pointer: "/components/schemas/Foo/prefixItems/x",
			want:    "x", wantOK: true,
		},
		{
			name:    "a component literally named items is named items",
			pointer: "/components/schemas/items",
			want:    "items", wantOK: true,
		},
		{
			name:    "a oneOf branch is named variant_N, not its ordinal",
			pointer: "/components/schemas/Foo/oneOf/0",
			want:    "variant_0", branch: true, wantOK: true,
		},
		{
			name:    "items beneath a oneOf branch is named off the branch's hint, not the ordinal",
			pointer: "/components/schemas/Foo/oneOf/0/items",
			want:    compile.SubHint("variant_0", "item"), wantOK: true,
		},
		{
			name:    "a branch reached through a property named oneOf",
			pointer: "/components/schemas/Foo/properties/oneOf/anyOf/0",
			want:    "variant_0", branch: true, wantOK: true,
		},
		{
			name:    "a property named oneOf with no index is not a branch",
			pointer: "/components/schemas/Foo/properties/oneOf",
			want:    "oneOf", wantOK: true,
		},
		{
			name:    "a non-numeric child of a composition keyword is not a branch",
			pointer: "/components/schemas/Foo/oneOf/x",
			want:    "x", wantOK: true,
		},
		{
			name:    "an unrecognised keyword is named after its own token, and what it holds hangs off that",
			pointer: "/components/schemas/Foo/not/items",
			want:    compile.SubHint("not", "item"), wantOK: true,
		},
		{
			name:    "an extension keyword is treated the same as any other unrecognised token",
			pointer: "/components/schemas/Foo/x-ext/items",
			want:    compile.SubHint("x-ext", "item"), wantOK: true,
		},
		{
			name:    "a keyed keyword with no key names itself",
			pointer: "/components/schemas/Foo/properties",
			want:    "properties", wantOK: true,
		},
		{
			name:    "a position under /paths has no enclosing hint to recover",
			pointer: "/paths/~1x/get/responses/200/content/application~1json/schema/items",
		},
		{
			name:    "nor has one under a component that is not a schema",
			pointer: "/components/responses/R/content/application~1json/schema/items",
		},
		{
			name:    `the component keyed ""`,
			pointer: "/components/schemas//items",
			want:    compile.SubHint("", "item"), wantOK: true,
		},
		{name: "the schemas map itself names no component", pointer: "/components/schemas"},
		{name: "the empty pointer", pointer: ""},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got, branch, ok := positionHint(tc.pointer)
			assert.Equal(t, tc.wantOK, ok, "pointer %q", tc.pointer)
			assert.Equal(t, tc.want, got, "pointer %q", tc.pointer)
			assert.Equal(t, tc.branch, branch, "pointer %q", tc.pointer)
		})
	}
}

// TestMappingTargetID_FallsBackToAnInternedPointer pins the last of the three
// answers a mapping target can have. A bare declared name and a component $ref
// are decided without the registry; a $ref to anything deeper can only be
// answered by what is already interned at that pointer, and a target nothing
// backs must come back unresolved rather than mint an ID for a node that does
// not exist.
func TestMappingTargetID_FallsBackToAnInternedPointer(t *testing.T) {
	t.Parallel()
	const sub = "#/components/schemas/Pet/properties/kind"

	empty := newRawLowerer(openapitest.DocDeclaring("Pet"))
	_, ok := mappingTargetID(empty.ctx, empty.types, sub)
	assert.False(t, ok, "nothing is interned at that pointer, so the target does not resolve")

	// A nested object owns a node at its own pointer, where a scalar property
	// would reduce to the shared primitive and leave the pointer backing nothing.
	l, _ := loweredFor(t, openapitest.ComponentSpec("    Pet:\n      type: object\n"+
		"      properties: {kind: {type: object, properties: {a: {type: string}}}}\n"))
	l.diags.AppendAll(LowerComponentSchemas(t.Context(), l.ctx, l.types, &l.anchors))

	got, ok := mappingTargetID(l.ctx, l.types, sub)
	require.True(t, ok, "the interned sub-schema resolves")
	assert.Equal(t, ids.ForPointer("/components/schemas/Pet/properties/kind"), got)
}
