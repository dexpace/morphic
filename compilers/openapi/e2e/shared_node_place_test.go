// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package e2e_test

import (
	"slices"
	"strings"
	"testing"

	"github.com/google/go-cmp/cmp"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dexpace/morphic/compilers/openapi"
	"github.com/dexpace/morphic/compilers/openapi/internal/diag"
	"github.com/dexpace/morphic/internal/harness"
	"github.com/dexpace/morphic/ir"
)

// aliasAmplification is the code of the alias-expansion refusal.
const aliasAmplification = "openapi/alias-amplification"

// codedPointers returns the pointers of the diagnostics carrying one of codes,
// in the order diags lists them.
func codedPointers(diags []ir.Diagnostic, codes ...string) []string {
	var out []string
	for _, d := range diags {
		if slices.Contains(codes, d.Code) {
			out = append(out, string(d.Provenance.Pointer))
		}
	}
	return out
}

// kinds lists each diagnostic as its code and pointer, in the order they come
// sorted, which is what two compiles of one document in two declaration orders
// must agree on, positions and messages aside.
func kinds(diags []ir.Diagnostic) []string {
	out := make([]string, 0, len(diags))
	for _, d := range diags {
		out = append(out, d.Code+" "+string(d.Provenance.Pointer))
	}
	slices.Sort(out)
	return out
}

// sharedSchema writes one schema that holds a finding as the anchored value of
// the component first and an alias of it as the component second.
func sharedSchema(first, second string) string {
	return head31 + "paths: {}\ncomponents:\n  schemas:\n    " + first +
		": &m {type: object, properties: {p: {type: 7}}}\n    " + second + ": *m\n"
}

// TestCompile_AFindingOnANodeAliasesShareIsPlacedAtItsLeastPointer holds a
// finding about a node an anchor and its alias write to one pointer whichever
// of the two is declared first: the least of the places that mount it, A before
// B, and A before A-, since pointers are ordered token by token and not by the
// bytes of the whole string.
func TestCompile_AFindingOnANodeAliasesShareIsPlacedAtItsLeastPointer(t *testing.T) {
	t.Parallel()
	const want = "/components/schemas/A/properties/p/type"
	for name, spec := range map[string]string{
		"A holds the anchor":     sharedSchema("A", "B"),
		"B holds the anchor":     sharedSchema("B", "A"),
		"A- is the alias":        sharedSchema("A", "A-"),
		"A- holds the anchor":    sharedSchema("A-", "A"),
		"the anchor is last":     head31 + "paths: {}\ncomponents:\n  schemas:\n    Z: &m {type: object, properties: {p: {type: 7}}}\n    A: *m\n",
		"two aliases, one first": head31 + "paths: {}\ncomponents:\n  schemas:\n    M: &m {type: object, properties: {p: {type: 7}}}\n    B: *m\n    A: *m\n",
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			_, diags := compileText(t, "spec.yaml", spec, openapi.Options{})

			found := codedPointers(diags, typeMismatch, diag.Validation+"/validation-allowed-values")
			require.Len(t, found, 2, "%+v", diags)
			for _, pointer := range found {
				assert.Equal(t, want, pointer)
			}
		})
	}
}

// TestCompile_ASharedNodesFindingsDoNotDependOnWhichPlaceIsWrittenFirst holds
// the two declaration orders of #1796's documents to equal findings, apart from
// where the text of the node is, and to the node's own position in both.
func TestCompile_ASharedNodesFindingsDoNotDependOnWhichPlaceIsWrittenFirst(t *testing.T) {
	t.Parallel()

	_, ab := compileText(t, "a.yaml", sharedSchema("A", "B"), openapi.Options{})
	_, ba := compileText(t, "b.yaml", sharedSchema("B", "A"), openapi.Options{})

	require.NotEmpty(t, ab)
	assert.Empty(t, cmp.Diff(kinds(ab), kinds(ba)), "the same findings in either order")
	for _, d := range slices.Concat(ab, ba) {
		assert.Equal(t, ir.Position{Line: 6, Column: 49}, d.Provenance.Position, "the node's own text, which is where it is written: %+v", d)
	}
}

// aliasLadder writes #1796's pyramid, each level a list of ten aliases of the
// one below, from L0 up, each anchor at its level.
func aliasLadder() string {
	var b strings.Builder
	b.WriteString(head31 + "paths: {}\ncomponents:\n  schemas:\n    L0: &a0 {type: string}\n")
	for i := 1; i <= 5; i++ {
		alias := "*a" + string(rune('0'+i-1))
		b.WriteString("    L" + string(rune('0'+i)) + ": &a" + string(rune('0'+i)) + " {allOf: [" + strings.TrimSuffix(strings.Repeat(alias+",", 10), ",") + "]}\n")
	}
	return b.String()
}

// descendingLadder is the same schemas declared from L5 down, each anchor
// written at its first use.
const descendingLadder = head31 + "paths: {}\ncomponents:\n  schemas:\n" +
	"    L5: {allOf: [&a4 {allOf: [&a3 {allOf: [&a2 {allOf: [&a1 {allOf: [&a0 {type: string},*a0,*a0,*a0,*a0,*a0,*a0,*a0,*a0,*a0]}," +
	"*a1,*a1,*a1,*a1,*a1,*a1,*a1,*a1,*a1]},*a2,*a2,*a2,*a2,*a2,*a2,*a2,*a2,*a2]},*a3,*a3,*a3,*a3,*a3,*a3,*a3,*a3,*a3]}," +
	"*a4,*a4,*a4,*a4,*a4,*a4,*a4,*a4,*a4]}\n    L4: *a4\n    L3: *a3\n    L2: *a2\n    L1: *a1\n    L0: *a0\n"

// TestCompile_TheAmplificationRefusalNamesTheLeastPlaceOfTheNodeItReaches holds
// the alias-expansion refusal, which is placed by the node it reaches, to the
// same pointer whether the document declares the levels up or down.
func TestCompile_TheAmplificationRefusalNamesTheLeastPlaceOfTheNodeItReaches(t *testing.T) {
	t.Parallel()
	for name, spec := range map[string]string{"ascending": aliasLadder(), "descending": descendingLadder} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			doc, diags := compileText(t, "ladder.yaml", spec, openapi.Options{})

			assert.Nil(t, doc, "the expansion is refused")
			assert.Equal(t, []string{"/components/schemas/L4/allOf"}, codedPointers(diags, aliasAmplification), "%+v", diags)
		})
	}
}

// innerAnchors writes #1796's two-level case with each place of the inner node
// in the order's position: o is the outer node X holds and Y repeats, i the
// inner node it holds and A repeats.
func innerAnchors(order []string) string {
	var b strings.Builder
	b.WriteString(head31 + "paths: {}\ncomponents:\n  schemas:\n")
	oWritten, iWritten := false, false
	for _, key := range order {
		switch {
		case key == "A" && !iWritten:
			b.WriteString("    A: &i {type: 7}\n")
			iWritten = true
		case key == "A":
			b.WriteString("    A: *i\n")
		case oWritten:
			b.WriteString("    " + key + ": *o\n")
		case iWritten:
			b.WriteString("    " + key + ": &o {allOf: [*i]}\n")
			oWritten = true
		default:
			b.WriteString("    " + key + ": &o {allOf: [&i {type: 7}]}\n")
			oWritten, iWritten = true, true
		}
	}
	return b.String()
}

// TestCompile_AnInnerSharedNodeIsPlacedAtItsLeastPointerInEveryDeclarationOrder
// holds a node inside another shared node to the least of its own places, at
// the same pointer in each of the six orders the components can be declared in:
// the inner node is settled after the node that holds it.
func TestCompile_AnInnerSharedNodeIsPlacedAtItsLeastPointerInEveryDeclarationOrder(t *testing.T) {
	t.Parallel()
	orders := [][]string{
		{"X", "Y", "A"}, {"X", "A", "Y"}, {"Y", "X", "A"}, {"Y", "A", "X"}, {"A", "X", "Y"}, {"A", "Y", "X"},
	}
	for _, order := range orders {
		t.Run(strings.Join(order, ""), func(t *testing.T) {
			t.Parallel()

			_, diags := compileText(t, "spec.yaml", innerAnchors(order), openapi.Options{})

			found := codedPointers(diags, typeMismatch, diag.Validation+"/validation-allowed-values")
			require.NotEmpty(t, found, "%+v", diags)
			for _, pointer := range found {
				assert.Equal(t, "/components/schemas/A/type", pointer, "order %v", order)
			}
		})
	}
}

// TestCompile_TheSharedNodeFixturesAreOrderInvariantUnderTheOracle holds the two
// committed fixtures whose findings sit on a node aliases share to a verdict of
// the order oracle that asked and found nothing, with no fixture left among the
// known findings.
func TestCompile_TheSharedNodeFixturesAreOrderInvariantUnderTheOracle(t *testing.T) {
	t.Parallel()
	for _, name := range []string{"cycle_alias_dual_position", "amplification_alias_bomb"} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			res := harness.Check(t.Context(), name+".yaml", readReproducer(t, name))

			var order *harness.Verdict
			for i := range res.Verdicts {
				if res.Verdicts[i].Oracle == harness.OracleOrder {
					order = &res.Verdicts[i]
				}
			}
			require.NotNil(t, order)
			assert.True(t, order.Asked(), "the oracle asked: %s", order.Declined)
			assert.Equal(t, harness.OutcomeOK, order.Outcome, order.Detail)
		})
	}
}

// TestCompile_ATypeMismatchOnASharedMountIsPlacedAtTheLeastMount holds the
// fixture mounting one anchored $ref as a schema and as a properties map to the
// finding of its refused mount placed at the least of the places that mount it,
// and no false schema.
func TestCompile_ATypeMismatchOnASharedMountIsPlacedAtTheLeastMount(t *testing.T) {
	t.Parallel()

	_, diags := compileText(t, "cycle.yaml", string(readReproducer(t, "cycle_alias_dual_position")), openapi.Options{})

	assert.Equal(t, []string{"/components/schemas/A/properties/$ref"}, codedPointers(diags, typeMismatch), "%+v", diags)
	assert.Len(t, byCode(diags, "openapi/cyclic-ref"), 1)
}

// anchorCycle is a recursive anchor, which the document is refused at, with a
// second alias of the cycle's node under a key that sorts before it.
const anchorCycle = head31 + "paths: {}\nx-a: &a [*a]\nx-0: *a\n"

// TestCompile_AnAnchorCycleIsRefusedWhereItIsWritten holds a document with a
// recursive anchor, and a second alias of it under a lesser key, to the refusal
// at the cycle's written place: no node is placed anywhere else.
func TestCompile_AnAnchorCycleIsRefusedWhereItIsWritten(t *testing.T) {
	t.Parallel()

	doc, diags := compileText(t, "spec.yaml", anchorCycle, openapi.Options{})

	assert.Nil(t, doc)
	assert.Equal(t, []string{"/x-a/0"}, codedPointers(diags, "openapi/cyclic-ref"), "%+v", diags)
}
