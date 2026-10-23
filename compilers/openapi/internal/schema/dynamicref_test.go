package schema_test

import (
	"encoding/json/jsontext"
	"fmt"
	"slices"
	"strings"
	"testing"

	"github.com/google/go-cmp/cmp"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dexpace/morphic/compilers/openapi/internal/diag"
	"github.com/dexpace/morphic/compilers/openapi/internal/ids"
	"github.com/dexpace/morphic/compilers/openapi/internal/openapitest"
	"github.com/dexpace/morphic/ir"
)

// TestDynamicRef_InAnotherDocumentsContentIsNotExpanded pins a $dynamicRef in
// content another document holds: its fragment names that document's
// $dynamicAnchor, which this compile does not index, so it is kept verbatim on
// the type lowered where that document writes it, rather than expanded to the
// source's anchor of the same name (GitHub #762).
func TestDynamicRef_InAnotherDocumentsContentIsNotExpanded(t *testing.T) {
	t.Parallel()
	doc, diags, _ := compileBeside(t, map[string]string{"ext.yaml": `paths:
  /x:
    get:
      responses:
        "200":
          description: ok
          content: {application/json: {schema: {$dynamicRef: '#node'}}}
components:
  schemas:
    Other: {$dynamicAnchor: node, type: object}
`}, `openapi: 3.1.0
info: {title: T, version: "1"}
paths:
  /p: {$ref: './ext.yaml#/paths/~1x'}
components:
  schemas:
    Node: {$dynamicAnchor: node, type: object}
`)

	const body = jsontext.Pointer("/paths/~1x/get/responses/200/content/application~1json/schema")
	assert.Empty(t, cmp.Diff([]ir.Diagnostic{dynamicRefNotExpanded(body + "/$dynamicRef")}, diags))
	assert.Equal(t, ids.In("ext.yaml").AnonType(body), onlyBodyTarget(t, doc),
		"lowered where ext.yaml writes it")
	held, ok := doc.Types[onlyBodyTarget(t, doc)].(*ir.Scalar)
	require.True(t, ok, "the position holds a node of its own for the reference it keeps")
	assert.Equal(t, ir.Provenance{Source: 1, Pointer: body}, held.Provenance)
	require.NotNil(t, held.Base)
	assert.Equal(t, ir.TypeID("t/prim/any"), held.Base.Target,
		"the source's anchor of the same name is not what the reference names")
	assert.Equal(t, ir.ReasonDegradedLowering, held.Unmodeled["openapi:$dynamicRef"].Reason)
	assert.Equal(t, ir.Provenance{Source: 1, Pointer: body + "/$dynamicRef"},
		held.Unmodeled["openapi:$dynamicRef"].Provenance)
}

// TestDynamicRef_InAnotherDocumentsContentAHoistReachesIsNotExpanded pins the
// same for a schema a $ref reaches: the source's Held names the schema of
// parameter P, a $ref into ext.yaml, so that schema is lowered where ext.yaml
// declares it, and its $dynamicRef is kept on the property, not expanded to
// the source's anchor of that name (GitHub #762).
func TestDynamicRef_InAnotherDocumentsContentAHoistReachesIsNotExpanded(t *testing.T) {
	t.Parallel()
	doc, diags, _ := compileBeside(t, map[string]string{"ext.yaml": `Param:
  name: q
  in: query
  schema: {type: object, properties: {inner: {$dynamicRef: '#node'}}}
`}, `openapi: 3.1.0
info: {title: T, version: "1"}
paths: {}
components:
  parameters:
    P: {$ref: './ext.yaml#/Param'}
  schemas:
    Node: {$dynamicAnchor: node, type: object}
    Held: {$ref: '#/components/parameters/P/schema'}
`)

	ext := ids.In("ext.yaml")
	inner := jsontext.Pointer("/Param/schema/properties/inner")
	assert.Empty(t, cmp.Diff([]ir.Diagnostic{dynamicRefNotExpanded(inner + "/$dynamicRef")}, diags))

	held, ok := doc.Types[ids.Document{}.NamedType("/components/schemas/Held")].(*ir.Scalar)
	require.True(t, ok)
	require.NotNil(t, held.Base)
	assert.Equal(t, ext.AnonType("/Param/schema"), held.Base.Target, "Held names the schema ext.yaml declares")
	model, ok := doc.Types[held.Base.Target].(*ir.Model)
	require.True(t, ok)
	assert.Equal(t, ir.Provenance{Source: 1, Pointer: "/Param/schema"}, model.Provenance)
	require.Len(t, model.Properties, 1)
	prop := model.Properties[0]
	assert.Equal(t, ext.Prop(inner), prop.ID)
	assert.Equal(t, ir.TypeID("t/prim/any"), prop.Type.Target)
	assert.Equal(t, ir.ReasonDegradedLowering, prop.Unmodeled["openapi:$dynamicRef"].Reason)
}

// TestDynamicRef_ExpandsAgainstTheOneMatchingAnchor pins the resolvable half of
// the $dynamicRef promise of docs/compilers/openapi.md#dynamic-ref, including
// the recursive shape that makes the keyword worth having.
func TestDynamicRef_ExpandsAgainstTheOneMatchingAnchor(t *testing.T) {
	t.Parallel()
	doc, diags := parseFull(t, openapitest.ComponentSpec(
		"    Meta: {$dynamicAnchor: meta, type: object, properties: {n: {type: string}}}\n"+
			"    Uses: {type: object, properties: {m: {$dynamicRef: '#meta'}}}\n"+
			"    Tree: {$dynamicAnchor: node, type: object, properties: {kids: {type: array, items: {$dynamicRef: '#node'}}}}\n"))
	openapitest.RequireNoErrorDiags(t, diags)

	assert.Equal(t, componentID("Meta"), propertyOf(t, doc, "Uses", "m").Type.Target,
		"the reference resolves to the anchor's own component, not to the top type")

	kids, ok := doc.Types[ir.TypeID("t/anon/components/schemas/Tree/properties/kids")].(*ir.List)
	require.True(t, ok)
	elem, ok := doc.Types[kids.Elem.Target].(*ir.Scalar)
	require.True(t, ok, "the items position keeps a node of its own")
	require.NotNil(t, elem.Base)
	assert.Equal(t, componentID("Tree"), elem.Base.Target,
		"a self-recursive dynamic reference terminates on the interned component ID")

	for _, td := range doc.Types {
		assert.NotContains(t, td.Common().Unmodeled, "openapi:$dynamicRef",
			"an expanded reference is the position's type; keeping it too would read as ignored")
	}

	// Expansion collapses an indirection the source left to evaluation, so it is
	// announced under its own code rather than sharing the composition one.
	assert.Equal(t, 2, openapitest.CountDiagsAt(diags, diag.DynamicRefExpanded, ir.SeverityInfo),
		"each expanded reference is announced once")
}

// TestDynamicRef_FragmentIsPercentDecoded pins that a $dynamicRef's fragment is
// read as the URI text it is. RFC 3986 §2.3 makes an unreserved character and
// its percent-encoded octet the same character, so `#my%2Danchor` names the
// anchor `my-anchor`; §2.1 makes the escape's hex digits case-insignificant. All
// three spellings below therefore address one declaration (GitHub #233).
func TestDynamicRef_FragmentIsPercentDecoded(t *testing.T) {
	t.Parallel()
	const anchor = "    B: {$dynamicAnchor: my-anchor, type: string}\n"
	cases := map[string]string{
		"an escaped hyphen is the hyphen":           "#my%2Danchor",
		"the escape's hex case is not significant":  "#my%2danchor",
		"the unescaped spelling names the same one": "#my-anchor",
	}
	for name, ref := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			doc, diags := parseFull(t, openapitest.ComponentSpec(anchor+"    A: {$dynamicRef: '"+ref+"'}\n"))
			openapitest.RequireNoErrorDiags(t, diags)

			sc, ok := doc.Types[componentID("A")].(*ir.Scalar)
			require.True(t, ok, "the reference position owns a node")
			require.NotNil(t, sc.Base)
			assert.Equal(t, componentID("B"), sc.Base.Target, "every spelling names the one anchor")
			assert.NotContains(t, sc.Unmodeled, "openapi:$dynamicRef",
				"an expanded reference is the position's type, so it is not also kept")
		})
	}
}

// TestDynamicRef_ReachesAnAnchorSpelledWithAPercent pins that the decode runs on
// the reference alone: an anchor carrying a literal `%` is addressed by escaping
// it as `%25`, and the anchor's own text is matched exactly as declared. The two
// sides mean different things — 2020-12 §8.2.2 makes `$dynamicAnchor` a plain
// name, §8.2.3.2 makes `$dynamicRef` a URI-reference — so only one is decoded.
//
// §8.2.2's production admits no `%` in an anchor name, so the document validator
// reports the declaration and this case arrives with an error diagnostic beside
// it, the same shape TestDynamicRef_NonScalarValueIsKeptNotExpanded documents.
// The lowering still has to agree with itself about what each side spells.
func TestDynamicRef_ReachesAnAnchorSpelledWithAPercent(t *testing.T) {
	t.Parallel()
	doc, _ := parseFull(t, openapitest.ComponentSpec(
		"    B: {$dynamicAnchor: 'pct%name', type: string}\n"+
			"    A: {$dynamicRef: '#pct%25name'}\n"))

	sc, ok := doc.Types[componentID("A")].(*ir.Scalar)
	require.True(t, ok, "the reference position owns a node")
	require.NotNil(t, sc.Base)
	assert.Equal(t, componentID("B"), sc.Base.Target,
		"an escaped percent addresses the percent itself, so the reference reaches the anchor as declared")
}

// TestDynamicRef_IrreducibleIsKeptAndSaysWhy pins the other half of that promise.
// Each case is a reference no static lowering can resolve, and each must survive
// verbatim with the reason naming which case it was.
func TestDynamicRef_IrreducibleIsKeptAndSaysWhy(t *testing.T) {
	t.Parallel()
	const anchors = "    M1: {$dynamicAnchor: dup, type: string}\n" +
		"    M2: {$dynamicAnchor: dup, type: integer}\n" +
		"    Deep: {type: object, properties: {i: {$dynamicAnchor: deep, type: string}}}\n"
	const anchor = "    M: {$dynamicAnchor: m, type: string}\n"
	// at is the node keeping the reference, defaulting to the component A itself;
	// invalid is the error the load phase reports of a reference that is no URI
	// reference, "" for none.
	cases := []struct {
		name, schemas, wantWhy, invalid string
		at                              ir.TypeID
	}{
		{name: "no such anchor", schemas: "    A: {$dynamicRef: '#nope'}\n",
			wantWhy: "no $dynamicAnchor \"nope\" is declared"},
		{name: "another document", schemas: "    A: {$dynamicRef: 'other.json#m'}\n",
			wantWhy: "is not a plain same-document fragment"},
		{name: "a pointer, not an anchor", schemas: "    A: {$dynamicRef: '#/components/schemas/M1'}\n",
			wantWhy: "not a plain same-document fragment"},
		{name: "an invalid percent escape", schemas: "    A: {$dynamicRef: '#my%zzanchor'}\n",
			wantWhy: `"#my%zzanchor" is not valid percent-encoded text: invalid URL escape "%zz"`,
			invalid: "openapi/validation/validation-invalid-format at /components/schemas/A/$dynamicRef"},
		{name: "an escaped percent decodes to the character", schemas: "    A: {$dynamicRef: '#pct%25name'}\n",
			wantWhy: `no $dynamicAnchor "pct%name" is declared`},
		// The anchor here is what a second decode would land on: it would read
		// this fragment as "#my-anchor" and expand. Being kept is the proof.
		{name: "the decode is not applied twice",
			schemas: "    H: {$dynamicAnchor: my-anchor, type: string}\n    A: {$dynamicRef: '#my%252Danchor'}\n",
			wantWhy: `no $dynamicAnchor "my%2Danchor" is declared`},
		{name: "declared twice", schemas: anchors + "    A: {$dynamicRef: '#dup'}\n",
			wantWhy: "declared 2 times"},
		{name: "not a component", schemas: anchors + "    A: {$dynamicRef: '#deep'}\n",
			wantWhy: "rather than on a component schema"},
		{name: "co-declared with a shape", schemas: anchors + "    A: {$dynamicRef: '#deep', type: string}\n",
			wantWhy: "co-declared with another applicator"},
		{name: "co-declared with items", schemas: anchor + "    A: {$dynamicRef: '#m', items: {type: integer}}\n",
			wantWhy: "co-declared with another applicator"},
		{name: "co-declared with prefixItems", schemas: anchor + "    A: {$dynamicRef: '#m', prefixItems: [{type: integer}]}\n",
			wantWhy: "co-declared with another applicator"},
		{name: "co-declared with required", schemas: anchor + "    A: {$dynamicRef: '#m', required: [p]}\n",
			wantWhy: "co-declared with another applicator"},
		{
			name:    "co-declared with a $ref",
			schemas: anchor + "    A: {$dynamicRef: '#m', $ref: '#/components/schemas/M'}\n",
			wantWhy: "co-declared with another applicator",
		},
		{
			name:    "co-declared with a union",
			schemas: anchor + "    A: {$dynamicRef: '#m', oneOf: [{type: string}, {type: integer}]}\n",
			wantWhy: "co-declared with another applicator",
		},
		{
			name: "the anchor is in a resource of its own",
			schemas: "    M: {$id: 'https://example.com/m', $dynamicAnchor: m, type: string}\n" +
				"    A: {$dynamicRef: '#m'}\n",
			wantWhy: `an $id at or above "/components/schemas/M"`,
		},
		{
			name:    "the reference is in a resource of its own",
			schemas: anchor + "    A: {$id: 'https://example.com/a', $dynamicRef: '#m'}\n",
			wantWhy: `an $id at or above "/components/schemas/A"`,
		},
		{
			// The pointer of a schema named "" ends in an empty reference token,
			// which the path walk once dropped as if it were the artifact of
			// splitting a pointer on '/' — reading the components/schemas map for
			// the $id instead of the schema itself, and expanding across a
			// boundary it should have stopped at.
			name:    `a schema named "" is still in a resource of its own`,
			schemas: anchor + "    \"\": {$id: 'https://example.com/empty', $dynamicRef: '#m'}\n",
			wantWhy: `an $id at or above "/components/schemas/"`,
			at:      "t/anon/components/schemas/",
		},
		{
			// The mirror of the case above, on the anchor's side. /components/
			// schemas/ addresses a component schema, so the refusal may not say it
			// does not — the reason is that an empty name earns no named TypeID,
			// which is the policy testdata/conformance/openapi/empty-names.yaml
			// records rather than a shape the compiler failed to recognise.
			name:    `an anchor on a schema named "" says why, not that it is no component`,
			schemas: "    \"\": {$dynamicAnchor: m, type: string}\n    A: {$dynamicRef: '#m'}\n",
			wantWhy: `is declared on the component schema keyed "" at "/components/schemas/", ` +
				`and an empty name earns no named type to expand to`,
		},
		{
			name: "an enclosing schema starts the resource",
			schemas: anchor +
				"    A: {$id: 'https://example.com/a', type: array, items: {$dynamicRef: '#m'}}\n",
			wantWhy: `an $id at or above "/components/schemas/A/items"`,
			at:      "t/anon/components/schemas/A/items",
		},
		{
			name: "expanding it would close a cycle",
			schemas: "    A: {$dynamicAnchor: a, $dynamicRef: '#b'}\n" +
				"    B: {$dynamicAnchor: b, $dynamicRef: '#a'}\n",
			wantWhy: `closes a cycle of $dynamicRef expansions back onto "/components/schemas/A"`,
		},
		{
			name:    "a self-reference is that cycle at length one",
			schemas: "    A: {$dynamicAnchor: a, $dynamicRef: '#a'}\n",
			wantWhy: `closes a cycle of $dynamicRef expansions back onto "/components/schemas/A"`,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			doc, diags := parseFull(t, openapitest.ComponentSpec(tc.schemas))
			var invalid []string
			for _, d := range diags {
				if d.Severity == ir.SeverityError {
					invalid = append(invalid, d.Code+" at "+string(d.Provenance.Pointer))
				}
			}
			assert.Equal(t, tc.invalid, strings.Join(invalid, "; "), "the errors: %+v", diags)

			at := tc.at
			if at == "" {
				at = componentID("A")
			}
			td, ok := doc.Types[at]
			require.True(t, ok, "the position owns a node to keep the reference on")
			entry, ok := td.Common().Unmodeled["openapi:$dynamicRef"]
			require.True(t, ok, "an irreducible $dynamicRef is kept verbatim, not dropped")
			assert.Equal(t, ir.ReasonDegradedLowering, entry.Reason)
			assertDiagContains(t, diags, string(entry.Provenance.Pointer), tc.wantWhy)
		})
	}
}

// TestDynamicRef_ResourceBoundaryVerdictIsOrderInvariant pins that which schema
// lowered first cannot decide whether the other sees the $id above it, nor
// whether the compiler reports stopping at its merge bound. Both come from a
// nodeview.View, whose merge-expansion memo records the depth an entry is good
// for (GitHub #402, #404).
//
// The order-invariance oracle cannot ask this: the construct needs YAML
// anchors, and reverseMappings declines to permute a document whose aliases it
// would lift above their anchors.
//
// It asserts the orders agree, not which verdict: a chain past the bound
// expands to nothing, so both currently miss the boundary (GitHub #401).
func TestDynamicRef_ResourceBoundaryVerdictIsOrderInvariant(t *testing.T) {
	t.Parallel()
	first, diags := parseFull(t, mergeBoundOrderSpec(true))
	openapitest.RequireNoErrorDiags(t, diags)
	last, diags := parseFull(t, mergeBoundOrderSpec(false))
	openapitest.RequireNoErrorDiags(t, diags)

	assert.Empty(t, cmp.Diff(first.Types, last.Types, orderInvariantIR()...),
		"the declaration order must not decide a resource-boundary verdict")

	// As a set: the per-schema diagnostics follow traversal order, which the
	// permutation changes by design, so what is compared is what was reported
	// and where, the way the harness's order-invariance oracle compares them.
	assert.Empty(t, cmp.Diff(diagnosticSet(first.Diagnostics), diagnosticSet(last.Diagnostics)),
		"the declaration order must not decide whether the merge bound is reported")
	assert.Contains(t, diagnosticSet(first.Diagnostics),
		"warning\x00"+diag.DegradedConstruct+"\x00", // a source-level finding: no pointer
		"the chain past the bound is reported in the as-written order, so the set is not trivially equal")
}

// TestDynamicRef_CycleIsRefusedAtEveryEdge pins that a cycle of $dynamicRef
// expansions is preserved whole rather than broken at whichever edge lowered
// first. Each member reaches the verdict from its own position, so no Scalar
// ends up with a Base chain that never terminates — the shape a loop of pure
// $refs lowers to the top type to avoid, and which neither pass.Validate nor
// irverify checks for aliases.
func TestDynamicRef_CycleIsRefusedAtEveryEdge(t *testing.T) {
	t.Parallel()
	doc, diags := parseFull(t, openapitest.ComponentSpec(
		"    A: {$dynamicAnchor: a, $dynamicRef: '#b'}\n"+
			"    B: {$dynamicAnchor: b, $dynamicRef: '#a'}\n"+
			"    Self: {$dynamicAnchor: s, $dynamicRef: '#s'}\n"))
	openapitest.RequireNoErrorDiags(t, diags)

	for _, name := range []string{"A", "B", "Self"} {
		sc, ok := doc.Types[componentID(name)].(*ir.Scalar)
		require.True(t, ok, "%s still owns a node", name)
		require.NotNil(t, sc.Base)
		assert.Equal(t, ir.TypeID("t/prim/any"), sc.Base.Target,
			"%s aliases the top type, never a link of the cycle", name)
		assert.Contains(t, sc.Unmodeled, "openapi:$dynamicRef",
			"%s keeps the reference it could not take", name)
	}
	assert.Equal(t, 0, openapitest.CountDiagsAt(diags, diag.DynamicRefExpanded, ir.SeverityInfo),
		"no edge of the cycle is reported as expanded")
}

// TestDynamicRef_ExpandsIntoACycleItIsNotPartOf pins the other side of that
// verdict: a position outside a cycle still expands onto it. Every member of the
// cycle refuses its own expansion, so the target is an ordinary alias over the
// top type rather than a link that loops.
func TestDynamicRef_ExpandsIntoACycleItIsNotPartOf(t *testing.T) {
	t.Parallel()
	doc, diags := parseFull(t, openapitest.ComponentSpec(
		"    A: {$dynamicAnchor: a, $dynamicRef: '#b'}\n"+
			"    B: {$dynamicAnchor: b, $dynamicRef: '#a'}\n"+
			"    Outside: {$dynamicRef: '#a'}\n"))
	openapitest.RequireNoErrorDiags(t, diags)

	sc, ok := doc.Types[componentID("Outside")].(*ir.Scalar)
	require.True(t, ok)
	require.NotNil(t, sc.Base)
	assert.Equal(t, componentID("A"), sc.Base.Target, "the expansion is taken")
	assert.NotContains(t, sc.Unmodeled, "openapi:$dynamicRef",
		"an expanded reference is the position's type, so it is not also kept")
}

// TestDynamicRef_ChainEndsAtAnAnchorItCannotFollow pins where the chain walk
// stops. A hop landing on an anchor that is not a component schema, or on a name
// declared more than once, cannot be followed any further — and a chain it
// cannot follow is not a chain that returns, so the expansion still stands.
func TestDynamicRef_ChainEndsAtAnAnchorItCannotFollow(t *testing.T) {
	t.Parallel()
	cases := map[string]string{
		"the hop lands off the components section": "    Mid: {$dynamicAnchor: mid, $dynamicRef: '#deep'}\n" +
			"    Holder: {type: object, properties: {i: {$dynamicAnchor: deep, type: string}}}\n",
		"the hop lands on a name declared twice": "    Mid: {$dynamicAnchor: mid, $dynamicRef: '#dup'}\n" +
			"    D1: {$dynamicAnchor: dup, type: string}\n    D2: {$dynamicAnchor: dup, type: integer}\n",
	}
	for name, schemas := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			doc, diags := parseFull(t, openapitest.ComponentSpec(schemas+"    A: {$dynamicRef: '#mid'}\n"))
			openapitest.RequireNoErrorDiags(t, diags)

			sc, ok := doc.Types[componentID("A")].(*ir.Scalar)
			require.True(t, ok)
			require.NotNil(t, sc.Base)
			assert.Equal(t, componentID("Mid"), sc.Base.Target)
		})
	}
}

// TestDynamicRef_NonScalarValueIsKeptNotExpanded covers the one irreducible case
// the sibling table cannot: a $dynamicRef whose value is not a string is a type
// error the document validator also reports, so the case arrives with an error
// diagnostic beside it. The lowering still has to keep the construct rather than
// treat a malformed reference as absent.
func TestDynamicRef_NonScalarValueIsKeptNotExpanded(t *testing.T) {
	t.Parallel()
	cases := map[string]string{
		"a sequence": "    A: {$dynamicRef: [not, a, string]}\n",
		"a mapping":  "    A: {$dynamicRef: {also: not}}\n",
	}
	for name, schemas := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			doc, diags := parseFull(t, openapitest.ComponentSpec(
				"    M1: {$dynamicAnchor: ok, type: string}\n"+schemas))

			td, ok := doc.Types[componentID("A")]
			require.True(t, ok, "the position owns a node to keep the reference on")
			entry, ok := td.Common().Unmodeled["openapi:$dynamicRef"]
			require.True(t, ok, "a malformed $dynamicRef is kept verbatim, not dropped")
			assert.Equal(t, ir.ReasonDegradedLowering, entry.Reason)
			assertDiagContains(t, diags, string(entry.Provenance.Pointer), "not a reference string")
		})
	}
}

// dynamicRefNotExpanded is the report of a $dynamicRef written in another
// document, at the pointer in that document's Document.Sources entry.
func dynamicRefNotExpanded(pointer jsontext.Pointer) ir.Diagnostic {
	return ir.Diagnostic{Severity: ir.SeverityInfo, Code: "openapi/degraded-construct",
		Message: "$dynamicRef was not expanded because it is written in another document, whose " +
			"$dynamicAnchor it names, and only the source's anchors are indexed; it is kept verbatim under Unmodeled",
		Provenance: ir.Provenance{Source: 1, Pointer: pointer}}
}

// mergeBoundOrderSpec writes one document whose resource boundary is reachable
// only through a YAML merge chain, with the two schemas that walk it declared in
// the given order. P's chain is 51 links and expands whole; Q's rides on P's tail
// for 71, past nodeview's MergeDepthLimit of 64.
func mergeBoundOrderSpec(pFirst bool) string {
	var b strings.Builder
	b.WriteString("openapi: 3.1.0\ninfo: {title: MergeBound, version: \"1\"}\npaths: {}\n")
	b.WriteString("x-c0: &c0 {$id: 'https://example.com/boundary'}\n")
	for i := 1; i <= 50; i++ {
		fmt.Fprintf(&b, "x-c%d: &c%d {<<: *c%d}\n", i, i, i-1)
	}
	b.WriteString("x-d0: &d0 {<<: *c50}\n")
	for i := 1; i <= 20; i++ {
		fmt.Fprintf(&b, "x-d%d: &d%d {<<: *d%d}\n", i, i, i-1)
	}
	b.WriteString("components:\n  schemas:\n    Node: {$dynamicAnchor: T, type: string}\n")
	const p = "    P:\n      <<: *c50\n      $dynamicRef: '#T'\n"
	const q = "    Q:\n      <<: *d20\n      $dynamicRef: '#T'\n"
	if pFirst {
		b.WriteString(p + q)
		return b.String()
	}
	b.WriteString(q + p)
	return b.String()
}

// diagnosticSet renders diagnostics as a sorted multiset of severity, code and
// pointer, which is how the harness's order-invariance oracle compares the
// diagnostics of two orders of one document.
func diagnosticSet(diags []ir.Diagnostic) []string {
	out := make([]string, 0, len(diags))
	for _, d := range diags {
		out = append(out, fmt.Sprintf("%s\x00%s\x00%s", d.Severity, d.Code, d.Provenance.Pointer))
	}
	slices.Sort(out)
	return out
}

// assertDiagContains requires one diagnostic at pointer whose message carries
// substr, so a case asserts the reason it was given and not merely that it was
// reported.
func assertDiagContains(t *testing.T, diags []ir.Diagnostic, pointer, substr string) {
	t.Helper()
	for _, d := range diags {
		if d.Provenance.Pointer == jsontext.Pointer(pointer) && strings.Contains(d.Message, substr) {
			return
		}
	}
	assert.Fail(t, "no diagnostic said why", "nothing at %q mentions %q; got %+v", pointer, substr, diags)
}
