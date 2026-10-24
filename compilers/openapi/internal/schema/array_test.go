package schema_test

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dexpace/morphic/compilers/openapi/internal/diag"
	"github.com/dexpace/morphic/compilers/openapi/internal/openapitest"
	"github.com/dexpace/morphic/compilers/openapi/internal/schema"
	"github.com/dexpace/morphic/ir"
)

// TestLower_TupleRest pins what a prefixItems tuple admits past its elements and
// the bounds beside them. 2020-12 applies items to every element past
// prefixItems and reads an absent one as `true`, so only `items: false` closes
// the tuple. The rest is modeled, so nothing is kept beside it and nothing is
// reported as degraded; an unevaluatedItems stays the §4.7 keeper's whatever
// the rest is.
func TestLower_TupleRest(t *testing.T) {
	t.Parallel()
	const head = "      type: array\n      prefixItems: [{type: string}, {type: integer}]\n"
	i64 := func(v int64) *int64 { return &v }
	for _, tc := range []struct {
		name   string
		body   string
		rest   ir.TypeID // "" = no rest
		consts *ir.Constraints
		kept   []string
	}{
		{name: "absent items admits anything after", body: head, rest: "t/prim/any"},
		{name: "items true admits anything after", body: head + "      items: true\n", rest: "t/prim/any"},
		{name: "items false admits nothing after", body: head + "      items: false\n"},
		{name: "a typed rest", body: head + "      items: {type: boolean}\n", rest: "t/prim/bool"},
		{
			name: "a referenced rest",
			body: head + "      items: {$ref: '#/components/schemas/B'}\n    B: {type: object}\n",
			rest: componentID("B"),
		},
		{
			name: "an inline rest hoists at its own pointer",
			body: head + "      items: {type: object, properties: {f: {type: string}}}\n",
			rest: "t/anon/components/schemas/Tup/items",
		},
		{
			name:   "closed and pinned from below",
			body:   head + "      items: false\n      minItems: 2\n",
			consts: &ir.Constraints{MinItems: i64(2)},
		},
		{
			name:   "an upper bound leaves the rest open",
			body:   head + "      maxItems: 1\n",
			rest:   "t/prim/any",
			consts: &ir.Constraints{MaxItems: i64(1)},
		},
		{
			name:   "a lower bound past the elements requires a rest",
			body:   head + "      items: {type: boolean}\n      minItems: 3\n",
			rest:   "t/prim/bool",
			consts: &ir.Constraints{MinItems: i64(3)},
		},
		{
			name:   "every bound is kept as written, satisfiable or not",
			body:   head + "      items: false\n      minItems: 3\n      maxItems: 1\n      uniqueItems: true\n",
			consts: &ir.Constraints{MinItems: i64(3), MaxItems: i64(1), UniqueItems: true},
		},
		{
			name: "unevaluatedItems is kept and the rest read from items alone",
			body: head + "      unevaluatedItems: false\n",
			rest: "t/prim/any",
			kept: []string{"openapi:unevaluated"},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			doc, diags := schema.LowerSpec(t, openapitest.ComponentSpec("    Tup:\n"+tc.body))
			openapitest.RequireNoErrorDiags(t, diags)

			tup, ok := typeByName(doc, "Tup").(*ir.Tuple)
			require.True(t, ok, "prefixItems hoists a Tuple")
			assert.Equal(t, []ir.TypeRef{{Target: "t/prim/string"}, {Target: "t/prim/integer"}}, tup.Elems)
			if tc.rest == "" {
				assert.Nil(t, tup.Rest, "no element may follow the head")
			} else if assert.NotNil(t, tup.Rest) {
				assert.Equal(t, tc.rest, tup.Rest.Target)
				assert.Contains(t, doc.Types, tup.Rest.Target, "the rest is lowered into the registry")
			}
			assert.Equal(t, tc.consts, tup.Constraints, "the instance's bounds reach Tuple.Constraints")
			assert.Equal(t, tc.kept, unmodeledKeys(tup.Unmodeled))
			assert.Empty(t, diagsAtPointer(diags, diag.DegradedConstruct, "/components/schemas/Tup"),
				"a modeled rest is no degradation")
		})
	}
}

func TestLower_ListConstraints(t *testing.T) {
	t.Parallel()
	spec := openapitest.ComponentSpec(`    L:
      type: array
      items: {type: string}
      minItems: 1
      maxItems: 9
      uniqueItems: true
`)
	doc, diags := schema.LowerSpec(t, spec)
	openapitest.RequireNoErrorDiags(t, diags)
	l, ok := typeByName(doc, "L").(*ir.List)
	require.True(t, ok)
	require.NotNil(t, l.Constraints)
	assert.True(t, l.Constraints.UniqueItems)
	require.NotNil(t, l.Constraints.MinItems)
	assert.Equal(t, int64(1), *l.Constraints.MinItems)
}

// TestLower_ListWithoutItems pins that an array schema writing no items lowers
// to a List over the top type in every dialect. 3.0's requirement is reported
// (TestLower_MissingArrayItems) and never applied to the IR.
func TestLower_ListWithoutItems(t *testing.T) {
	t.Parallel()
	for _, version := range []string{"3.0.3", "3.1.0", "3.2.0"} {
		t.Run(version, func(t *testing.T) {
			t.Parallel()
			doc, diags := schema.LowerSpec(t, openapitest.ComponentSpecVer(version, "    L: {type: array}\n"))
			openapitest.RequireNoErrorDiags(t, diags)
			l, ok := typeByName(doc, "L").(*ir.List)
			require.True(t, ok)
			assert.Equal(t, ir.TypeID("t/prim/any"), l.Elem.Target)
		})
	}
}

// TestLower_MissingArrayItems pins which declarations the 3.0 rule reports and
// where. Every schema writing type array and no items is reported at its own
// pointer, whatever it lowers to; a written items, a type that is not array, a
// $ref site, a subschema kept verbatim and a 2020-12 document are not. Reports
// are counted as the lowering returns them, before compile.Diags drops
// repeats, and each row lowers each declaration once, so a doubled report
// reddens.
func TestLower_MissingArrayItems(t *testing.T) {
	t.Parallel()
	const holder = "    Holder:\n      type: object\n      properties:\n"
	cases := []struct {
		name    string
		version string
		schemas string
		want    []string // report pointers, in order; nil when silent
		// invalid is the error the load phase reports, the case writing what its
		// version does not allow; "" for none.
		invalid string
	}{
		{name: "a bare array", version: "3.0.3",
			schemas: "    L: {type: array}\n", want: []string{"/components/schemas/L"}},
		{name: "a nullable array", version: "3.0.3",
			schemas: "    L: {type: array, nullable: true}\n", want: []string{"/components/schemas/L"}},
		{name: "a property", version: "3.0.3",
			schemas: holder + "        tags: {type: array}\n", want: []string{"/components/schemas/Holder/properties/tags"}},
		{name: "an array as another's items", version: "3.0.3",
			schemas: "    L: {type: array, items: {type: array}}\n", want: []string{"/components/schemas/L/items"}},
		{name: "a type list holding array", version: "3.0.3",
			schemas: "    T: {type: [array, string]}\n", want: []string{"/components/schemas/T"},
			invalid: "openapi/validation/validation-type-mismatch"},
		{name: "a tuple", version: "3.0.3",
			schemas: "    L: {type: array, prefixItems: [{type: string}]}\n", want: []string{"/components/schemas/L"}},
		{name: "an enum of arrays", version: "3.0.3",
			schemas: "    L: {type: array, enum: [[1, 2], [3]]}\n", want: []string{"/components/schemas/L"}},
		{name: "an empty enum", version: "3.0.3",
			schemas: "    L: {type: array, enum: []}\n", want: []string{"/components/schemas/L"}},
		{name: "a const", version: "3.0.3",
			schemas: "    L: {type: array, const: [1]}\n", want: []string{"/components/schemas/L"}},
		{name: "an allOf beside it", version: "3.0.3",
			schemas: "    L: {type: array, allOf: [{minItems: 1}]}\n", want: []string{"/components/schemas/L"}},
		{name: "a oneOf beside it", version: "3.0.3",
			schemas: "    L: {type: array, oneOf: [{maxItems: 1}, {minItems: 3}]}\n", want: []string{"/components/schemas/L"}},
		{name: "a $ref to it from a sibling", version: "3.0.3",
			schemas: holder + "        l: {$ref: '#/components/schemas/L'}\n    L: {type: array}\n",
			want:    []string{"/components/schemas/L"}},
		{name: "items written empty is silent", version: "3.0.3",
			schemas: "    L: {type: array, items: {}}\n"},
		{name: "array keywords without a type are silent", version: "3.0.3",
			schemas: "    L: {minItems: 1}\n"},
		{name: "a $ref site's sibling type is silent", version: "3.0.3",
			schemas: "    L: {$ref: '#/components/schemas/S', type: array}\n    S: {type: string}\n"},
		{name: "an array kept verbatim under not is silent", version: "3.0.3",
			schemas: "    L: {type: integer, not: {type: array}}\n"},
		{name: "3.1 is silent", version: "3.1.0", schemas: "    L: {type: array}\n"},
		{name: "3.2 is silent", version: "3.2.0", schemas: "    L: {type: array}\n"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			l, loadDiags := schema.LoweredFor(t, openapitest.ComponentSpecVer(tc.version, tc.schemas))
			raw := schema.LowerComponentSchemas(t.Context(), l.Ctx, l.Types)
			openapitest.RequireNoErrorDiags(t, raw)
			var invalid []string
			for _, d := range loadDiags {
				if d.Severity == ir.SeverityError {
					invalid = append(invalid, d.Code)
				}
			}
			assert.Equal(t, tc.invalid, strings.Join(invalid, ", "), "the load phase's errors: %+v", loadDiags)
			assert.Equal(t, tc.want, missingItemsAt(t, raw))
		})
	}
}

// TestLower_MissingArrayItemsAtAReferencedPositionWarnsOnce pins that a $ref
// naming a coordinate inside another declaration (a property here; an `items`
// or a parameter's schema takes the same path) leaves lowering it to that
// declaration: the body is lowered once, by its owner, and warned of once, in
// either order, and promising it builds nothing more once every declaration
// has lowered (compile.Types.Finish).
func TestLower_MissingArrayItemsAtAReferencedPositionWarnsOnce(t *testing.T) {
	t.Parallel()
	const (
		ref    = "    A:\n      type: object\n      properties:\n        copy: {$ref: '#/components/schemas/Holder/properties/tags'}\n"
		holder = "    Holder:\n      type: object\n      properties:\n        tags: {type: array}\n"
		tags   = "/components/schemas/Holder/properties/tags"
	)
	for name, schemas := range map[string]string{"ref first": ref + holder, "declaration first": holder + ref} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			l, _ := schema.LoweredFor(t, openapitest.ComponentSpecVer("3.0.3", schemas))
			raw := schema.LowerComponentSchemas(t.Context(), l.Ctx, l.Types)
			require.Len(t, missingItemsAt(t, raw), 1, "the declaration lowers the property once")
			_, finished := l.Types.Finish(t.Context())
			raw = append(raw, finished...)
			require.Len(t, missingItemsAt(t, raw), 1, "and the reference's promise lowers it no more")

			l.Diags.AppendAll(raw)
			assert.Equal(t, []string{tags}, missingItemsAt(t, l.Diags.List()))
		})
	}
}

// TestCompile_MissingArrayItemsReachesTheDiagnostics drives the issue's exact
// document (GitHub #650) through the public compiler: the warning is raised by
// the schema lowering, and a caller only sees it if it survives the walk up to
// Compile's own diagnostics.
func TestCompile_MissingArrayItemsReachesTheDiagnostics(t *testing.T) {
	t.Parallel()
	doc, diags := parseFull(t, openapitest.ComponentSpecVer("3.0.3", "    NoItems: {type: array}\n"))
	assert.Equal(t, []string{"/components/schemas/NoItems"}, missingItemsAt(t, diags),
		"the warning reaches Compile's diagnostics, once")

	l, ok := doc.Types[componentID("NoItems")].(*ir.List)
	require.True(t, ok)
	assert.Equal(t, ir.TypeID("t/prim/any"), l.Elem.Target)
}

// missingItemsAt returns the pointer of each missing-array-items report in
// diags, in order, holding each to the warning severity the code carries.
func missingItemsAt(t *testing.T, diags []ir.Diagnostic) []string {
	t.Helper()
	var at []string
	for _, d := range diags {
		if d.Code != diag.MissingArrayItems {
			continue
		}
		assert.Equal(t, ir.SeverityWarning, d.Severity, "at %s", d.Provenance.Pointer)
		at = append(at, string(d.Provenance.Pointer))
	}
	return at
}
