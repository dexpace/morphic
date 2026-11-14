// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package irverify_test

import (
	"fmt"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dexpace/morphic/ir"
	"github.com/dexpace/morphic/ir/irverify"
)

const (
	armMisplaced = "ir/example-arm-misplaced"
	outcomeTwice = "ir/example-outcome-twice"
)

// exampleFindings returns each violation of the two example codes doc draws, as
// "code path: message", in the order Verify sorts them: by code, then path.
func exampleFindings(doc *ir.Document) []string {
	var out []string
	for _, v := range verifyDeclared(doc) {
		if v.Code == armMisplaced || v.Code == outcomeTwice {
			out = append(out, fmt.Sprintf("%s %s: %s", v.Code, v.Path, v.Message))
		}
	}
	return out
}

// scenarioErrorExample is the error a scenario ends in.
func scenarioErrorExample() *ir.ErrorExample {
	return &ir.ErrorExample{Type: ir.TypeRef{Target: "t/x/M"}, Content: scenarioValue}
}

// TestVerify_ExampleArms holds an example to the arms its carrier takes. Only an
// operation's example is a scenario, with Input and one of Output or Error;
// every other carrier's example is a value, and Headers beside it. No source
// writes either into the other's arm, so a document holding one is a compiler's
// defect, and an example ending in both an output and an error says two
// outcomes for one run.
func TestVerify_ExampleArms(t *testing.T) {
	t.Parallel()
	const model = "doc.Types[t/x/M].Examples[0]"
	op := scenarioOp + ".Examples[0]"
	cases := []struct {
		name  string
		owner func(doc *ir.Document) *[]ir.Example
		ex    ir.Example
		want  []string
	}{
		{"a model's example with a scenario's three arms", modelExamples, ir.Example{
			Name: "ex", Input: &ir.ScenarioInput{}, Output: &scenarioValue, Error: scenarioErrorExample(),
		}, []string{
			armMisplaced + " " + model + ".Error: example holds a scenario's error, and only an operation's example may",
			armMisplaced + " " + model + ".Input: example holds a scenario's input, and only an operation's example may",
			armMisplaced + " " + model + ".Output: example holds a scenario's output, and only an operation's example may",
			outcomeTwice + " " + model + ".Error: example ends in both an output and an error",
		}},
		{"an operation's example with a value and headers", operationExamples, ir.Example{
			Value: &scenarioValue, Headers: &scenarioValue, Output: &scenarioValue,
		}, []string{
			armMisplaced + " " + op + ".Headers: operation's example holds headers, and a scenario states its input and its outcome",
			armMisplaced + " " + op + ".Value: operation's example holds a value, and a scenario states its input and its outcome",
		}},
		{"an operation's example ending in both", operationExamples, ir.Example{
			Input: &ir.ScenarioInput{}, Output: &scenarioValue, Error: scenarioErrorExample(),
		}, []string{
			outcomeTwice + " " + op + ".Error: example ends in both an output and an error",
		}},
		// The valid twins: each is what its carrier takes, and draws nothing.
		{"a model's example holding a value and headers", modelExamples, ir.Example{
			Name: "ex", Value: &scenarioValue, Headers: &scenarioValue,
		}, nil},
		{"an operation's scenario ending in an output", operationExamples, ir.Example{
			Input: &ir.ScenarioInput{}, Output: &scenarioValue,
		}, nil},
		{"an operation's scenario ending in an error", operationExamples, ir.Example{
			Input: &ir.ScenarioInput{}, Error: scenarioErrorExample(),
		}, nil},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			doc := scenarioBase()
			*tc.owner(doc) = []ir.Example{tc.ex}

			assert.Equal(t, tc.want, exampleFindings(doc))
		})
	}
}

// modelExamples is the examples of scenarioBase's model.
func modelExamples(doc *ir.Document) *[]ir.Example {
	return &doc.Types["t/x/M"].Common().Examples
}

// operationExamples is the examples of scenarioBase's first operation.
func operationExamples(doc *ir.Document) *[]ir.Example {
	return &scenarioOperation(doc).Examples
}

// TestVerify_ExampleArmsAreHeldAtEveryCarrier plants a scenario's output in an
// example of each of the eight structs that carry examples but the operation,
// and requires the misplaced arm at each, so a check matching the field on one
// struct alone misses the rest.
func TestVerify_ExampleArmsAreHeldAtEveryCarrier(t *testing.T) {
	t.Parallel()
	example := []ir.Example{{Output: &scenarioValue}}
	plant := map[string]func(doc *ir.Document){
		"doc.Types[t/x/M].Examples[0].Output": func(d *ir.Document) {
			d.Types["t/x/M"].Common().Examples = example
		},
		"doc.Types[t/x/M].Properties[0].Examples[0].Output": func(d *ir.Document) {
			d.Types["t/x/M"].(*ir.Model).Properties[0].Examples = example
		},
		"doc.Types[t/x/E].Members[0].Examples[0].Output": func(d *ir.Document) {
			d.Types["t/x/E"].(*ir.Enum).Members[0].Examples = example
		},
		"doc.Types[t/x/U].Variants[0].Examples[0].Output": func(d *ir.Document) {
			d.Types["t/x/U"] = &ir.Union{ID: "t/x/U", Name: named("u"), Variants: []ir.Variant{
				{Name: named("m"), Type: ir.TypeRef{Target: "t/x/M"}, Examples: example},
			}}
		},
		scenarioOp + ".Params[0].Examples[0].Output": func(d *ir.Document) {
			scenarioOperation(d).Params[0].Examples = example
		},
		scenarioOp + ".Request.Contents[0].Examples[0].Output": func(d *ir.Document) {
			scenarioOperation(d).Request = &ir.Payload{Contents: []ir.Content{{
				MediaType: "application/json", Type: ir.TypeRef{Target: "t/x/M"}, Examples: example,
			}}}
		},
		"doc.Messages[msg/x/m].Examples[0].Output": func(d *ir.Document) {
			d.Messages = map[ir.MessageID]ir.Message{"msg/x/m": {ID: "msg/x/m", Examples: example}}
		},
	}
	for path, mutate := range plant {
		t.Run(path, func(t *testing.T) {
			t.Parallel()
			doc := scenarioBase()
			mutate(doc)

			got := verifyDeclared(doc)

			require.Contains(t, violationPaths(got, armMisplaced), path)
			for _, v := range got {
				if v.Code == armMisplaced {
					assert.Equal(t, path, v.Path, "one carrier is planted, so one arm is misplaced")
				}
			}
		})
	}
}

// exampleGrowth is a document of n models, each with the two examples a clean
// document carries, and a property with one.
func exampleGrowth(n int) *ir.Document {
	doc := scenarioBase()
	for i := range n {
		id := ir.TypeID(fmt.Sprintf("t/x/G%d", i))
		m := &ir.Model{ID: id, Name: named("g"), Properties: []ir.Property{{
			ID: ir.PropID(fmt.Sprintf("p/x/G%d/a", i)), Name: named("a"), Type: ir.TypeRef{Target: "t/prim/string"},
			Examples: []ir.Example{{Value: &scenarioValue}},
		}}}
		m.Examples = []ir.Example{{Name: "a", Value: &scenarioValue}, {Name: "b", Value: &scenarioValue, Headers: &scenarioValue}}
		doc.Types[id] = m
	}
	return doc
}

// TestVerify_ACleanExampleCostsNothing holds the example check to the cost of a
// value it finds clean: twice the models, each with its examples, cost less than
// one allocation more per model, so a path is spelled only for a violation. It
// is not run in parallel: AllocsPerRun measures the process.
func TestVerify_ACleanExampleCostsNothing(t *testing.T) {
	const models = 100
	few, many := exampleGrowth(models), exampleGrowth(2*models)
	require.Empty(t, exampleFindings(many), "the document is clean of the codes measured")

	allocs := func(doc *ir.Document) float64 {
		return testing.AllocsPerRun(5, func() { _ = irverify.Verify(doc) })
	}
	added := allocs(many) - allocs(few)
	assert.Less(t, added, float64(models), "%v allocations more for %d models more", added, models)
}
