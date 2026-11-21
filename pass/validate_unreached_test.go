// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package pass_test

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/dexpace/morphic/ir"
	"github.com/dexpace/morphic/ir/irtest"
)

// TestValidate_TheChecksOfWhatASourceSaysSkipAReferenceWithNoDeclaration holds
// each of them to a position whose type is empty or names nothing the document
// declares: such a reference is irverify's when empty and the dangling check's
// otherwise, so a check that reads one stays silent and does not panic. It is
// the row that fails if a check forgets and reads an empty ID as a type to
// judge: every position holds a bound, a default, an example, a base or a
// requirement that a declared type would make a finding.
func TestValidate_TheChecksOfWhatASourceSaysSkipAReferenceWithNoDeclaration(t *testing.T) {
	t.Parallel()
	for name, target := range map[string]ir.TypeID{"empty": "", "undeclared": "type/ghost"} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			missing := ir.TypeRef{Target: target}
			blank := func(id string) ir.Property {
				return property(id, missing, func(property *ir.Property) {
					property.Required = true
					property.Constraints = lengths(6, 5)
					property.Default = text("x")
					property.Examples = []ir.Example{{Value: text("x")}}
				})
			}
			doc := noFiniteValueDoc(
				composed("type/a", &missing, blank("property/a/0")),
				&ir.List{ID: "type/l", ElementType: missing, Constraints: &ir.Constraints{MinItems: new(int64(1))}},
				&ir.MapT{ID: "type/mp", Value: missing, Constraints: &ir.Constraints{MinProperties: new(int64(1))}},
				&ir.Tuple{ID: "type/p", ElementTypes: []ir.TypeRef{missing, missing}},
				&ir.Union{ID: "type/u", Variants: []ir.Variant{{Type: missing}}},
				&ir.Scalar{ID: "type/s", Base: &missing},
			)
			doc.Types["type/a"].(*ir.Model).Mixins = []ir.TypeRef{missing}
			doc.Types["type/l"].Common().Examples = []ir.Example{{Value: &ir.Value{Kind: ir.ValueList, List: []ir.Value{*text("x")}}}}
			doc.Services = []ir.Service{{ID: "service/x", Groups: []ir.OperationGroup{{ID: "operation-group/x", Operations: []ir.Operation{{
				ID: "operation/x", Params: []ir.Parameter{{ID: "parameter/x/q", Type: missing, Default: text("x"), Constraints: numeric("10", "", "5", "")}},
				Examples: []ir.Example{
					{Name: "blank", Input: &ir.ScenarioInput{Params: map[ir.ParameterID]ir.Value{"": *text("x"), "parameter/x/q": *text("x"), "parameter/x/none": *text("x")}}},
					{Name: "failing", Input: &ir.ScenarioInput{}, Error: &ir.ErrorExample{Type: missing, Content: *text("x")}},
				},
				Responses: []ir.Response{{Headers: []ir.Header{{Property: blank("property/h")}}}},
			}}}}}}

			var diags []ir.Diagnostic
			assert.NotPanics(t, func() { diags = validated(t, doc) })

			for _, code := range []string{boundsCode, defaultCode, exampleCode, cycleCode, finiteCode} {
				assert.Empty(t, withCode(diags, code), "%s on a reference to %q", code, target)
			}
			if target != "" {
				assert.Contains(t, irtest.DiagCodes(diags), "ir/dangling-type-ref", "the reference is reported, by the check that owns it")
			}
		})
	}
}
