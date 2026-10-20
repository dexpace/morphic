package schema_test

import (
	"strings"
	"testing"

	"github.com/google/go-cmp/cmp"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dexpace/morphic/compilers/openapi/internal/openapitest"
	"github.com/dexpace/morphic/ir"
)

// equivalenceDefs are the components every pair composes: two plain schemas, a
// third to compose beside them.
const equivalenceDefs = `    A: {type: object, properties: {a: {type: string}}}
    B: {type: object, properties: {b: {type: string}}}
    C: {type: object, properties: {c: {type: integer}}}
`

// composedView is a composed model with what the nested and flat spellings may
// differ in projected away: pointers, provenance and the IDs of hoisted nodes.
type composedView struct {
	Base       string
	Mixins     []string
	Props      []propView
	Additional ir.AdditionalMode
	DiscValue  string
	Variants   []composedView
}

// propView is one property of a composedView.
type propView struct {
	Wire     string
	Type     string
	Required bool
}

// projectTarget names a type reference without the ID of a node hoisted at a
// pointer, which differs by spelling: a component is kept by ID, anything else by
// its kind.
func projectTarget(doc *ir.Document, id ir.TypeID) string {
	if strings.HasPrefix(string(id), "t/anon/") {
		return "anon:" + string(doc.Types[id].Kind())
	}
	return string(id)
}

// viewOf projects the type at id: a model as itself, a union as its variants,
// each projected in turn. depth bounds the descent, which a union's variants
// reach once.
func viewOf(doc *ir.Document, id ir.TypeID, depth int) composedView {
	var v composedView
	switch td := doc.Types[id].(type) {
	case *ir.Model:
		v = modelView(doc, td)
	case *ir.Union:
		if depth > 0 {
			for _, variant := range td.Variants {
				v.Variants = append(v.Variants, viewOf(doc, variant.Type.Target, depth-1))
			}
		}
	default:
		v.Base = "not a model or union: " + string(id)
	}
	return v
}

// modelView projects a model's composition, properties and openness.
func modelView(doc *ir.Document, m *ir.Model) composedView {
	v := composedView{Additional: m.Additional, DiscValue: m.DiscriminatorValue}
	if m.Base != nil {
		v.Base = string(m.Base.Target)
	}
	for _, mixin := range m.Mixins {
		v.Mixins = append(v.Mixins, string(mixin.Target))
	}
	for _, p := range m.Properties {
		v.Props = append(v.Props, propView{Wire: p.WireName, Type: projectTarget(doc, p.Type.Target), Required: p.Required})
	}
	return v
}

// diagCodes lists the codes doc's compilation reported, in order.
func diagCodes(diags []ir.Diagnostic) []string {
	out := make([]string, 0, len(diags))
	for _, d := range diags {
		out = append(out, d.Code)
	}
	return out
}

// compileView compiles the defs with the component m and returns M's view and
// the codes reported, requiring no error.
func compileView(t *testing.T, m string) (composedView, []string) {
	t.Helper()
	doc, diags := parseFull(t, openapitest.ComponentSpec(equivalenceDefs+m))
	openapitest.RequireNoErrorDiags(t, diags)
	return viewOf(doc, componentID("M"), 1), diagCodes(diags)
}

// pet is a discriminated base routing to M.
const pet = `    Pet:
      type: object
      required: [kind]
      properties: {kind: {type: string}}
      discriminator:
        propertyName: kind
        mapping: {dog: '#/components/schemas/M'}
`

// equivalencePairs are (nested, flat) spellings of one composition M. Each
// nested spelling must lower as its flat one does
// (docs/compilers/openapi.md#allof-classification), at any depth.
var equivalencePairs = []struct {
	name   string
	nested string
	flat   string
}{
	{"one wrapper around two refs", `    M:
      allOf:
        - allOf: [{$ref: '#/components/schemas/A'}, {$ref: '#/components/schemas/B'}]
`, `    M:
      allOf: [{$ref: '#/components/schemas/A'}, {$ref: '#/components/schemas/B'}]
`},
	{"two wrappers", `    M:
      allOf:
        - allOf:
            - allOf: [{$ref: '#/components/schemas/A'}]
            - {$ref: '#/components/schemas/B'}
`, `    M:
      allOf: [{$ref: '#/components/schemas/A'}, {$ref: '#/components/schemas/B'}]
`},
	{"three wrappers", `    M:
      allOf:
        - allOf:
            - {$ref: '#/components/schemas/A'}
            - allOf:
                - allOf: [{$ref: '#/components/schemas/B'}, {$ref: '#/components/schemas/C'}]
`, `    M:
      allOf: [{$ref: '#/components/schemas/A'}, {$ref: '#/components/schemas/B'}, {$ref: '#/components/schemas/C'}]
`},
	{"four wrappers", `    M:
      allOf:
        - allOf:
            - allOf:
                - allOf:
                    - allOf: [{$ref: '#/components/schemas/A'}, {$ref: '#/components/schemas/B'}]
`, `    M:
      allOf: [{$ref: '#/components/schemas/A'}, {$ref: '#/components/schemas/B'}]
`},
	{"a discriminated base inside a wrapper", pet + `    M:
      allOf:
        - allOf:
            - {$ref: '#/components/schemas/A'}
            - {$ref: '#/components/schemas/Pet'}
`, pet + `    M:
      allOf: [{$ref: '#/components/schemas/A'}, {$ref: '#/components/schemas/Pet'}]
`},
	{"inline branches with properties, required and type object", `    M:
      allOf:
        - allOf:
            - {$ref: '#/components/schemas/A'}
            - type: object
              required: [x]
              properties: {x: {type: integer}, y: {type: string}}
            - allOf:
                - properties: {z: {type: boolean}}
`, `    M:
      allOf:
        - {$ref: '#/components/schemas/A'}
        - type: object
          required: [x]
          properties: {x: {type: integer}, y: {type: string}}
        - properties: {z: {type: boolean}}
`},
	{"a wrapper's own properties merge after its entries", `    M:
      allOf:
        - required: [w]
          properties: {w: {type: string}}
          allOf:
            - type: object
              properties: {x: {type: integer}}
            - properties: {y: {type: integer}}
`, `    M:
      allOf:
        - type: object
          properties: {x: {type: integer}}
        - properties: {y: {type: integer}}
        - required: [w]
          properties: {w: {type: string}}
`},
	{"a true branch", `    M:
      allOf:
        - allOf: [{$ref: '#/components/schemas/A'}, true]
`, `    M:
      allOf: [{$ref: '#/components/schemas/A'}, true]
`},
	{"a false branch", `    M:
      allOf:
        - allOf: [{$ref: '#/components/schemas/A'}, false]
`, `    M:
      allOf: [{$ref: '#/components/schemas/A'}, false]
`},
	{"a residue description", `    M:
      allOf:
        - description: grouped
          allOf: [{$ref: '#/components/schemas/A'}, {$ref: '#/components/schemas/B'}]
`, `    M:
      allOf:
        - {$ref: '#/components/schemas/A'}
        - {$ref: '#/components/schemas/B'}
        - description: grouped
`},
	{"a distributed oneOf beside the wrappers", `    M:
      type: object
      allOf:
        - allOf:
            - {$ref: '#/components/schemas/C'}
            - properties: {w: {type: string}}
      oneOf: [{$ref: '#/components/schemas/A'}, {$ref: '#/components/schemas/B'}]
`, `    M:
      type: object
      allOf:
        - {$ref: '#/components/schemas/C'}
        - properties: {w: {type: string}}
      oneOf: [{$ref: '#/components/schemas/A'}, {$ref: '#/components/schemas/B'}]
`},
}

// TestAllOf_NestedLowersAsFlat holds the claim of the flattening itself: a
// nested spelling of a composition lowers as its flat one does. It compares the
// composed models with pointers, provenance and hoisted IDs projected away —
// base, mixins, properties in order with their type and required flag, openness,
// discriminator value, variants — and the codes reported. The per-case rows of
// TestAllOf_NestedInlineWrapperFlattens each pin one consequence; this holds the
// property for every reader of the list.
func TestAllOf_NestedLowersAsFlat(t *testing.T) {
	t.Parallel()
	for _, tc := range equivalencePairs {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			nested, nestedCodes := compileView(t, tc.nested)
			flat, flatCodes := compileView(t, tc.flat)
			assert.NotEqual(t, composedView{}, flat, "the flat spelling lowers to something to compare")
			assert.Empty(t, cmp.Diff(flat, nested), "the nested spelling composes as the flat one does")
			assert.Equal(t, flatCodes, nestedCodes, "and reports the same codes")
		})
	}
}

// TestAllOf_NestedLowersAsFlat_ReachesWhatItCompares pins that the projection
// sees the distinctions the pairs rely on, so a pair that agrees does so for a
// reason: a distributed union has variants, the discriminated subtype has its
// tag, and a false branch closes the model.
func TestAllOf_NestedLowersAsFlat_ReachesWhatItCompares(t *testing.T) {
	t.Parallel()
	byName := map[string]string{}
	for _, tc := range equivalencePairs {
		byName[tc.name] = tc.flat
	}
	distributed, _ := compileView(t, byName["a distributed oneOf beside the wrappers"])
	require.Len(t, distributed.Variants, 2, "the oneOf distributes")
	assert.Equal(t, []string{"w"}, wires(distributed.Variants[0].Props), "each variant carries the body's own property")
	assert.Equal(t, string(componentID("C")), distributed.Variants[0].Base, "and the ref the wrapper held as its base")

	closed, _ := compileView(t, byName["a false branch"])
	assert.Equal(t, ir.AdditionalClosed, closed.Additional)

	subtype, _ := compileView(t, byName["a discriminated base inside a wrapper"])
	assert.Equal(t, "dog", subtype.DiscValue)
	assert.Equal(t, componentID("Pet"), ir.TypeID(subtype.Base), "the discriminated ref is the base")
}

// wires lists the wire names of props.
func wires(props []propView) []string {
	out := make([]string, 0, len(props))
	for _, p := range props {
		out = append(out, p.Wire)
	}
	return out
}
