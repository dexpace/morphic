package schema

import (
	"strconv"
	"testing"

	oas3 "github.com/speakeasy-api/openapi/jsonschema/oas3"
	soa "github.com/speakeasy-api/openapi/openapi"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	yaml "gopkg.in/yaml.v3"

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

func TestRefLastSegment(t *testing.T) {
	t.Parallel()
	assert.Equal(t, "Pet", refLastSegment("#/components/schemas/Pet"))
	assert.Equal(t, "bare", refLastSegment("bare"))
}

func TestMappingTargetID(t *testing.T) {
	t.Parallel()
	l := &lowerer{
		ctx: lowering.New(0, openapitest.DocDeclaring("Cat", "Dog", "A/B"), ir.SourceInfo{}, "", overlay.Origin{}),
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
	empty := lowering.New(0, openapitest.DocDeclaring(""), ir.SourceInfo{}, "", overlay.Origin{})
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
		pointer string
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
		pointer := "/components/schemas/Host/oneOf/" + strconv.Itoa(i)

		fromComposition := branchHint(inline, i)
		fromPointer, ok := branchPointerHint(pointer)
		require.True(t, ok, "the branch pointer is recognized as one")
		assert.Equal(t, fromComposition, fromPointer,
			"branch %d must be named the same whichever lowering interns it first", i)
		assert.Equal(t, fromComposition, subSchemaHint(inline, pointer),
			"and subSchemaHint is the path that actually asks")
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
	l.diags.AppendAll(LowerComponentSchemas(l.ctx, l.types, &l.anchors))

	got, ok := mappingTargetID(l.ctx, l.types, sub)
	require.True(t, ok, "the interned sub-schema resolves")
	assert.Equal(t, ids.ForPointer("/components/schemas/Pet/properties/kind"), got)
}
