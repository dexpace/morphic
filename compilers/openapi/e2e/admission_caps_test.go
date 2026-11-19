// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package e2e_test

import (
	"fmt"
	"slices"
	"strings"
	"testing"

	"github.com/google/go-cmp/cmp"
	"github.com/google/go-cmp/cmp/cmpopts"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dexpace/morphic/compilers/openapi/internal/diag"
	"github.com/dexpace/morphic/ir"
)

// capNeighbours compose the schema T capTarget writes beside an object body:
// as an allOf's one entry, as one of two in either order, and as a union's
// branch in either order.
var capNeighbours = []string{
	"O: {type: object, properties: {o: {type: string}}}",
	"PA: {type: object, properties: {u: {type: string}}, allOf: [" + capRef("T") + "]}",
	"PA2: {type: object, properties: {u: {type: string}}, allOf: [" + capRef("T") + ", " + capRef("O") + "]}",
	"PA2r: {type: object, properties: {u: {type: string}}, allOf: [" + capRef("O") + ", " + capRef("T") + "]}",
	"PU: {type: object, properties: {u: {type: string}}, anyOf: [" + capRef("T") + ", " + capRef("O") + "]}",
	"PUr: {type: object, properties: {u: {type: string}}, anyOf: [" + capRef("O") + ", " + capRef("T") + "]}",
}

// capRef is a $ref to the component name.
func capRef(name string) string { return "{$ref: '#/components/schemas/" + name + "'}" }

// capTarget writes T, a schema of strings, and what it names, sized n: a
// union of n $refs to strings, so an entry naming it takes in 2n+2 schemas; a
// chain of $refs ending in a string that an entry naming T takes in n schemas
// of; or an enum of n strings.
func capTarget(kind string, n int) []string {
	switch kind {
	case "union":
		out := make([]string, 0, n+1)
		refs := make([]string, n)
		for i := range n {
			out = append(out, fmt.Sprintf("S%d: {type: string}", i))
			refs[i] = capRef(fmt.Sprintf("S%d", i))
		}
		return append(out, "T: {anyOf: ["+strings.Join(refs, ", ")+"]}")
	case "chain":
		out, prev := []string{"Str: {type: string}"}, "Str"
		for i := range n - 3 {
			out = append(out, fmt.Sprintf("A%d: %s", i, capRef(prev)))
			prev = fmt.Sprintf("A%d", i)
		}
		return append(out, "T: "+capRef(prev))
	default:
		members := make([]string, n)
		for i := range members {
			members[i] = fmt.Sprintf("v%d", i)
		}
		return []string{"T: {enum: [" + strings.Join(members, ", ") + "]}"}
	}
}

// TestCompile_AReadPastTheAdmissionCapsKeepsItsKinds holds a schema read past
// the caps of docs/compilers/openapi.md#allof-refinement, 256 schemas or 4,096
// enum members, to the kinds it lowers to, which a cap leaves whole: beside an
// object body, a union branch over a string is dead and an allOf entry over
// one draws the warning that the conjuncts share no value and is kept, in
// either order, and the neighbours lower and report as they do within the caps.
func TestCompile_AReadPastTheAdmissionCapsKeepsItsKinds(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		kind  string
		sizes []int
	}{{"union", []int{127, 128, 300}}, {"chain", []int{255, 256, 257}}, {"enum", []int{4096, 4097}}} {
		t.Run(tc.kind, func(t *testing.T) {
			t.Parallel()
			var within capNeighbourhood
			for _, n := range tc.sizes {
				schemas := slices.Concat(capNeighbours, capTarget(tc.kind, n))
				inBothOrders(t, schemas, func(t *testing.T, schemas []string) {
					doc, diags := compileAnnotationSpec(t, "caps", admissionSpec("3.1.0", "", schemas...))
					requireKindsRead(t, doc, diags)
					if schemas[0] != capNeighbours[0] {
						return
					}
					got := neighbourhoodOf(doc, diags)
					if within.types == nil {
						within = got
						return
					}
					assert.Empty(t, cmp.Diff(within, got, cmp.AllowUnexported(capNeighbourhood{}),
						cmpopts.IgnoreTypes(ir.Position{})), "size %d lowers as size %d", n, tc.sizes[0])
				})
			}
		})
	}
}

// requireKindsRead holds doc and diags to the lowering of capNeighbours over a
// T of strings: every branch over T dead, every entry over T kept and its
// conjunction reported, O the Base wherever an entry names it.
func requireKindsRead(t *testing.T, doc *ir.Document, diags []ir.Diagnostic) {
	t.Helper()
	for name, dead := range map[string]string{"PU": "anyOf/0", "PUr": "anyOf/1"} {
		union, ok := typeByName(doc, name).(*ir.Union)
		require.True(t, ok, name)
		assert.Len(t, union.Variants, 1, "%s: the branch over T is no variant", name)
		assert.Contains(t, union.Unmodeled, "openapi:"+dead, name)
	}
	for name, base := range map[string]ir.TypeID{"PA": "", "PA2": componentID("O"), "PA2r": componentID("O")} {
		model, ok := typeByName(doc, name).(*ir.Model)
		require.True(t, ok, name)
		got := ir.TypeID("")
		if model.Base != nil {
			got = model.Base.Target
		}
		assert.Equal(t, base, got, name)
		assert.Empty(t, model.Mixins, name)
		assert.Contains(t, model.Unmodeled, "openapi:$ref", "%s: the entry over T is kept", name)
	}
	requireErrorsAndWarnings(t, diags, []wantFinding{
		{code: diag.DegradedConstruct, pointer: "/components/schemas/PU", contains: []string{"anyOf/0: string"}},
		{code: diag.DegradedConstruct, pointer: "/components/schemas/PUr", contains: []string{"anyOf/1: string"}},
		{code: diag.DegradedConstruct, pointer: "/components/schemas/PA", contains: []string{"share no value", "allOf/0: string"}},
		{code: diag.DegradedConstruct, pointer: "/components/schemas/PA2", contains: []string{"share no value", "allOf/0: string"}},
		{code: diag.DegradedConstruct, pointer: "/components/schemas/PA2r", contains: []string{"share no value", "allOf/1: string"}},
	})
}

// capNeighbourhood is what capNeighbours lower to: each node a component or
// one of its variants owns, and each finding at one, in order.
type capNeighbourhood struct {
	types map[ir.TypeID]ir.TypeDef
	diags []ir.Diagnostic
}

// neighbourhoodOf returns the part of doc and diags capNeighbours own.
func neighbourhoodOf(doc *ir.Document, diags []ir.Diagnostic) capNeighbourhood {
	owned := func(s string) bool {
		return slices.ContainsFunc([]string{"/O", "/PA", "/PU"}, func(p string) bool {
			return strings.Contains(s, "/components/schemas"+p)
		})
	}
	out := capNeighbourhood{types: map[ir.TypeID]ir.TypeDef{}}
	for id, td := range doc.Types {
		if owned(string(id)) {
			out.types[id] = td
		}
	}
	for _, diagnostic := range diags {
		if owned(string(diagnostic.Provenance.Pointer)) {
			out.diags = append(out.diags, diagnostic)
		}
	}
	return out
}

// TestCompile_ABranchPastTheAdmissionCapsBesideANullableBodyLives holds the one
// question a read past the caps leaves open: beside a body that may admit
// null, whether the branch admits null is not read, so a branch over strings
// lives and is kept on its variant, where within the caps it is dead
// (docs/compilers/openapi.md#union-beside-structure).
func TestCompile_ABranchPastTheAdmissionCapsBesideANullableBodyLives(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		kind   string
		n      int
		within bool
	}{{"union", 127, true}, {"union", 128, false}, {"enum", 4096, true}, {"enum", 4097, false}} {
		t.Run(fmt.Sprint(tc.kind, " ", tc.n), func(t *testing.T) {
			t.Parallel()
			schemas := append([]string{capNeighbours[0], "PN: {type: [object, 'null'], properties: {u: {type: string}}, anyOf: [" +
				capRef("T") + ", " + capRef("O") + "]}"}, capTarget(tc.kind, tc.n)...)
			doc, diags := compileAnnotationSpec(t, "caps", admissionSpec("3.1.0", "", schemas...))
			union, ok := typeByName(doc, "PN").(*ir.Union)
			require.True(t, ok)
			if tc.within {
				assert.Len(t, union.Variants, 1)
				requireErrorsAndWarnings(t, diags, []wantFinding{
					{code: diag.DegradedConstruct, pointer: "/components/schemas/PN", contains: []string{"anyOf/0: string"}},
				})
				return
			}
			assert.Len(t, union.Variants, 2, "the branch over T lives")
			requireErrorsAndWarnings(t, diags, nil)
			assert.Equal(t, 1, countFindings(diags, diag.DegradedConstruct, ir.SeverityInfo, "/components/schemas/PN/anyOf/0"),
				"the branch is kept on its variant")
		})
	}
}
