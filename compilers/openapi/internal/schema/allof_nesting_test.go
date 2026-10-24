package schema_test

import (
	"encoding/json/jsontext"
	"maps"
	"slices"
	"strings"
	"testing"

	"github.com/google/go-cmp/cmp"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dexpace/morphic/compilers/openapi/internal/diag"
	"github.com/dexpace/morphic/compilers/openapi/internal/openapitest"
	"github.com/dexpace/morphic/ir"
	"github.com/dexpace/morphic/ir/irtest"
)

// TestAllOf_NestedLowersAsFlat holds the claim of the flattening itself: a
// nested spelling of a composition lowers as its flat one does. It compares the
// composed models with pointers, provenance and hoisted IDs projected away —
// base, mixins, properties in order with their type and required flag, the
// required names held for no property, openness, discriminator value, variants —
// and the codes reported. The per-case rows of
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
// tag, a false branch closes the model, and a required name the base declares
// is held in Requires. The residue and the false branch are also kept, which
// only a positive Kept shows.
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
	assert.Positive(t, closed.Kept, "the false branch is kept beside closing the model")

	required, _ := compileView(t, byName["a required name only the base declares"])
	assert.Equal(t, []string{"a"}, required.Requires, "the name is required of the composition, not of a property it holds")

	residue, _ := compileView(t, byName["a residue description"])
	assert.Positive(t, residue.Kept, "the residue is kept")

	subtype, _ := compileView(t, byName["a discriminated base inside a wrapper"])
	assert.Equal(t, "dog", subtype.DiscValue)
	assert.Equal(t, componentID("Pet"), ir.TypeID(subtype.Base), "the discriminated ref is the base")
}

// TestAllOf_NestedInlineWrapperFlattens pins the flattening of
// docs/compilers/openapi.md#allof-classification: a nested spelling classifies
// like the flat one, with every property keeping the pointer it was written at
// (GitHub #599).
func TestAllOf_NestedInlineWrapperFlattens(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name   string
		m      string
		base   ir.TypeID
		mixins []ir.TypeID
		props  map[string]string // wire name -> provenance pointer
	}{
		{
			name: "the issue: a wrapper around two refs",
			m: `    M:
      allOf:
        - allOf:
            - {$ref: '#/components/schemas/A'}
            - {$ref: '#/components/schemas/B'}
`,
			mixins: []ir.TypeID{componentID("A"), componentID("B")},
		},
		{
			name: "two levels of wrapper",
			m: `    M:
      allOf:
        - allOf:
            - allOf:
                - {$ref: '#/components/schemas/A'}
            - {$ref: '#/components/schemas/B'}
`,
			mixins: []ir.TypeID{componentID("A"), componentID("B")},
		},
		{
			name: "a ref with an inline branch beside it carries the nested pointer",
			m: `    M:
      allOf:
        - allOf:
            - {$ref: '#/components/schemas/A'}
            - type: object
              properties: {c: {type: string}}
`,
			base:  componentID("A"),
			props: map[string]string{"c": "/components/schemas/M/allOf/0/allOf/1/properties/c"},
		},
		{
			name: "a wrapper with its own properties and type object",
			m: `    M:
      allOf:
        - type: object
          properties: {w: {type: string}}
          allOf:
            - {$ref: '#/components/schemas/A'}
`,
			base:  componentID("A"),
			props: map[string]string{"w": "/components/schemas/M/allOf/0/properties/w"},
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			for _, doc := range nestedOrders(t, tc.m, nestedAB) {
				m, ok := typeByName(doc, "M").(*ir.Model)
				require.True(t, ok, "M should be a model")
				if tc.base == "" {
					assert.Nil(t, m.Base)
				} else if assert.NotNil(t, m.Base) {
					assert.Equal(t, tc.base, m.Base.Target)
				}
				assert.Equal(t, append([]ir.TypeID{}, tc.mixins...), refTargets(m.Mixins))
				assert.Empty(t, m.Unmodeled, "a flattened wrapper is not kept verbatim")
				assert.False(t, slices.ContainsFunc(doc.Diagnostics, func(d ir.Diagnostic) bool {
					return strings.HasPrefix(string(d.Provenance.Pointer), "/components/schemas/M")
				}), "flattening is exact, so nothing is reported under M: %+v", doc.Diagnostics)
				got := map[string]string{}
				for _, p := range m.Properties {
					got[p.WireName] = string(p.Provenance.Pointer)
				}
				want := tc.props
				if want == nil {
					want = map[string]string{}
				}
				assert.Equal(t, want, got)
			}
		})
	}
}

// TestAllOf_NestedWrapperRequiredAttaches pins that a required list inside a
// wrapper is read like one at the top: it attaches to a property a flattened
// branch declares, and a name none declares is listed in Model.Requires.
func TestAllOf_NestedWrapperRequiredAttaches(t *testing.T) {
	t.Parallel()
	t.Run("wrapper required and nested-nested required attach", func(t *testing.T) {
		t.Parallel()
		m := `    M:
      allOf:
        - required: [x]
          allOf:
            - type: object
              properties: {x: {type: integer}, y: {type: integer}}
            - allOf:
                - required: [y]
`
		for _, doc := range nestedOrders(t, m, nestedAB) {
			model, ok := typeByName(doc, "M").(*ir.Model)
			require.True(t, ok, "M should be a model")
			props := openapitest.PropsByWire(model.Properties)
			assert.True(t, props["x"].Required, "the wrapper's required reaches the property it names")
			assert.True(t, props["y"].Required, "so does a required two wrappers deep")
			assert.Empty(t, model.Requires, "every name found its property")
		}
	})
	t.Run("a name no own property declares is listed in Requires", func(t *testing.T) {
		t.Parallel()
		m := `    M:
      allOf:
        - required: [ghost]
          allOf:
            - {$ref: '#/components/schemas/A'}
`
		for _, doc := range nestedOrders(t, m, nestedAB) {
			model, ok := typeByName(doc, "M").(*ir.Model)
			require.True(t, ok, "M should be a model")
			assert.Equal(t, []string{"ghost"}, model.Requires, "the wrapper's required list is read")
		}
	})
}

// TestAllOf_NestedWrapperRequiredAttachesBesideUnion pins that a required two
// wrappers deep reaches its property in every distributed variant.
func TestAllOf_NestedWrapperRequiredAttachesBesideUnion(t *testing.T) {
	t.Parallel()
	u := `    U:
      oneOf: [{$ref: '#/components/schemas/P'}, {$ref: '#/components/schemas/Q'}]
      allOf:
        - allOf:
            - type: object
              properties: {x: {type: string}, y: {type: string}}
            - allOf:
                - required: [y]
`
	const pq = `    P: {type: object, properties: {p: {type: string}}}
    Q: {type: object, properties: {q: {type: string}}}
`
	for _, doc := range nestedOrders(t, u, nestedAB+pq) {
		for _, id := range []ir.TypeID{"t/composed/components/schemas/U/oneOf/0", "t/composed/components/schemas/U/oneOf/1"} {
			model, ok := doc.Types[id].(*ir.Model)
			require.True(t, ok, "%s should be a model", id)
			assert.True(t, openapitest.PropsByWire(model.Properties)["y"].Required, id)
		}
	}
}

// TestAllOf_NestedWrapperBaseSelection pins that the classification of
// docs/compilers/openapi.md#allof-classification runs over the flattened list:
// a discriminated ref inside a wrapper is the Base, whichever nested order it
// is written in, and still answers from its mapping.
func TestAllOf_NestedWrapperBaseSelection(t *testing.T) {
	t.Parallel()
	const pet = `    Pet:
      type: object
      required: [kind]
      properties: {kind: {type: string}}
      discriminator:
        propertyName: kind
        mapping: {dog: '#/components/schemas/Dog'}
    Named: {type: object, properties: {name: {type: string}}}
`
	tests := []struct {
		name string
		dog  string
	}{
		{"pet first inside the wrapper", `    Dog:
      allOf:
        - allOf:
            - {$ref: '#/components/schemas/Pet'}
            - {$ref: '#/components/schemas/Named'}
`},
		{"pet last inside the wrapper", `    Dog:
      allOf:
        - allOf:
            - {$ref: '#/components/schemas/Named'}
            - {$ref: '#/components/schemas/Pet'}
`},
		{"pet inside a wrapper beside a top-level ref", `    Dog:
      allOf:
        - {$ref: '#/components/schemas/Named'}
        - allOf:
            - {$ref: '#/components/schemas/Pet'}
`},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			for _, doc := range nestedOrders(t, tc.dog, pet) {
				dog, ok := typeByName(doc, "Dog").(*ir.Model)
				require.True(t, ok, "Dog should be a model")
				require.NotNil(t, dog.Base)
				assert.Equal(t, componentID("Pet"), dog.Base.Target)
				assert.Equal(t, []ir.TypeID{componentID("Named")}, refTargets(dog.Mixins))
				assert.Equal(t, "dog", dog.DiscriminatorValue, "the mapping key, not the schema name")
			}
		})
	}
}

// TestAllOf_NestedFalseBranchClosesModel pins that a `false` inside a wrapper
// still closes the composition, and is kept at the pointer it was written at.
func TestAllOf_NestedFalseBranchClosesModel(t *testing.T) {
	t.Parallel()
	m := `    M:
      allOf:
        - allOf: [false]
`
	for _, doc := range nestedOrders(t, m, nestedAB) {
		model, ok := typeByName(doc, "M").(*ir.Model)
		require.True(t, ok, "M should be a model")
		assert.Equal(t, ir.AdditionalClosed, model.Additional)
		entry, ok := model.Unmodeled["openapi:allOf/0/allOf/0"]
		require.True(t, ok, "the false is kept under its nested key; got %+v", model.Unmodeled)
		assert.Equal(t, "false", string(entry.Value))
		assert.Equal(t, jsontext.Pointer("/components/schemas/M/allOf/0/allOf/0"), entry.Provenance.Pointer)
		assert.NotContains(t, model.Unmodeled, "openapi:allOf/0", "the wrapper itself is not kept")
		assert.True(t, openapitest.HasDiagCodeAt(doc.Diagnostics, diag.FalseSchema,
			"/components/schemas/M/allOf/0/allOf/0"))
	}
}

// TestAllOf_NestedWrapperWithResidueKeptVerbatim pins that a wrapper writing
// a keyword the merge does not consume is still flattened: its entries and
// properties compose as the flat spelling's do, the whole wrapper is kept under
// its key, and the diagnostic names the extra keyword and not allOf.
func TestAllOf_NestedWrapperWithResidueKeptVerbatim(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name    string
		sibling string
		residue string
	}{
		{"description", "description: Doc", "description"},
		{"title", "title: Grouped", "title"},
		{"vendor extension", "x-vendor: keepme", "x-vendor"},
		{"readOnly", "readOnly: true", "readOnly"},
		{"discriminator", "discriminator: {propertyName: kind}", "discriminator"},
		{"oneOf", "oneOf: [{type: string}]", "oneOf"},
		{"additionalProperties false", "additionalProperties: false", "additionalProperties"},
		{"type set with null", `type: [object, "null"]`, "type"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			m := `    M:
      allOf:
        - allOf:
            - {$ref: '#/components/schemas/A'}
          ` + tc.sibling + `
          properties: {w: {type: string}}
`
			for _, doc := range nestedOrders(t, m, nestedAB) {
				model, ok := typeByName(doc, "M").(*ir.Model)
				require.True(t, ok, "M should be a model")
				require.NotNil(t, model.Base, "the wrapper's ref is classified as in the flat spelling")
				assert.Equal(t, componentID("A"), model.Base.Target)
				assert.Empty(t, model.Mixins)
				_, hasW := openapitest.PropsByWire(model.Properties)["w"]
				assert.True(t, hasW, "the wrapper's properties still merge")
				entry, ok := model.Unmodeled["openapi:allOf/0"]
				require.True(t, ok, "the wrapper is kept verbatim; got %+v", model.Unmodeled)
				assert.Equal(t, jsontext.Pointer("/components/schemas/M/allOf/0"), entry.Provenance.Pointer)
				assert.Contains(t, string(entry.Value), `"allOf"`, "the whole wrapper is kept")
				var infos int
				for _, d := range doc.Diagnostics {
					if d.Code == diag.DegradedConstruct && d.Provenance.Pointer == "/components/schemas/M/allOf/0" {
						infos++
					}
				}
				assert.Equal(t, 1, infos, "exactly one degraded-construct at the wrapper")
				msg := openapitest.DiagMessageAt(t, doc.Diagnostics, diag.DegradedConstruct,
					ir.SeverityInfo, "/components/schemas/M/allOf/0")
				assert.Contains(t, msg, "("+tc.residue+")")
				assert.Contains(t, msg, "nested allOf compose")
				assert.NotContains(t, msg, "(allOf")
			}
		})
	}
}

// TestAllOf_KeptEntryOfAKeptWrapperIsKeptTwice pins the trade-off
// docs/compilers/openapi.md#allof-inline-residue states: a wrapper kept for
// its residue is kept whole, its entries included, so an entry kept itself, or
// a false one, is in Unmodeled twice, inside the wrapper's copy and under its
// own path.
func TestAllOf_KeptEntryOfAKeptWrapperIsKeptTwice(t *testing.T) {
	t.Parallel()
	m := `    M:
      allOf:
        - description: grouped
          allOf:
            - {$ref: '#/components/schemas/A'}
            - {title: inner, properties: {p: {type: string}}}
            - false
`
	for _, doc := range nestedOrders(t, m, nestedAB) {
		model, ok := typeByName(doc, "M").(*ir.Model)
		require.True(t, ok, "M should be a model")
		assert.Equal(t, []string{"openapi:allOf/0", "openapi:allOf/0/allOf/1", "openapi:allOf/0/allOf/2"},
			slices.Sorted(maps.Keys(model.Unmodeled)), "the wrapper and each entry kept itself, each under its own path")
		wrapper := string(model.Unmodeled["openapi:allOf/0"].Value)
		assert.Contains(t, wrapper, `"title":"inner"`, "the wrapper's copy holds the kept entry")
		assert.Contains(t, wrapper, `,false]`, "and the false one")
		assert.Contains(t, string(model.Unmodeled["openapi:allOf/0/allOf/1"].Value), `"title":"inner"`)
		assert.Equal(t, "false", string(model.Unmodeled["openapi:allOf/0/allOf/2"].Value))
	}
}

// TestAllOf_NestedEmptyAllOfKeptVerbatim pins that a branch wrapping an empty
// allOf is not flattened: it is kept verbatim and reported, never dropped. The
// source breaks the schema's minItems, so the one error the load reports for it
// is expected and nothing else.
func TestAllOf_NestedEmptyAllOfKeptVerbatim(t *testing.T) {
	t.Parallel()
	m := `    M:
      allOf:
        - allOf: []
        - {$ref: '#/components/schemas/A'}
`
	docs := make([]*ir.Document, 0, 2)
	for _, src := range []string{m + nestedAB, nestedAB + m} {
		doc, diags := parseFull(t, openapitest.ComponentSpec(src))
		docs = append(docs, doc)
		errs := slices.DeleteFunc(slices.Clone(diags), func(d ir.Diagnostic) bool {
			return d.Severity != ir.SeverityError
		})
		require.Len(t, errs, 1, "only the minItems finding is an error")
		assert.Equal(t, jsontext.Pointer("/components/schemas/M/allOf/0/allOf"), errs[0].Provenance.Pointer)

		model, ok := typeByName(doc, "M").(*ir.Model)
		require.True(t, ok, "M should be a model")
		require.NotNil(t, model.Base, "the sibling $ref is the sole classified ref")
		assert.Equal(t, componentID("A"), model.Base.Target)
		assert.Empty(t, model.Mixins)
		entry, ok := model.Unmodeled["openapi:allOf/0"]
		require.True(t, ok, "the empty wrapper is kept verbatim; got %+v", model.Unmodeled)
		assert.JSONEq(t, `{"allOf":[]}`, string(entry.Value))
		assert.Contains(t,
			openapitest.DiagMessageAt(t, doc.Diagnostics, diag.DegradedConstruct,
				ir.SeverityInfo, "/components/schemas/M/allOf/0"),
			"only its properties and required list compose; the branch (allOf) is kept verbatim")
	}
	assert.Empty(t, cmp.Diff(docs[0].Types, docs[1].Types, orderInvariantIR()...),
		"declaration order must not change the types")
}

// TestAllOf_NestedInlineBranchKeyedByPointer pins that an inline branch inside
// a wrapper is kept under a key spelling its path, so it can collide with
// neither a sibling nor a top-level branch of the same index.
func TestAllOf_NestedInlineBranchKeyedByPointer(t *testing.T) {
	t.Parallel()
	m := `    M:
      allOf:
        - type: object
          properties: {a: {type: string}}
          description: Top
        - allOf:
            - type: object
              properties: {b: {type: string}}
              description: First
            - type: object
              properties: {c: {type: string}}
              minProperties: 1
`
	for _, doc := range nestedOrders(t, m, nestedAB) {
		model, ok := typeByName(doc, "M").(*ir.Model)
		require.True(t, ok, "M should be a model")
		require.Len(t, model.Unmodeled, 3, "got %+v", model.Unmodeled)
		for key, want := range map[string]string{
			"openapi:allOf/0":         `"description":"Top"`,
			"openapi:allOf/1/allOf/0": `"description":"First"`,
			"openapi:allOf/1/allOf/1": `"minProperties":1`,
		} {
			entry, ok := model.Unmodeled[key]
			require.True(t, ok, "missing %s in %+v", key, model.Unmodeled)
			assert.Contains(t, string(entry.Value), want)
			assert.Equal(t, "/components/schemas/M/"+strings.TrimPrefix(key, "openapi:"),
				string(entry.Provenance.Pointer))
			assert.Contains(t,
				openapitest.DiagMessageAt(t, doc.Diagnostics, diag.DegradedConstruct, ir.SeverityInfo,
					"/components/schemas/M/"+strings.TrimPrefix(key, "openapi:")), "inline allOf branch")
		}
	}
}

// TestAllOf_NestedBranchIsNamedByItsInnermostIndex pins that a $ref branch
// inside a wrapper is named by its innermost index, in either declaration
// order: an outside $ref to that pointer lowers it from the other side. The
// target is the empty-named component because a branch holding a $ref takes
// its target's name first, so only a target that names nothing lets the index
// reach the IR. The B before the wrapper puts the branch at flat position 2 but
// innermost index 1, so only the innermost index yields variant_1.
func TestAllOf_NestedBranchIsNamedByItsInnermostIndex(t *testing.T) {
	t.Parallel()
	m := `    M:
      allOf:
        - {$ref: '#/components/schemas/B'}
        - allOf:
            - {$ref: '#/components/schemas/A'}
            - {$ref: '#/components/schemas/', description: Narrowed}
`
	outsider := `    Outsider: {$ref: '#/components/schemas/M/allOf/1/allOf/1'}
`
	unnamed := "    \"\": {type: object, properties: {e: {type: string}}}\n"
	for _, doc := range nestedOrders(t, m, outsider+unnamed+nestedAB) {
		narrowed, ok := doc.Types["t/anon/components/schemas/M/allOf/1/allOf/1"]
		require.True(t, ok, "the narrowed branch is hoisted at its pointer")
		assert.Equal(t, "variant_1", narrowed.Common().Name.Hint)
	}
}

// compileView compiles the defs with the component m and returns M's view and
// the codes reported, requiring no error.
func compileView(t *testing.T, m string) (composedView, []string) {
	t.Helper()
	doc, diags := parseFull(t, openapitest.ComponentSpec(equivalenceDefs+m))
	openapitest.RequireNoErrorDiags(t, diags)
	return viewOf(doc, componentID("M"), 1), irtest.DiagCodes(diags)
}

// equivalencePairs are (nested, flat) spellings of one composition M. Each
// nested spelling must lower as its flat one does
// (docs/compilers/openapi.md#allof-classification), at any depth up to
// MaxAllOfFlattenDepth.
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
	{"a required name only the base declares", `    M:
      allOf:
        - allOf:
            - {$ref: '#/components/schemas/A'}
            - {required: [a]}
`, `    M:
      allOf: [{$ref: '#/components/schemas/A'}, {required: [a]}]
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

// wires lists the wire names of props.
func wires(props []propView) []string {
	out := make([]string, 0, len(props))
	for _, p := range props {
		out = append(out, p.Wire)
	}
	return out
}

// refTargets lists the TypeIDs refs target, in order.
func refTargets(refs []ir.TypeRef) []ir.TypeID {
	out := make([]ir.TypeID, 0, len(refs))
	for _, r := range refs {
		out = append(out, r.Target)
	}
	return out
}

// equivalenceDefs are the components every pair composes: two plain schemas, a
// third to compose beside them.
const equivalenceDefs = `    A: {type: object, properties: {a: {type: string}}}
    B: {type: object, properties: {b: {type: string}}}
    C: {type: object, properties: {c: {type: integer}}}
`

// pet is a discriminated base routing to M.
const pet = `    Pet:
      type: object
      required: [kind]
      properties: {kind: {type: string}}
      discriminator:
        propertyName: kind
        mapping: {dog: '#/components/schemas/M'}
`

// composedView is a composed model with what the nested and flat spellings may
// differ in projected away: pointers, provenance and the IDs of hoisted nodes.
// Kept counts the model's Unmodeled entries: their keys and values differ by
// spelling, but how many the composition kept must not.
type composedView struct {
	Base       string
	Mixins     []string
	Props      []propView
	Requires   []string
	Additional ir.AdditionalMode
	DiscValue  string
	Variants   []composedView
	Kept       int
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

// modelView projects a model's composition, properties, the required names it
// holds no property for, and its openness.
func modelView(doc *ir.Document, m *ir.Model) composedView {
	v := composedView{
		Requires: m.Requires, Additional: m.Additional, DiscValue: m.DiscriminatorValue, Kept: len(m.Unmodeled),
	}
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
