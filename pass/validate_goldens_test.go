// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package pass_test

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/dexpace/morphic/ir"
	"github.com/dexpace/morphic/pass"
)

// onTheGoldens returns, as "<golden> @ <node>: <message>", each finding keep
// accepts that run reports over each IR golden under testdata, in path order
// and, within one, in the order reported. A check that starts firing on a
// document it did not, or stops on one it did, changes the list.
func onTheGoldens(t *testing.T, run func(doc *ir.Document) []ir.Diagnostic, keep func(ir.Diagnostic) bool) []string {
	t.Helper()
	var out []string
	for _, sweepDocument := range sweepDocuments(t) {
		if sweepDocument.name == "reachDocument" {
			continue
		}
		for _, diag := range run(sweepDocument.doc) {
			if keep(diag) {
				out = append(out, strings.TrimPrefix(sweepDocument.name, "../testdata/")+" @ "+diag.Provenance.Node+": "+diag.Message)
			}
		}
	}
	return out
}

// validatesOnce is what Validate reports over doc, read once.
func validatesOnce(t *testing.T) func(doc *ir.Document) []ir.Diagnostic {
	return func(doc *ir.Document) []ir.Diagnostic { return validatedOnce(t, doc) }
}

// withTheCode keeps the findings of code.
func withTheCode(code string) func(ir.Diagnostic) bool {
	return func(diagnostic ir.Diagnostic) bool { return diagnostic.Code == code }
}

const (
	schemas      = "conformance/openapi/"
	componentsOf = "property/openapi/components/schemas/"
)

// TestValidate_BoundsFireOnTheGoldensWhereTheirSpecsContradictThemselves holds
// pass/bounds-admit-no-value to the two properties of one spec under testdata
// whose bounds exclude every number, a minimum past a maximum written beyond a
// float's range and an exclusive minimum past an exclusive maximum.
func TestValidate_BoundsFireOnTheGoldensWhereTheirSpecsContradictThemselves(t *testing.T) {
	t.Parallel()
	got := onTheGoldens(t, validatesOnce(t), withTheCode(boundsCode))

	assert.Equal(t, []string{
		schemas + "numeric-precision.golden.json @ " + componentsOf + "S/properties/bounded: bounds admit no number: " +
			"none is at least 1.8e308 and at most 123456789012345678901234567890",
		schemas + "numeric-precision.golden.json @ " + componentsOf + "S/properties/exclusive: bounds admit no number: " +
			"none is above 0.5 and below 0.12345678901234567890123456789",
	}, got)
}

// TestValidate_DefaultsFireOnTheGoldensWhereTheirTypeAdmitsNothingLikeThem holds
// pass/default-outside-type to the two defaults under testdata that their
// types do not admit: bytes on a string, and a string past a maximum length
// written on the allOf branch it is declared in. Every other default of the
// goldens, null on a nullable string and a list among them, is admitted.
func TestValidate_DefaultsFireOnTheGoldensWhereTheirTypeAdmitsNothingLikeThem(t *testing.T) {
	t.Parallel()
	got := onTheGoldens(t, validatesOnce(t), withTheCode(defaultCode))

	assert.Equal(t, []string{
		schemas + "allof-redeclared-bounds.golden.json @ " + componentsOf + "DefaultPastItsOwnBound/allOf/0/properties/p: " +
			"default lies outside the bounds declared for it",
		schemas + "defaults.golden.json @ " + componentsOf + "S/properties/blob: " +
			"default is bytes, and type/primitive/string holds a string",
	}, got)
}

// TestValidate_ExamplesAndValuesWithNoFiniteFormFireOnNoGolden holds the two
// codes that read what no spec under testdata writes wrongly to no finding.
func TestValidate_ExamplesAndValuesWithNoFiniteFormFireOnNoGolden(t *testing.T) {
	t.Parallel()
	for _, code := range []string{exampleCode, finiteCode} {
		assert.Empty(t, onTheGoldens(t, validatesOnce(t), withTheCode(code)), code)
	}
}

// TestValidate_ACompositionCycleFiresOnTheGoldenWhoseModelExtendsItself holds
// pass/composition-cycle to the one model under testdata whose base is itself.
func TestValidate_ACompositionCycleFiresOnTheGoldenWhoseModelExtendsItself(t *testing.T) {
	t.Parallel()
	got := onTheGoldens(t, validatesOnce(t), withTheCode(cycleCode))

	assert.Equal(t, []string{
		schemas + "union-restated-branches.golden.json @ type/openapi/components/schemas/RenderSelf: composition never ends: " +
			"base type/openapi/components/schemas/RenderSelf leads back into a cycle of bases and mixins",
	}, got)
}

// TestNames_HeaderCollisionsFireOnTheGoldenHoldingOneAndInheritedPropertiesOnNone
// holds the two scopes added to the name reader to the goldens: one pair of
// headers, x-rate beside X-Rate of one response, and no property meeting one
// its model inherits.
func TestNames_HeaderCollisionsFireOnTheGoldenHoldingOneAndInheritedPropertiesOnNone(t *testing.T) {
	t.Parallel()
	names := func(doc *ir.Document) []ir.Diagnostic {
		_, diags := pass.Names{}.Run(t.Context(), doc)
		return diags
	}
	scope := func(word string) func(ir.Diagnostic) bool {
		return func(diagnostic ir.Diagnostic) bool {
			return diagnostic.Code == "pass/name-collision" && strings.Contains(diagnostic.Message, ", "+word)
		}
	}

	assert.Equal(t, []string{
		schemas + "header-name-case.golden.json @ operation/openapi/paths/~1traces/get: in operation operation/openapi/paths/~1traces/get, " +
			`response "200", headers 0 ("x-rate") and 1 ("X-Rate") share the neutral name "x_rate", ` +
			"so a target can tell them apart only by ID",
	}, onTheGoldens(t, names, scope("response ")))
	assert.Empty(t, onTheGoldens(t, names, func(diagnostic ir.Diagnostic) bool { return strings.Contains(diagnostic.Message, "exposed propert") }))
	assert.Empty(t, onTheGoldens(t, names, scope("error ")))
	assert.Empty(t, onTheGoldens(t, names, scope("common error ")))
	assert.Empty(t, onTheGoldens(t, names, scope("the default response")))
}
