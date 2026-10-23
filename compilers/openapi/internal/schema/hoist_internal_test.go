package schema

import (
	"encoding/json/jsontext"
	"strconv"
	"strings"
	"testing"

	oas3 "github.com/speakeasy-api/openapi/jsonschema/oas3"
	"github.com/speakeasy-api/openapi/references"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dexpace/morphic/compilers/compile"
	"github.com/dexpace/morphic/compilers/openapi/internal/diag"
	"github.com/dexpace/morphic/compilers/openapi/internal/ids"
	"github.com/dexpace/morphic/compilers/openapi/internal/lowering"
	"github.com/dexpace/morphic/compilers/openapi/internal/openapitest"
	"github.com/dexpace/morphic/ir"
)

// TestCommonFor_NamesOnlyAComponent pins the split every hoisted node carries.
// A top-level component schema is named from the source; any deeper position is
// anonymous and carries only its context-derived hint. Getting this backwards
// gives an emitter a name to render where the source declared none, or drops the
// one it did declare.
func TestCommonFor_NamesOnlyAComponent(t *testing.T) {
	t.Parallel()
	c := lowering.Ctx{SrcIndex: 2}

	named := commonFor(c, "t/x", "/components/schemas/User", "ignored_hint")
	assert.False(t, named.Anonymous, "a component schema is a declaration")
	assert.Equal(t, "User", named.Name.Source)
	assert.Equal(t, "user", named.Name.Canonical, "the framework supplies the neutral words")
	assert.Empty(t, named.Name.Hint, "a named node needs no hint")

	anon := commonFor(c, "t/y", "/components/schemas/User/properties/id", "user_id")
	assert.True(t, anon.Anonymous, "an inline position declares no name")
	assert.Empty(t, anon.Name.Source)
	assert.Equal(t, "user_id", anon.Name.Hint, "only the hint survives")
}

// TestCommonFor_StampsTheDeclaringPointer pins that the node records where it
// was written, with the compile's own source index — the pair every consumer
// traces a type back through.
func TestCommonFor_StampsTheDeclaringPointer(t *testing.T) {
	t.Parallel()
	got := commonFor(lowering.Ctx{SrcIndex: 5}, "t/x", "/components/schemas/User", "")

	assert.Equal(t, ir.TypeID("t/x"), got.ID)
	assert.Equal(t, ir.Provenance{Source: 5, Pointer: "/components/schemas/User"}, got.Provenance)
}

// TestInternNode_DerivesTheIDAndInternsOnce pins the single hoisting entry
// point: the ID comes from the pointer, and a pointer visited twice yields the
// same node rather than a second one. Re-entering build is what a recursive
// schema would do forever.
func TestInternNode_DerivesTheIDAndInternsOnce(t *testing.T) {
	t.Parallel()
	c := lowering.Ctx{SrcIndex: 0}
	ts := compile.NewTypes()
	const at = "/components/schemas/User"

	var built int
	build := func(common ir.TypeCommon) ir.TypeDef {
		built++
		return &ir.Model{TypeCommon: common}
	}

	first := internNode(c, ts, at, "", build)
	second := internNode(c, ts, at, "", build)

	assert.Equal(t, ids.Document{}.ForPointer(at), first, "the ID is derived from the pointer")
	assert.Equal(t, first, second, "a second visit returns the interned ID")
	assert.Equal(t, 1, built, "and does not build the node again")

	td, ok := ts.Node(first)
	require.True(t, ok, "the node is registered under the ID it returned")
	assert.Equal(t, first, td.Common().ID, "and carries that same ID")
}

// TestRegisteredNode_ReportsAMissRatherThanDroppingIt pins the invariant check.
// Outside its build, every ID interning returns has a node, so a miss is a
// compiler bug no source can provoke — and the caller was about to attach docs
// or preserved constructs to that node, which would vanish without a word.
func TestRegisteredNode_ReportsAMissRatherThanDroppingIt(t *testing.T) {
	t.Parallel()
	c := lowering.Ctx{SrcIndex: 4}
	ts := compile.NewTypes()

	_, ok, diags := registeredNode(c, ts, "t/absent", "/components/schemas/Ghost")

	assert.False(t, ok)
	require.Len(t, diags, 1, "a miss is announced, not swallowed")
	assert.Equal(t, ir.SeverityError, diags[0].Severity)
	assert.Equal(t, diag.InternalInvariant, diags[0].Code)
	assert.Equal(t, ir.Provenance{Source: 4, Pointer: "/components/schemas/Ghost"}, diags[0].Provenance)

	id := internNode(c, ts, "/components/schemas/User", "", func(cm ir.TypeCommon) ir.TypeDef {
		return &ir.Model{TypeCommon: cm}
	})
	td, ok, diags := registeredNode(c, ts, id, "/components/schemas/User")
	assert.True(t, ok)
	assert.Empty(t, diags, "a hit reports nothing")
	assert.Equal(t, id, td.Common().ID)
}

// TestRegisteredNode_LeavesANodeStillBeingBuiltToItsBuilder pins the one miss
// that is no bug: a revisit while the node's build is still running further up
// the walk. That frame finishes the node once its build returns, so the
// revisit hands back nothing and reports nothing (GitHub #749).
func TestRegisteredNode_LeavesANodeStillBeingBuiltToItsBuilder(t *testing.T) {
	t.Parallel()
	c := lowering.Ctx{}
	ts := compile.NewTypes()
	const at = "/components/schemas/User"

	var midBuild bool
	var midBuildDiags []ir.Diagnostic
	id := internNode(c, ts, at, "", func(cm ir.TypeCommon) ir.TypeDef {
		_, midBuild, midBuildDiags = registeredNode(c, ts, cm.ID, at)
		return &ir.Model{TypeCommon: cm}
	})
	assert.False(t, midBuild, "there is no node to hand back yet")
	assert.Empty(t, midBuildDiags, "and that is not a broken invariant")

	_, ok, diags := registeredNode(c, ts, id, at)
	assert.True(t, ok, "once the build returns, the node is there")
	assert.Empty(t, diags)
}

// TestTargetHint_Shapes pins what a reference suggests from its text alone:
// each row is unresolved, so the walk reads the pointer, never the schema
// behind it. A fragment spelling a pointer is decoded at both layers (GitHub
// #505) and named by positionHint, so a branch or structural position is
// suggested as the node it holds (GitHub #521), whether at a component or
// elsewhere (GitHub #529). Any other fragment (non-UTF-8 included, GitHub
// #520) is named by the text after its last '/', percent-decoded when that is
// valid UTF-8, else as written.
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
		{name: "any other position outside components is named by positionHint's replay, not its last token",
			ref: "#/paths/~1x/get/responses/200/content/application~1json/schema/items", want: compile.SubHint("schema", "item")},
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
			assert.Equal(t, tc.want, targetHint(nil, oas3.NewJSONSchemaFromReference(references.Reference(tc.ref))))
		})
	}
}

// TestTargetHint_FollowsABranchToItsTarget pins where targetHint's walk goes on
// and where it stops, over the bindings a load gives. A reference to a
// composition branch holding a $ref (at, holding to) is named after that
// branch's target (GitHub #521), inside a component or not; a target that
// names nothing leaves the branch's own name. Any other position holding a
// $ref is named for the position, as positionHint replays it: a guess under
// /paths that the declaration replaces (GitHub #729).
func TestTargetHint_FollowsABranchToItsTarget(t *testing.T) {
	t.Parallel()
	const under = "/paths/~1x/get/responses/200/content/application~1json/schema"
	tests := []struct {
		name string
		at   string
		to   string
		want string
	}{
		{"a branch beneath a component", "/components/schemas/X/oneOf/0", "#/components/schemas/Y", "Y"},
		{"a branch elsewhere", under + "/oneOf/0", "#/components/schemas/Y", "Y"},
		{"a branch holding a reference to another document", "/components/schemas/X/oneOf/0",
			"./Fish%2DTank.yaml", "Fish-Tank.yaml"},
		{"a branch whose target names nothing", "/components/schemas/X/oneOf/0", "#/components/schemas/", "variant_0"},
		{"a branch whose other document names nothing", "/components/schemas/X/oneOf/0", "./schemas/", "variant_0"},
		{"any other position beneath a component", "/components/schemas/X/items", "#/components/schemas/Y",
			compile.SubHint("X", "item")},
		{"any other position elsewhere", under + "/items", "#/components/schemas/Y", compile.SubHint("schema", "item")},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			l, _ := loweredFor(t, hintSpec(tc.at, tc.to))
			u, ok := l.ctx.Doc.Components.GetSchemas().Get("U")
			require.True(t, ok)
			assert.Equal(t, tc.want, targetHint(l.ctx.Bindings(), u))
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

			choice, ok := doc.Types[ids.Document{}.ForPointer(ids.Ptr("components", "schemas", "Choice"))].(*ir.Union)
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
// position is named for where it is, whatever it holds: positionHint's name.
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
		{"any other position elsewhere, inline", inline, under + "/items", compile.SubHint("schema", "item")},
		{"any other position elsewhere, holding a $ref", ref("#/components/schemas/T"), under + "/items",
			compile.SubHint("schema", "item")},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, tc.want, subSchemaHint(nil, tc.decl, tc.pointer))
		})
	}
}

// TestPositionHint_BranchShapes pins which pointers name a composition branch.
//
// The walk decides whether hoistSubSchema answers what the composition would,
// so a pointer it misreads either reintroduces the disagreement (GitHub #181) or
// invents a variant hint for something that is not a branch. The rows that are
// not branches matter as much as the rows that are: `oneOf` is a legal property
// name, and a schema keyword taking numbered children is not necessarily a
// composition.
func TestPositionHint_BranchShapes(t *testing.T) {
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
		{name: "a branch outside components", pointer: "/paths/~1x/get/responses/200/content/application~1json/schema/oneOf/0", want: "variant_0"},
		{name: "a bare segment", pointer: "oneOf"},
		{name: "the empty pointer", pointer: ""},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			hint, branch := positionHint(tc.pointer)
			got := ""
			if branch {
				got = hint
			}
			assert.Equal(t, tc.want != "", branch, "pointer %q", tc.pointer)
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

		fromComposition := branchHint(nil, inline, i)
		fromPointer, branch := positionHint(pointer)
		require.True(t, branch, "the branch pointer is recognized as one")
		assert.Equal(t, fromComposition, fromPointer,
			"branch %d must be named the same whichever lowering interns it first", i)
		assert.Equal(t, fromComposition, subSchemaHint(nil, inline, pointer),
			"and subSchemaHint is the path that actually asks")
	}
}

// TestPositionHint_Shapes pins the left-to-right walk down a schema: each step
// recomposes the enclosing hint by role (items, additionalProperties,
// contentSchema, a patternProperties entry, a prefixItems slot, a composition
// branch) or, for a keyed map (properties, $defs, definitions,
// dependentSchemas, dependencies), takes the key itself. A key is never read as
// a keyword (GitHub #518), and a branch ordinal names "variant_0", never "0".
// Beneath /components/schemas the walk roots at the component; elsewhere at the
// pointer's first token, resetting at every token it does not know (GitHub
// #529).
func TestPositionHint_Shapes(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name    string
		pointer jsontext.Pointer
		want    string
		branch  bool
	}{
		{
			name: "a bare component", pointer: "/components/schemas/Foo",
			want: "Foo",
		},
		{
			name: "items under a component", pointer: "/components/schemas/Foo/items",
			want: compile.SubHint("Foo", "item"),
		},
		{
			name:    "nested roles",
			pointer: "/components/schemas/Foo/items/additionalProperties",
			want:    compile.SubHint(compile.SubHint("Foo", "item"), "value"),
		},
		{
			name:    "a property literally named items is not the items keyword",
			pointer: "/components/schemas/Foo/properties/items",
			want:    "items",
		},
		{
			name:    "a property named items holding an array names its own items by role",
			pointer: "/components/schemas/Foo/properties/items/items",
			want:    compile.SubHint("items", "item"),
		},
		{
			name:    "a property literally named properties holding items names its own items by role",
			pointer: "/components/schemas/Foo/properties/properties/items",
			want:    compile.SubHint("properties", "item"),
		},
		{
			name:    "additionalProperties holding an object whose own additionalProperties is a role",
			pointer: "/components/schemas/Foo/properties/additionalProperties/additionalProperties",
			want:    compile.SubHint("additionalProperties", "value"),
		},
		{
			name:    "a patternProperties entry whose pattern holds ~1",
			pointer: "/components/schemas/Foo/patternProperties/a~1b",
			want:    compile.SubHint("Foo", "pattern"),
		},
		{
			name:    "a patternProperties entry keyed items is not the items keyword",
			pointer: "/components/schemas/Foo/patternProperties/items",
			want:    compile.SubHint("Foo", "pattern"),
		},
		{
			name:    "an items entry inside a patternProperties entry keyed items",
			pointer: "/components/schemas/Foo/patternProperties/items/items",
			want:    compile.SubHint(compile.SubHint("Foo", "pattern"), "item"),
		},
		{
			name:    "a $defs entry keyed items is not the items keyword",
			pointer: "/components/schemas/Foo/$defs/items",
			want:    "items",
		},
		{
			name:    "a dependentSchemas entry keyed items is not the items keyword",
			pointer: "/components/schemas/Foo/dependentSchemas/items",
			want:    "items",
		},
		{
			name:    "a definitions entry keyed items is not the items keyword",
			pointer: "/components/schemas/Foo/definitions/items",
			want:    "items",
		},
		{
			name:    "a dependencies entry keyed items is not the items keyword",
			pointer: "/components/schemas/Foo/dependencies/items",
			want:    "items",
		},
		{
			name:    "a contentSchema",
			pointer: "/components/schemas/Foo/contentSchema",
			want:    compile.SubHint("Foo", "content"),
		},
		{
			name:    "a prefixItems slot",
			pointer: "/components/schemas/Foo/prefixItems/2",
			want:    compile.SubHint("Foo", "2"),
		},
		{
			name:    "a prefixItems member that is no slot is named after its own token",
			pointer: "/components/schemas/Foo/prefixItems/x",
			want:    "x",
		},
		{
			name:    "a component literally named items is named items",
			pointer: "/components/schemas/items",
			want:    "items",
		},
		{
			name:    "a oneOf branch is named variant_N, not its ordinal",
			pointer: "/components/schemas/Foo/oneOf/0",
			want:    "variant_0", branch: true,
		},
		{
			name:    "items beneath a oneOf branch is named off the branch's hint, not the ordinal",
			pointer: "/components/schemas/Foo/oneOf/0/items",
			want:    compile.SubHint("variant_0", "item"),
		},
		{
			name:    "a branch reached through a property named oneOf",
			pointer: "/components/schemas/Foo/properties/oneOf/anyOf/0",
			want:    "variant_0", branch: true,
		},
		{
			name:    "a property named oneOf with no index is not a branch",
			pointer: "/components/schemas/Foo/properties/oneOf",
			want:    "oneOf",
		},
		{
			name:    "a non-numeric child of a composition keyword is not a branch",
			pointer: "/components/schemas/Foo/oneOf/x",
			want:    "x",
		},
		{
			name:    "an unrecognised keyword is named after its own token, and what it holds hangs off that",
			pointer: "/components/schemas/Foo/not/items",
			want:    compile.SubHint("not", "item"),
		},
		{
			name:    "an extension keyword is treated the same as any other unrecognised token",
			pointer: "/components/schemas/Foo/x-ext/items",
			want:    compile.SubHint("x-ext", "item"),
		},
		{
			name:    "a keyed keyword with no key names itself",
			pointer: "/components/schemas/Foo/properties",
			want:    "properties",
		},
		{
			name:    "a position under /paths resets at every token the walk does not know, so items is named off the schema's own hint, not the response or media type that led there",
			pointer: "/paths/~1x/get/responses/200/content/application~1json/schema/items",
			want:    compile.SubHint("schema", "item"),
		},
		{
			name:    "a component that is not a schema replays the same way",
			pointer: "/components/responses/R/content/application~1json/schema/items",
			want:    compile.SubHint("schema", "item"),
		},
		{
			name:    `the component keyed ""`,
			pointer: "/components/schemas//items",
			want:    compile.SubHint("", "item"),
		},
		{
			name:    "the schemas map has too few tokens to root at a component, so it replays like any other pointer",
			pointer: "/components/schemas",
			want:    "schemas",
		},
		{name: "the empty pointer has no tokens to replay, so it is named \"\" and is never a branch", pointer: ""},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got, branch := positionHint(tc.pointer)
			assert.Equal(t, tc.want, got, "pointer %q", tc.pointer)
			assert.Equal(t, tc.branch, branch, "pointer %q", tc.pointer)
		})
	}
}

// hintSpec is a document whose schema U is a $ref to at, a composition branch
// or an items position, which holds a $ref to to; Y is a schema to name.
func hintSpec(at, to string) string {
	holder := "{oneOf: [{$ref: '" + to + "'}]}"
	if strings.HasSuffix(at, "/items") {
		holder = "{type: array, items: {$ref: '" + to + "'}}"
	}
	doc := "openapi: 3.1.0\ninfo: {title: t, version: '1'}\npaths:"
	x := ""
	if strings.HasPrefix(at, "/components/schemas/X") {
		doc += " {}\n"
		x = "    X: " + holder + "\n"
	} else {
		doc += "\n  /x: {get: {responses: {'200': {description: ok, content: {application/json: {schema: " + holder + "}}}}}}\n"
	}
	return doc + "components:\n  schemas:\n" + x + "    Y: {type: object}\n    U: {$ref: '#" + at + "'}\n"
}
