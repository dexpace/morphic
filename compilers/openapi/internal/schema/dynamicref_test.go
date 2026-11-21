// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

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

	"github.com/dexpace/morphic/compilers/openapi"
	"github.com/dexpace/morphic/compilers/openapi/internal/diag"
	"github.com/dexpace/morphic/compilers/openapi/internal/ids"
	"github.com/dexpace/morphic/compilers/openapi/internal/openapitest"
	"github.com/dexpace/morphic/ir"
	"github.com/dexpace/morphic/ir/irtest"
)

// TestDynamicRef_ExpandsAgainstTheDocumentItIsWrittenIn pins a $dynamicRef in
// content another document holds: its fragment names that document's
// $dynamicAnchor, so it expands there, to the type lowered where that document
// declares it, never to the source's anchor of the same name (GitHub #762).
func TestDynamicRef_ExpandsAgainstTheDocumentItIsWrittenIn(t *testing.T) {
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
	other := ids.In("ext.yaml").ForPointer("/components/schemas/Other")
	require.Len(t, diags, 1)
	assert.Equal(t, "openapi/dynamic-ref-expanded", diags[0].Code)
	assert.Equal(t, ir.Provenance{Source: 1, Pointer: body + "/$dynamicRef"}, diags[0].Provenance)
	held, ok := doc.Types[onlyBodyTarget(t, doc)].(*ir.Scalar)
	require.True(t, ok, "the position holds an alias, as it holds one over the source's own")
	require.NotNil(t, held.Base)
	assert.Equal(t, other, held.Base.Target, "the source's anchor of that name is not what the reference names")
	assert.Empty(t, held.Unmodeled, "an expanded reference is not kept as well")
	target, ok := doc.Types[other].(*ir.Model)
	require.True(t, ok)
	assert.Equal(t, ir.Provenance{Source: 1, Pointer: "/components/schemas/Other"}, target.Provenance)
}

// TestDynamicRef_NamesNoAnchorOfTheSourceFromAnotherDocument pins the other side
// for a schema a $ref reaches: the source's Held names the schema of parameter
// P, a $ref into ext.yaml, whose document declares no such anchor, so its
// $dynamicRef is the $ref it spells there, which resolves nowhere, and is
// never expanded to the source's anchor of that name (GitHub #762).
func TestDynamicRef_NamesNoAnchorOfTheSourceFromAnotherDocument(t *testing.T) {
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
	require.Len(t, diags, 1, "%+v", diags)
	assert.Equal(t, diag.UnresolvedRef, diags[0].Code)
	assert.Equal(t, ir.Provenance{Source: 1, Pointer: inner}, diags[0].Provenance, "placed in ext.yaml")
	assert.True(t, strings.HasPrefix(diags[0].Message, `unresolved $dynamicRef "#node": `), diags[0].Message)

	held := useTargetOf(t, doc.Types[ids.DocumentIDNamespace{}.NamedType("/components/schemas/Held")])
	assert.Equal(t, ext.AnonymousType("/Param/schema"), held, "Held names the schema ext.yaml declares")
	model, ok := doc.Types[held].(*ir.Model)
	require.True(t, ok)
	assert.Equal(t, ir.Provenance{Source: 1, Pointer: "/Param/schema"}, model.Provenance)
	require.Len(t, model.Properties, 1)
	property := model.Properties[0]
	assert.Equal(t, ext.Property(inner), property.ID)
	assert.Equal(t, ir.TypeID("type/primitive/any"), property.Type.Target)
	assert.NotContains(t, property.Unmodeled, "openapi:$dynamicRef")
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

	kids, ok := doc.Types[ir.TypeID("type/anonymous/components/schemas/Tree/properties/kids")].(*ir.List)
	require.True(t, ok)
	element, ok := doc.Types[kids.ElementType.Target].(*ir.Scalar)
	require.True(t, ok, "the items position keeps a node of its own")
	require.NotNil(t, element.Base)
	assert.Equal(t, componentID("Tree"), element.Base.Target,
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

// TestDynamicRef_AnAnchorTheGrammarRefusesNamesNothing pins that an anchor is
// read as a $ref's is: 2020-12 §8.2.2's production admits no `%`, so the document
// validator reports the declaration, and it declares no name for a reference to
// reach, however the reference spells one: the reference names no
// $dynamicAnchor, so it is the $ref it spells, which resolves nowhere.
func TestDynamicRef_AnAnchorTheGrammarRefusesNamesNothing(t *testing.T) {
	t.Parallel()
	doc, diags := parseFull(t, openapitest.ComponentSpec(
		"    B: {$dynamicAnchor: 'pct%name', type: string}\n"+
			"    A: {$dynamicRef: '#pct%25name'}\n"))

	sc, ok := doc.Types[componentID("A")].(*ir.Scalar)
	require.True(t, ok, "the reference position owns a node")
	require.NotNil(t, sc.Base)
	assert.Equal(t, ir.PrimitiveTypeID(ir.PrimitiveAny), sc.Base.Target)
	assert.NotContains(t, sc.Unmodeled, "openapi:$dynamicRef")
	assertDiagContains(t, diags, "/components/schemas/A", `unresolved $dynamicRef "#pct%25name": `)
}

// TestDynamicRef_IrreducibleIsKeptAndSaysWhy pins the irreducible half of the
// docs/compilers/openapi.md#dynamic-ref promise; the resolvable half is
// TestDynamicRef_ExpandsAgainstTheOneMatchingAnchor.
// Each case is a reference no static lowering can resolve, and each must survive
// verbatim with the reason naming which case it was.
func TestDynamicRef_IrreducibleIsKeptAndSaysWhy(t *testing.T) {
	t.Parallel()
	const anchors = "    M1: {$dynamicAnchor: dup, type: string}\n" +
		"    M2: {$dynamicAnchor: dup, type: integer}\n" +
		"    Deep: {type: object, properties: {i: {$dynamicAnchor: deep, type: string}}}\n"
	const anchor = "    M: {$dynamicAnchor: m, type: string}\n"
	cases := []struct {
		name, schemas, wantWhy string
	}{
		{name: "declared twice", schemas: anchors + "    A: {$dynamicRef: '#dup'}\n",
			wantWhy: "declared 2 times"},
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
			// Its fragment names the anchor, as a $ref resolves it, in a resource
			// the reference is not in.
			name: "the anchor is in a resource of its own",
			schemas: "    M: {$id: 'https://example.com/m', $dynamicAnchor: m, type: string}\n" +
				"    A: {$dynamicRef: 'https://example.com/m#m'}\n",
			wantWhy: `an identifier between it and "/components/schemas/M" puts the two in different schema resources`,
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
			openapitest.RequireNoErrorDiags(t, diags)
			td, ok := doc.Types[componentID("A")]
			require.True(t, ok, "the position owns a node to keep the reference on")
			entry, ok := td.Common().Unmodeled["openapi:$dynamicRef"]
			require.True(t, ok, "an irreducible $dynamicRef is kept verbatim, not dropped")
			assert.Equal(t, ir.ReasonDegradedLowering, entry.Reason)
			assertDiagContains(t, diags, string(entry.Provenance.Pointer), tc.wantWhy)
		})
	}
}

// TestDynamicRef_NamingNoDynamicAnchorIsTheRefItSpells pins the static half of
// docs/compilers/openapi.md#dynamic-ref: a $dynamicRef whose fragment names no
// $dynamicAnchor, as a $ref resolves it, is that $ref (JSON Schema 2020-12
// §8.2.3.2). One resolving nowhere draws the error its $ref twin draws, worded
// with its keyword, and lowers the top type, keeping nothing; a pointer
// resolves.
func TestDynamicRef_NamingNoDynamicAnchorIsTheRefItSpells(t *testing.T) {
	t.Parallel()
	const anchor = "    M: {$dynamicAnchor: m, type: string}\n"
	cases := []struct {
		name, schemas, ref, at, invalid string
	}{
		{name: "no such anchor", schemas: "    A: {$dynamicRef: '#nope'}\n", ref: "#nope"},
		{name: "another document", schemas: "    A: {$dynamicRef: 'other.json#m'}\n", ref: "other.json#m"},
		{name: "an invalid percent escape", schemas: "    A: {$dynamicRef: '#my%zzanchor'}\n", ref: "#my%zzanchor",
			invalid: "openapi/validation/validation-invalid-format at /components/schemas/A/$dynamicRef"},
		{name: "an escaped percent decodes to the character", schemas: "    A: {$dynamicRef: '#pct%25name'}\n",
			ref: "#pct%25name"},
		// The anchor here is what a second decode would land on: it would read
		// this fragment as "#my-anchor" and resolve. Resolving nowhere is the proof.
		{name: "the decode is not applied twice",
			schemas: "    H: {$dynamicAnchor: my-anchor, type: string}\n    A: {$dynamicRef: '#my%252Danchor'}\n",
			ref:     "#my%252Danchor"},
		{name: "the anchor is in a resource of its own",
			schemas: "    M: {$id: 'https://example.com/m', $dynamicAnchor: m, type: string}\n    A: {$dynamicRef: '#m'}\n",
			ref:     "#m"},
		{name: "the reference is in a resource of its own",
			schemas: anchor + "    A: {$id: 'https://example.com/a', $dynamicRef: '#m'}\n", ref: "#m"},
		{name: "an enclosing schema starts the resource",
			schemas: anchor + "    A: {$id: 'https://example.com/a', type: array, items: {$dynamicRef: '#m'}}\n",
			ref:     "#m", at: "/components/schemas/A/items"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			doc, diags := parseFull(t, openapitest.ComponentSpec(tc.schemas))
			at := tc.at
			if at == "" {
				at = "/components/schemas/A"
			}
			var errs []string
			for _, diagnostic := range diags {
				if diagnostic.Severity == ir.SeverityError {
					errs = append(errs, diagnostic.Code+" at "+string(diagnostic.Provenance.Pointer))
				}
				assert.NotEqual(t, diag.DynamicRefExpanded, diagnostic.Code, "%+v", diagnostic)
				if diagnostic.Severity == ir.SeverityInfo {
					assert.False(t, strings.HasSuffix(string(diagnostic.Provenance.Pointer), "/$dynamicRef"), "nothing is kept: %+v", diagnostic)
				}
			}
			want := []string{"openapi/unresolved-ref at " + at}
			if tc.invalid != "" {
				want = append(want, tc.invalid)
			}
			assert.ElementsMatch(t, want, errs, "%+v", diags)
			assertDiagContains(t, diags, at, `unresolved $dynamicRef "`+tc.ref+`": `)
			for id, td := range doc.Types {
				assert.NotContains(t, td.Common().Unmodeled, "openapi:$dynamicRef", id)
			}
		})
	}

	doc, diags := parseFull(t, openapitest.ComponentSpec("    M1: {type: string}\n    A: {$dynamicRef: '#/components/schemas/M1'}\n"))
	assert.Empty(t, diags)
	a, ok := doc.Types[componentID("A")].(*ir.Scalar)
	require.True(t, ok)
	require.NotNil(t, a.Base)
	assert.Equal(t, componentID("M1"), a.Base.Target, "a pointer resolves")
	assert.Empty(t, a.Unmodeled)
}

// TestDynamicRef_AnAnchorOnlyAnotherDocumentDeclaresIsKept pins a $dynamicRef
// whose fragment lands on a $dynamicAnchor of another document: it is dynamic,
// and read against the declarations of the document it is written in, where
// none of its name stands, so it is kept.
func TestDynamicRef_AnAnchorOnlyAnotherDocumentDeclaresIsKept(t *testing.T) {
	t.Parallel()
	doc, diags, _ := compileBeside(t, map[string]string{"ext.yaml": "$defs: {M: {$dynamicAnchor: m, type: string}}\n"},
		openapitest.ComponentSpec("    A: {$dynamicRef: './ext.yaml#m'}\n"))
	openapitest.RequireNoErrorDiags(t, diags)
	a, ok := doc.Types[componentID("A")].(*ir.Scalar)
	require.True(t, ok)
	assert.Equal(t, ir.ReasonDegradedLowering, a.Unmodeled["openapi:$dynamicRef"].Reason)
	assertDiagContains(t, diags, "/components/schemas/A/$dynamicRef", `no $dynamicAnchor "m" is declared in this document`)
}

// TestDynamicRef_AnAnchorAnotherDocumentsResourceReachesIsKept pins a name
// declared once in the reference's document and again in a resource of the
// source that reaches it by a $ref: that resource is in the dynamic scope and
// outermost (JSON Schema 2020-12 §8.2.3.2), so the target depends on the
// evaluation path and the reference is kept.
func TestDynamicRef_AnAnchorAnotherDocumentsResourceReachesIsKept(t *testing.T) {
	t.Parallel()
	doc, diags, _ := compileBeside(t, map[string]string{"ext.yaml": "List:\n  $id: https://example.com/list\n" +
		"  type: array\n  items: {$dynamicRef: '#items'}\n  $defs: {items: {$dynamicAnchor: items, type: integer}}\n"},
		openapitest.ComponentSpec("    StrList: {$id: 'https://example.com/strlist', $ref: 'list', "+
			"$defs: {items: {$dynamicAnchor: items, type: string}}}\n    Plain: {$ref: './ext.yaml#/List'}\n"))
	openapitest.RequireNoErrorDiags(t, diags)
	items, ok := doc.Types[ids.In("ext.yaml").AnonymousType("/List/items")].(*ir.Scalar)
	require.True(t, ok)
	assert.Equal(t, ir.ReasonDegradedLowering, items.Unmodeled["openapi:$dynamicRef"].Reason)
	assert.Empty(t, openapitest.PointersOf(diags, diag.DynamicRefExpanded))
	for _, diagnostic := range diags {
		if diagnostic.Provenance.Pointer == "/List/items/$dynamicRef" {
			assert.Equal(t, 1, diagnostic.Provenance.Source)
			assert.Contains(t, diagnostic.Message, `$dynamicAnchor "items" is also declared in a schema resource of another `+
				"document that reaches this one")
		}
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
// A chain past the bound expands to nothing, so an $id past it is not seen. It
// must not read as none: in either order neither reference expands (GitHub #401).
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

// TestDynamicRef_ReadsWhatASchemaDeclares pins that a declaration and a resource
// boundary are the schemas', as a $ref reads them: a $dynamicAnchor in an example
// or an extension declares nothing, so it makes no name a duplicate, and a
// property named $id, or an $id raw YAML writes, starts no resource.
func TestDynamicRef_ReadsWhatASchemaDeclares(t *testing.T) {
	t.Parallel()
	for name, spec := range map[string]string{
		"an example": openapitest.ComponentSpec("    Node: {$dynamicAnchor: node, type: object}\n" +
			"    Ex: {type: object, example: {$dynamicAnchor: node}}\n    D: {$dynamicRef: '#node'}\n"),
		"an extension": openapitest.ComponentSpec("    Node: {$dynamicAnchor: node, type: object}\n"+
			"    D: {$dynamicRef: '#node'}\n") + "x-raw: {$dynamicAnchor: node}\n",
		"a property named $id": openapitest.ComponentSpec("    Node: {$dynamicAnchor: node, type: object}\n" +
			"    H: {type: object, properties: {$id: {type: string}, d: {$dynamicRef: '#node'}}}\n"),
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			doc, diags := parseFull(t, spec)
			openapitest.RequireNoErrorDiags(t, diags)
			assert.Contains(t, irtest.DiagCodes(diags), "openapi/dynamic-ref-expanded")
			for _, td := range doc.Types {
				assert.NotContains(t, td.Common().Unmodeled, "openapi:$dynamicRef")
			}
		})
	}
}

// TestDynamicRef_RestsOnNoReadTheMergeBoundCut pins that a chain past the bound
// keeps the reference: an $id past it may start a resource, and an anchor past it
// may make the name a duplicate, and either reads as nothing there (GitHub #401).
// Its report says the read was cut, and the one warning names the bound that did.
func TestDynamicRef_RestsOnNoReadTheMergeBoundCut(t *testing.T) {
	t.Parallel()
	var b strings.Builder
	b.WriteString("openapi: 3.1.0\ninfo: {title: Deep, version: \"1\"}\npaths: {}\nx-l0: &l0 {$id: 'https://example.com/deep'}\n")
	for i := 1; i <= 70; i++ {
		fmt.Fprintf(&b, "x-l%d: &l%d {<<: *l%d}\n", i, i, i-1)
	}
	b.WriteString("components:\n  schemas:\n    Node: {$dynamicAnchor: T, type: string}\n    A: {<<: *l70, $dynamicRef: '#T'}\n")
	doc, diags := parseFull(t, b.String())

	sc, ok := doc.Types[componentID("A")].(*ir.Scalar)
	require.True(t, ok)
	assert.Equal(t, ir.ReasonDegradedLowering, sc.Unmodeled["openapi:$dynamicRef"].Reason)
	assert.NotContains(t, irtest.DiagCodes(diags), "openapi/dynamic-ref-expanded")
	messages := make([]string, 0, len(diags))
	for _, diagnostic := range diags {
		messages = append(messages, diagnostic.Message)
	}
	joined := strings.Join(messages, "\n")
	assert.Contains(t, joined, "not read whole")
	assert.Contains(t, joined, "deeper than 64 levels")
	assert.NotContains(t, joined, "walk bounds", "the only bound in play is the merge bound")
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
		assert.Equal(t, ir.TypeID("type/primitive/any"), sc.Base.Target,
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
// error the rule table reports, and every reader then takes the field for left
// out (docs/compilers/openapi.md#validation), so nothing is kept or expanded and
// the table's error is the one report.
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
			require.True(t, ok, "the position owns a node")
			assert.NotContains(t, td.Common().Unmodeled, "openapi:$dynamicRef", "the malformed $dynamicRef is read as left out")
			assertDiagContains(t, diags, "/components/schemas/A/$dynamicRef", "must be a string")
		})
	}
}

// mergeBoundOrderSpec writes one document whose resource boundary is reachable
// only through a YAML merge chain, with the two schemas that walk it declared in
// the given order. P's chain is 51 links and expands whole; Q's rides on P's tail
// for 72, past nodeview's MergeDepthLimit of 64.
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
	for _, diagnostic := range diags {
		out = append(out, fmt.Sprintf("%s\x00%s\x00%s", diagnostic.Severity, diagnostic.Code, diagnostic.Provenance.Pointer))
	}
	slices.Sort(out)
	return out
}

// assertDiagContains requires one diagnostic at pointer whose message carries
// substr, so a case asserts the reason it was given and not merely that it was
// reported.
func assertDiagContains(t *testing.T, diags []ir.Diagnostic, pointer, substr string) {
	t.Helper()
	for _, diagnostic := range diags {
		if diagnostic.Provenance.Pointer == jsontext.Pointer(pointer) && strings.Contains(diagnostic.Message, substr) {
			return
		}
	}
	assert.Fail(t, "no diagnostic said why", "nothing at %q mentions %q; got %+v", pointer, substr, diags)
}

// dynamicAtSpec is a document declaring the anchor n at one position, holder
// being the component or the response holding it, and Uses, whose property m
// writes a $dynamicRef naming n and r a $ref naming that position, declared
// after holder or, first, before it.
func dynamicAtSpec(holder, uses string, first bool) string {
	schemas := holder + uses
	if first {
		schemas = uses + holder
	}
	return "openapi: 3.1.0\ninfo: {title: t, version: '1'}\npaths: {}\ncomponents:\n" +
		"  responses:\n    Rsp: {description: ok, content: {application/json: {schema: {$dynamicAnchor: rsp, type: object}}}}\n" +
		"  schemas:\n" + schemas
}

// TestDynamicRef_ExpandsToAnyPositionARefNames pins a $dynamicRef naming the one
// $dynamicAnchor of its name wherever its document declares it in its own
// resource: under $defs, on a property, on an items schema, in a response's
// inline schema and on the component keyed "". Each expands to the type a $ref
// to that position resolves to, the declaration's own ID, which no use mints,
// so the documents compiled with the reference declared before the anchor and
// after it are one.
func TestDynamicRef_ExpandsToAnyPositionARefNames(t *testing.T) {
	t.Parallel()
	for name, tc := range map[string]struct{ holder, name, pointer string }{
		"$defs": {"    Outer: {type: object, $defs: {Inner: {$dynamicAnchor: n, type: object}}}\n", "n",
			"/components/schemas/Outer/$defs/Inner"},
		"a property": {"    Outer: {type: object, properties: {p: {$dynamicAnchor: n, type: object}}}\n", "n",
			"/components/schemas/Outer/properties/p"},
		"an items schema": {"    Outer: {type: array, items: {$dynamicAnchor: n, type: object}}\n", "n",
			"/components/schemas/Outer/items"},
		"a response's inline schema": {"", "rsp",
			"/components/responses/Rsp/content/application~1json/schema"},
		`the component keyed ""`: {"    \"\": {$dynamicAnchor: n, type: object}\n", "n", "/components/schemas/"},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			uses := "    Uses: {type: object, properties: {m: {$dynamicRef: '#" + tc.name + "'}, r: {$ref: '#" +
				tc.pointer + "'}}}\n"
			docs := make([]*ir.Document, 0, 2)
			for _, first := range []bool{false, true} {
				doc, diags := parseFull(t, dynamicAtSpec(tc.holder, uses, first))
				for _, diagnostic := range diags {
					assert.NotEqual(t, ir.SeverityError, diagnostic.Severity, "%+v", diagnostic)
				}
				property, irProperty := propertyOf(t, doc, "Uses", "m"), propertyOf(t, doc, "Uses", "r")
				assert.Equal(t, irProperty.Type.Target, property.Type.Target, "the expansion names what the $ref names")
				assert.Equal(t, ids.In("").ForPointer(jsontext.Pointer(tc.pointer)), property.Type.Target)
				assert.NotContains(t, property.Unmodeled, "openapi:$dynamicRef")
				assert.Equal(t, []jsontext.Pointer{"/components/schemas/Uses/properties/m/$dynamicRef"},
					openapitest.PointersOf(diags, diag.DynamicRefExpanded))
				docs = append(docs, doc)
			}
			assert.Empty(t, cmp.Diff(docs[0].Types, docs[1].Types, orderInvariantIR()...),
				"which is declared first decides nothing")
			assert.Empty(t, cmp.Diff(diagnosticSet(docs[0].Diagnostics), diagnosticSet(docs[1].Diagnostics)))
		})
	}
}

// TestDynamicRef_ExpandsWithinOneResource pins the reference and its anchor
// read in one schema resource an $id starts: the reference expands to the
// anchor, there the resource's root, as the usual JSON Schema spelling of a
// recursive schema has it. An $id between the two puts them in two resources,
// so the fragment names no anchor of the reference's, which makes it the $ref
// it spells, resolving nowhere.
func TestDynamicRef_ExpandsWithinOneResource(t *testing.T) {
	t.Parallel()
	doc, diags := parseFull(t, openapitest.ComponentSpec(
		"    Dyn:\n      $id: 'https://example.com/dyn'\n      $dynamicAnchor: node\n      type: object\n"+
			"      properties: {children: {type: array, items: {$dynamicRef: '#node'}}}\n"+
			"    Split:\n      $dynamicAnchor: split\n      type: object\n"+
			"      properties: {c: {$id: 'https://example.com/c', type: array, items: {$dynamicRef: '#split'}}}\n"))
	items, ok := doc.Types[ir.TypeID("type/anonymous/components/schemas/Dyn/properties/children/items")].(*ir.Scalar)
	require.True(t, ok)
	require.NotNil(t, items.Base)
	assert.Equal(t, componentID("Dyn"), items.Base.Target)
	var errs []string
	for _, diagnostic := range diags {
		if diagnostic.Severity == ir.SeverityError {
			errs = append(errs, diagnostic.Code+" at "+string(diagnostic.Provenance.Pointer))
		}
	}
	assert.Equal(t, []string{"openapi/unresolved-ref at /components/schemas/Split/properties/c/items"}, errs,
		"#split names no anchor of c's own resource, so it is the $ref it spells, which resolves nowhere")
	assertDiagContains(t, diags, "/components/schemas/Split/properties/c/items", `unresolved $dynamicRef "#split": `)
}

// d2019 is the `$schema` of JSON Schema 2019-09, which spells a dynamic
// reference $recursiveRef.
const d2019 = "$schema: 'https://json-schema.org/draft/2019-09/schema'"

// TestRecursiveRef_ExpandsWhereItsTargetIsStatic pins 2019-09's $recursiveRef
// in an OpenAPI document: "#" names the root of the resource the reference is
// in, and the reference expands to it where that declares no $recursiveAnchor
// true, or no other resource declaring one, in a dialect defining the keyword,
// reaches it. Under no identifier "#" names the OpenAPI Object, which holds no
// schema, so it is kept, saying what makes it expand. Any other value, a
// sibling it cannot carry, a resource declaring the anchor that reaches it and
// an identifier or a declaration the merge bound may hide each keep it too,
// under degraded_lowering, saying why.
func TestRecursiveRef_ExpandsWhereItsTargetIsStatic(t *testing.T) {
	t.Parallel()
	items := func(name, head, ref string) string {
		return "    " + name + ": {" + d2019 + ", " + head + "type: object, properties: {c: {type: array, items: " +
			ref + "}}}\n"
	}
	tree := items("Tree", "$id: 'https://example.com/tree', $recursiveAnchor: true, ", "{$recursiveRef: '#'}")
	chain := func() string {
		var b strings.Builder
		b.WriteString("x-c0: &c0 {description: d}\n")
		for i := 1; i <= 70; i++ {
			fmt.Fprintf(&b, "x-c%d: &c%d {<<: *c%d}\n", i, i, i-1)
		}
		return b.String()
	}()
	const at = "/components/schemas/%s/properties/c/items"
	for _, tc := range []struct {
		name, schemas, extra, model, expandsTo, why string
	}{
		{name: "an anchored root under an identifier", schemas: tree, model: "Tree", expandsTo: "Tree"},
		{name: "two declarations in one resource", model: "Tree", expandsTo: "Tree",
			schemas: strings.Replace(tree, "{type: array, items:", "{type: array, $recursiveAnchor: true, items:", 1)},
		{name: "a root declaring no anchor", schemas: items("Static", "$id: 'https://example.com/s', ", "{$recursiveRef: '#'}"),
			model: "Static", expandsTo: "Static"},
		{name: "a root declaring none beside two resources declaring it", model: "Static", expandsTo: "Static",
			schemas: items("Static", "$id: 'https://example.com/s', ", "{$recursiveRef: '#'}") + tree +
				items("Other", "$id: 'https://example.com/o', $recursiveAnchor: true, ", "{$ref: 'https://example.com/tree'}")},
		{name: "two resources neither reaching the other", model: "Tree", expandsTo: "Tree",
			schemas: tree + items("Other", "$id: 'https://example.com/o', $recursiveAnchor: true, ", "{}")},
		{name: "a resource declaring it in a dialect defining none", model: "Tree", expandsTo: "Tree",
			schemas: tree + "    Modern: {$id: 'https://example.com/m', $recursiveAnchor: true}\n"},
		{name: "a root declaring the anchor false", model: "False", expandsTo: "False",
			schemas: items("False", "$id: 'https://example.com/f', $recursiveAnchor: false, ", "{$recursiveRef: '#'}")},
		{name: "no identifier", schemas: items("Bare", "$recursiveAnchor: true, ", "{$recursiveRef: '#'}"), model: "Bare",
			why: `"#" names the root of the document it is written in, which holds no schema; under an identifier of ` +
				`its own, the schema it means to recurse to would be that root`},
		{name: "another value", model: "Pointer",
			schemas: items("Pointer", "$id: 'https://example.com/p', ", "{$recursiveRef: '#/x'}"),
			why:     `its value is "#/x", and JSON Schema 2019-09 defines it only for "#"`},
		{name: "a sibling", model: "Beside",
			schemas: items("Beside", "$id: 'https://example.com/b', ", "{$recursiveRef: '#', type: string}"),
			why:     "it is co-declared with another applicator"},
		{name: "a resource declaring it that reaches this one", model: "Tree",
			schemas: tree + items("Other", "$id: 'https://example.com/o', $recursiveAnchor: true, ",
				"{$ref: 'https://example.com/tree'}"),
			why: "$recursiveAnchor: true is also declared in a schema resource that reaches this one"},
		{name: "a declaration past the merge bound", model: "Tree", extra: chain,
			schemas: tree + "    Deep: {<<: *c70}\n",
			why:     "this document's identifiers were not read whole"},
		{name: "an identifier past the merge bound", model: "Deep", extra: chain,
			schemas: "    Deep: {<<: *c70, " + d2019 + ", type: object, properties: {c: {type: array, items: {$recursiveRef: '#'}}}}\n",
			why:     `a schema at or above "/components/schemas/Deep/properties/c/items" merges past the levels`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			src := strings.Replace(openapitest.ComponentSpec(tc.schemas), "components:", tc.extra+"components:", 1)
			doc, diags := parseFull(t, src)
			pointer := fmt.Sprintf(at, tc.model)
			node := doc.Types[ir.TypeID("type/anonymous"+pointer)]
			require.NotNil(t, node, "the items position owns a node")
			if tc.expandsTo != "" {
				scalar, ok := node.(*ir.Scalar)
				require.True(t, ok)
				require.NotNil(t, scalar.Base)
				assert.Equal(t, componentID(tc.expandsTo), scalar.Base.Target)
				assert.Equal(t, []jsontext.Pointer{jsontext.Pointer(pointer + "/$recursiveRef")},
					openapitest.PointersOf(diags, diag.DynamicRefExpanded))
				assertDiagContains(t, diags, pointer+"/$recursiveRef", "the root of the schema resource it is written in")
				assert.NotContains(t, node.Common().Unmodeled, "openapi:$recursiveRef")
				return
			}
			entry, ok := node.Common().Unmodeled["openapi:$recursiveRef"]
			require.True(t, ok, "kept verbatim")
			assert.Equal(t, ir.ReasonDegradedLowering, entry.Reason)
			assertDiagContains(t, diags, pointer+"/$recursiveRef", "$recursiveRef was not expanded because "+tc.why)
		})
	}
}

// TestRecursiveRef_IsAKeywordOnlyWhereItsDialectDefinesIt pins 2019-09's pair
// outside the dialect defining it: in a 2020-12 schema each is an unknown
// keyword, kept verbatim with the reference unexpanded, where in a 2019-09 one
// `$recursiveAnchor` is consumed as `$dynamicAnchor` is. Beside a $dynamicRef,
// which is read first, a $recursiveRef is kept as unknown too.
func TestRecursiveRef_IsAKeywordOnlyWhereItsDialectDefinesIt(t *testing.T) {
	t.Parallel()
	doc, diags := parseFull(t, openapitest.ComponentSpec(
		"    Modern: {$recursiveAnchor: true, type: object, properties: {c: {type: array, items: {$recursiveRef: '#'}}}}\n"+
			"    Anchored: {"+d2019+", $id: 'https://example.com/a', $recursiveAnchor: true, type: object}\n"+
			"    Both: {"+d2019+", $dynamicAnchor: n, type: object, properties: {c: {$dynamicRef: '#n', $recursiveRef: '#'}}}\n"))
	openapitest.RequireNoErrorDiags(t, diags)
	items := doc.Types[ir.TypeID("type/anonymous/components/schemas/Modern/properties/c/items")]
	require.NotNil(t, items)
	assert.Equal(t, ir.ReasonOutOfScope, items.Common().Unmodeled["openapi:$recursiveRef"].Reason)
	assert.Equal(t, ir.ReasonOutOfScope, doc.Types[componentID("Modern")].Common().Unmodeled["openapi:$recursiveAnchor"].Reason)
	assert.NotContains(t, doc.Types[componentID("Anchored")].Common().Unmodeled, "openapi:$recursiveAnchor")
	assert.Contains(t, openapitest.PointersOf(diags, diag.UnknownSchemaKeyword),
		jsontext.Pointer("/components/schemas/Modern/properties/c/items/$recursiveRef"))
	assert.Equal(t, ir.ReasonOutOfScope, propertyOf(t, doc, "Both", "c").Unmodeled["openapi:$recursiveRef"].Reason)
}

// TestDynamicRef_ExpandsToAnAnchorOnlyADraftPlaces pins an anchor at a position
// the parser's model holds no schema for, a JSON Schema draft's `definitions`
// entry: the reference lands there, as a $ref naming the anchor would, so the
// schema is built and the reference expands to it. Where a $ref naming it
// would not resolve, an `$anchor` of its name making the name ambiguous in its
// resource, nothing builds it, and the reference is kept, saying so.
func TestDynamicRef_ExpandsToAnAnchorOnlyADraftPlaces(t *testing.T) {
	t.Parallel()
	const outer = "    Outer: {$schema: 'http://json-schema.org/draft-07/schema#', type: object, " +
		"definitions: {Inner: {$dynamicAnchor: n, type: object}}}\n    D: {$dynamicRef: '#n'}\n"
	doc, diags := parseFull(t, openapitest.ComponentSpec(outer))
	openapitest.RequireNoErrorDiags(t, diags)
	inner := ir.TypeID("type/anonymous/components/schemas/Outer/definitions/Inner")
	_, built := doc.Types[inner].(*ir.Model)
	assert.True(t, built)
	scalar, ok := doc.Types[componentID("D")].(*ir.Scalar)
	require.True(t, ok)
	require.NotNil(t, scalar.Base)
	assert.Equal(t, inner, scalar.Base.Target)

	doc, diags = parseFull(t, openapitest.ComponentSpec(outer+"    Twin: {$anchor: n, type: string}\n"))
	assert.NotContains(t, doc.Types, inner)
	assert.Contains(t, doc.Types[componentID("D")].Common().Unmodeled, "openapi:$dynamicRef")
	assertDiagContains(t, diags, "/components/schemas/D/$dynamicRef",
		`$dynamicAnchor "n" is declared at "/components/schemas/Outer/definitions/Inner", where no schema could be built`)
}

// TestRecursiveRef_LandsOnTheRootOfItsResource pins a $recursiveRef in
// another document whose resource's root no $ref reaches: the reference lands
// there as a $ref of "#" would, so the root is built and the reference expands
// to it, in that document's namespace.
func TestRecursiveRef_LandsOnTheRootOfItsResource(t *testing.T) {
	t.Parallel()
	doc, diags, _ := compileBeside(t, map[string]string{"ext.json": `{"$schema": "https://json-schema.org/draft/2019-09/schema",
  "$defs": {"T": {"$id": "https://example.com/t", "type": "object",
    "properties": {"c": {"type": "array", "items": {"$recursiveRef": "#"}}}}}}`},
		openapitest.ComponentSpec("    S: {$ref: 'ext.json#/$defs/T/properties/c'}\n"))
	openapitest.RequireNoErrorDiags(t, diags)
	root := ids.In("ext.json").ForPointer("/$defs/T")
	_, built := doc.Types[root].(*ir.Model)
	require.True(t, built, "the root no $ref reaches is built")
	items, ok := doc.Types[ids.In("ext.json").ForPointer("/$defs/T/properties/c/items")].(*ir.Scalar)
	require.True(t, ok)
	require.NotNil(t, items.Base)
	assert.Equal(t, root, items.Base.Target)
}

// TestDynamicRef_AListAnotherReadingKeepsIsNoReference holds a $dynamicRef
// whose value is a list an alias shares with another schema's `required`,
// which accepts it, so the value stays as written: the reference is not
// expanded, since its value is no reference string.
func TestDynamicRef_AListAnotherReadingKeepsIsNoReference(t *testing.T) {
	t.Parallel()
	src := "openapi: 3.1.0\ninfo: {title: t, version: \"1\"}\npaths: {}\nx-l: &l [a]\n" +
		"components:\n  schemas:\n    S: {$dynamicRef: *l}\n    T: {type: object, required: *l, properties: {a: {type: string}}}\n"

	_, diags, _ := compileFiles(t, nil, src, openapi.Options{})

	assertDiagContains(t, diags, "/components/schemas/S/$dynamicRef",
		"$dynamicRef was not expanded because its value is not a reference string")
}

// TestDiscriminator_AListAnotherReadingKeepsResolvesNothing holds a schema's
// `discriminator` that is a list an alias shares with another schema's
// `required`, which accepts it, so it stays as written and is kept
// unread: the model holds no discriminator there, so no mapping is resolved, and
// the one finding is the table's.
func TestDiscriminator_AListAnotherReadingKeepsResolvesNothing(t *testing.T) {
	t.Parallel()
	src := "openapi: 3.1.0\ninfo: {title: t, version: \"1\"}\npaths: {}\nx-l: &l [a]\n" +
		"components:\n  schemas:\n    S: {type: string, discriminator: *l}\n" +
		"    T: {type: object, required: *l, properties: {a: {type: string}}}\n"

	doc, diags, _ := compileFiles(t, nil, src, openapi.Options{})

	require.Len(t, diags, 2, "%+v", diags)
	assert.Equal(t, jsontext.Pointer("/components/schemas/S/discriminator"), diags[0].Provenance.Pointer)
	assert.Contains(t, doc.Types[componentID("S")].Common().Unmodeled, "openapi:discriminator", "kept verbatim")
}
