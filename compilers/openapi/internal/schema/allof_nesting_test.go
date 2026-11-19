// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

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

	residue, _ := compileView(t, byName["a residue"])
	assert.Positive(t, residue.Kept, "the residue is kept")

	closure, _ := compileView(t, byName["a closure"])
	assert.Equal(t, ir.AdditionalClosed, closure.Additional, "the wrapper declaring no member closes the model")
	assert.Zero(t, closure.Kept, "and nothing of it is kept")

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
				model, ok := typeByName(doc, "M").(*ir.Model)
				require.True(t, ok, "M should be a model")
				if tc.base == "" {
					assert.Nil(t, model.Base)
				} else if assert.NotNil(t, model.Base) {
					assert.Equal(t, tc.base, model.Base.Target)
				}
				assert.Equal(t, append([]ir.TypeID{}, tc.mixins...), refTargets(model.Mixins))
				assert.Empty(t, model.Unmodeled, "a flattened wrapper is not kept verbatim")
				assert.False(t, slices.ContainsFunc(doc.Diagnostics, func(diagnostic ir.Diagnostic) bool {
					return strings.HasPrefix(string(diagnostic.Provenance.Pointer), "/components/schemas/M")
				}), "flattening is exact, so nothing is reported under M: %+v", doc.Diagnostics)
				got := map[string]string{}
				for _, property := range model.Properties {
					got[property.WireName] = string(property.Provenance.Pointer)
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
		for _, id := range []ir.TypeID{"type/composed/components/schemas/U/oneOf/0", "type/composed/components/schemas/U/oneOf/1"} {
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
// properties compose as the flat spelling's do, and what it writes is kept as
// any entry's is, each keyword under its own key at the wrapper, or the whole
// wrapper under its key for an unevaluated keyword, reported once at the
// wrapper naming the extra keyword and not allOf, as the validation-only
// keyword the unevaluated one is.
func TestAllOf_NestedWrapperWithResidueKeptVerbatim(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name    string
		sibling string
		residue string
		whole   bool
	}{
		{"discriminator", "discriminator: {propertyName: kind}", "discriminator", false},
		{"oneOf", "oneOf: [{type: string}]", "oneOf", false},
		{"unevaluatedProperties false", "unevaluatedProperties: false", "unevaluatedProperties", true},
		{"type set with null", `type: [object, "null"]`, "type", false},
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
				code := diag.DegradedConstruct
				if tc.whole {
					code = diag.ValidationOnlyKeyword
				}
				var infos []ir.Diagnostic
				for _, diagnostic := range doc.Diagnostics {
					if diagnostic.Code == code && diagnostic.Provenance.Pointer == "/components/schemas/M/allOf/0" {
						infos = append(infos, diagnostic)
					}
				}
				require.Len(t, infos, 1, "exactly one %s at the wrapper", code)
				assert.NotContains(t, infos[0].Message, "(allOf")
				if !tc.whole {
					entry, ok := model.Unmodeled["openapi:"+tc.residue]
					require.True(t, ok, "the keyword is kept under its own key; got %+v", model.Unmodeled)
					assert.Equal(t, jsontext.Pointer("/components/schemas/M/allOf/0/"+tc.residue), entry.Provenance.Pointer)
					assert.NotContains(t, model.Unmodeled, "openapi:allOf/0")
					assert.Contains(t, infos[0].Message, "no home for "+tc.residue+" written on this inline branch")
					continue
				}
				entry, ok := model.Unmodeled["openapi:allOf/0"]
				require.True(t, ok, "the wrapper is kept verbatim; got %+v", model.Unmodeled)
				assert.Equal(t, jsontext.Pointer("/components/schemas/M/allOf/0"), entry.Provenance.Pointer)
				assert.Contains(t, string(entry.Value), `"allOf"`, "the whole wrapper is kept")
				assert.Contains(t, infos[0].Message, "("+tc.residue+")")
				assert.Contains(t, infos[0].Message, "nested allOf compose")
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
        - additionalProperties: false
          allOf:
            - {$ref: '#/components/schemas/A'}
            - {patternProperties: {'^i': {type: string}}, properties: {p: {type: string}}}
            - false
`
	for _, doc := range nestedOrders(t, m, nestedAB) {
		model, ok := typeByName(doc, "M").(*ir.Model)
		require.True(t, ok, "M should be a model")
		assert.Equal(t, []string{"openapi:allOf/0", "openapi:allOf/0/allOf/1", "openapi:allOf/0/allOf/2"},
			slices.Sorted(maps.Keys(model.Unmodeled)), "the wrapper and each entry kept itself, each under its own path")
		wrapper := string(model.Unmodeled["openapi:allOf/0"].Value)
		assert.Contains(t, wrapper, `"patternProperties":`, "the wrapper's copy holds the kept entry")
		assert.Contains(t, wrapper, `,false]`, "and the false one")
		assert.Contains(t, string(model.Unmodeled["openapi:allOf/0/allOf/1"].Value), `"patternProperties":`)
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
		errs := slices.DeleteFunc(slices.Clone(diags), func(diagnostic ir.Diagnostic) bool {
			return diagnostic.Severity != ir.SeverityError
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
			"nothing reads the rest of it; the branch (allOf) is kept verbatim")
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
          additionalProperties: false
        - allOf:
            - type: object
              properties: {b: {type: string}}
              patternProperties: {'^b': {type: string}}
            - type: object
              properties: {c: {type: string}}
              unevaluatedProperties: false
`
	for _, doc := range nestedOrders(t, m, nestedAB) {
		model, ok := typeByName(doc, "M").(*ir.Model)
		require.True(t, ok, "M should be a model")
		require.Len(t, model.Unmodeled, 3, "got %+v", model.Unmodeled)
		for key, want := range map[string][2]string{
			"openapi:allOf/0":         {`"additionalProperties":false`, diag.DegradedConstruct},
			"openapi:allOf/1/allOf/0": {`"patternProperties":`, diag.DegradedConstruct},
			"openapi:allOf/1/allOf/1": {`"unevaluatedProperties":false`, diag.ValidationOnlyKeyword},
		} {
			entry, ok := model.Unmodeled[key]
			require.True(t, ok, "missing %s in %+v", key, model.Unmodeled)
			assert.Contains(t, string(entry.Value), want[0])
			assert.Equal(t, "/components/schemas/M/"+strings.TrimPrefix(key, "openapi:"),
				string(entry.Provenance.Pointer))
			assert.Contains(t,
				openapitest.DiagMessageAt(t, doc.Diagnostics, want[1], ir.SeverityInfo,
					"/components/schemas/M/"+strings.TrimPrefix(key, "openapi:")), "inline allOf branch")
		}
	}
}

// TestAllOf_ABranchNamingNothingIsNamedForItsSchema pins the name of a $ref
// branch inside a wrapper whose target names nothing: the schema holding the
// list, in either declaration order, as an outside $ref to that pointer lowers
// it from the other side. Only a target that names nothing lets it through, since
// a branch holding a $ref takes its target's name first.
func TestAllOf_ABranchNamingNothingIsNamedForItsSchema(t *testing.T) {
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
		narrowed, ok := doc.Types["type/anonymous/components/schemas/M/allOf/1/allOf/1"]
		require.True(t, ok, "the narrowed branch is hoisted at its pointer")
		assert.Equal(t, "m", narrowed.Common().Name.Hint)
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
// statement.MaxFlattenDepth.
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
	{"a residue", `    M:
      allOf:
        - unevaluatedProperties: false
          allOf: [{$ref: '#/components/schemas/A'}, {$ref: '#/components/schemas/B'}]
`, `    M:
      allOf:
        - {$ref: '#/components/schemas/A'}
        - {$ref: '#/components/schemas/B'}
        - unevaluatedProperties: false
`},
	{"a closure", `    M:
      allOf:
        - additionalProperties: false
          allOf: [{$ref: '#/components/schemas/A'}, {$ref: '#/components/schemas/B'}]
`, `    M:
      allOf:
        - {$ref: '#/components/schemas/A'}
        - {$ref: '#/components/schemas/B'}
        - additionalProperties: false
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
	for _, propView := range props {
		out = append(out, propView.Wire)
	}
	return out
}

// refTargets lists the TypeIDs refs target, in order.
func refTargets(refs []ir.TypeRef) []ir.TypeID {
	out := make([]ir.TypeID, 0, len(refs))
	for _, typeRef := range refs {
		out = append(out, typeRef.Target)
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
	if strings.HasPrefix(string(id), "type/anonymous/") {
		return "anon:" + string(doc.Types[id].Kind())
	}
	return string(id)
}

// viewOf projects the type at id: a model as itself, a union as its variants,
// each projected in turn. depth bounds the descent, which a union's variants
// reach once.
func viewOf(doc *ir.Document, id ir.TypeID, depth int) composedView {
	var composedViewValue composedView
	switch td := doc.Types[id].(type) {
	case *ir.Model:
		composedViewValue = modelView(doc, td)
	case *ir.Union:
		if depth > 0 {
			for _, variant := range td.Variants {
				composedViewValue.Variants = append(composedViewValue.Variants, viewOf(doc, variant.Type.Target, depth-1))
			}
		}
	default:
		composedViewValue.Base = "not a model or union: " + string(id)
	}
	return composedViewValue
}

// modelView projects a model's composition, properties, the required names it
// holds no property for, and its openness.
func modelView(doc *ir.Document, model *ir.Model) composedView {
	composedViewValue := composedView{
		Requires: model.Requires, Additional: model.Additional, DiscValue: model.DiscriminatorValue, Kept: len(model.Unmodeled),
	}
	if model.Base != nil {
		composedViewValue.Base = string(model.Base.Target)
	}
	for _, mixin := range model.Mixins {
		composedViewValue.Mixins = append(composedViewValue.Mixins, string(mixin.Target))
	}
	for _, property := range model.Properties {
		composedViewValue.Props = append(composedViewValue.Props, propView{Wire: property.WireName, Type: projectTarget(doc, property.Type.Target), Required: property.Required})
	}
	return composedViewValue
}
