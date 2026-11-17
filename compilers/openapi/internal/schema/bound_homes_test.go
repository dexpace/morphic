// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package schema_test

import (
	"testing"

	"github.com/google/go-cmp/cmp"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dexpace/morphic/compilers/openapi/internal/openapitest"
	"github.com/dexpace/morphic/ir"
)

// TestRefinement_AnOwnedNodeHoldsTheBoundsItsLoweringFolds pins which bounds an
// allOf branch writes land on the node the position owns where no entry decides
// what it lowers to, and which are kept verbatim: each is read off what builds
// the node, the position's types, and never off the node it built. A union of
// types holds a value bound in the node built for the type that takes it, and
// none of an object's, which the model of a type list builds with no voice, so
// a bound written for an object there is kept rather than dropped.
func TestRefinement_AnOwnedNodeHoldsTheBoundsItsLoweringFolds(t *testing.T) {
	t.Parallel()
	for name, tc := range map[string]struct {
		schema string
		kept   []string // the keywords kept verbatim on the position's own node
		holder string   // the node its folded bounds land on, if not the position's
		min    string   // the bound that folds there, by Constraints field
	}{
		"object variant takes no voice": {
			schema: "{type: [object, array], minProperties: 1, allOf: [{maxProperties: 2}]}",
			kept:   []string{"openapi:maxProperties"},
		},
		"string variant takes its own bound, an object's is kept": {
			schema: "{type: [object, string], allOf: [{minProperties: 2, minLength: 2}]}",
			kept:   []string{"openapi:minProperties"},
			holder: "/type/1", min: "minLength",
		},
		"array variant takes a collection bound": {
			schema: "{type: [object, array], allOf: [{maxItems: 3, minProperties: 2}]}",
			kept:   []string{"openapi:minProperties"},
			holder: "/type/1", min: "maxItems",
		},
		"no type to build for lowers to the top type's alias, which holds every bound": {
			schema: "{minItems: 1, allOf: [{minLength: 2}]}",
			kept:   []string{"openapi:minItems"},
			min:    "minLength",
		},
	} {
		doc, diags := parseFull(t, openapitest.ComponentSpecVer("3.1.0", "    X: "+tc.schema+"\n"))
		openapitest.RequireNoErrorDiags(t, diags)
		assert.Empty(t, validated(t, doc), name)

		kept := make([]string, 0, len(tc.kept))
		for key := range typeByName(doc, "X").Common().Unmodeled {
			kept = append(kept, key)
		}
		assert.ElementsMatch(t, tc.kept, kept, "%s: what the position keeps", name)

		if tc.min == "" {
			continue
		}
		holder := componentID("X")
		if tc.holder != "" {
			holder = ir.TypeID("t/anon/components/schemas/X" + tc.holder)
		}
		cons := constraintsOf(t, doc.Types[holder])
		require.NotNil(t, cons, name)
		switch tc.min {
		case "minLength":
			require.NotNil(t, cons.MinLength, name)
			assert.EqualValues(t, 2, *cons.MinLength, name)
		default:
			require.NotNil(t, cons.MaxItems, name)
			assert.EqualValues(t, 3, *cons.MaxItems, name)
		}
	}
}

// constraintsOf returns the Constraints of td, a Scalar or a List.
func constraintsOf(t *testing.T, td ir.TypeDef) *ir.Constraints {
	t.Helper()
	switch n := td.(type) {
	case *ir.Scalar:
		return n.Constraints
	case *ir.List:
		return n.Constraints
	default:
		require.Failf(t, "no constraints", "got %T", td)
		return nil
	}
}

// TestRefinement_AnOwnedNodeOfAnUntypedShapeKeepsNothingItHolds covers the nodes a
// refinement beside shape keywords with no type, deciding no kind, builds: a
// list for an array's keywords, and a union of an object's and an array's.
func TestRefinement_AnOwnedNodeOfAnUntypedShapeKeepsNothingItHolds(t *testing.T) {
	t.Parallel()
	for name, tc := range map[string]struct {
		schema string
		kind   ir.TypeKind
	}{
		"array":         {"{items: {type: string}, allOf: [{description: d}]}", ir.KindList},
		"object, array": {"{properties: {a: {type: string}}, items: {type: string}, allOf: [{description: d}]}", ir.KindUnion},
	} {
		doc, diags := parseFull(t, openapitest.ComponentSpecVer("3.1.0", "    X: "+tc.schema+"\n"))
		openapitest.RequireNoErrorDiags(t, diags)
		assert.Equal(t, tc.kind, typeByName(doc, "X").Kind(), name)
		assert.Empty(t, typeByName(doc, "X").Common().Unmodeled, name)
	}
}

// TestRefinement_AnOwnedNodeHoldsTheSameWhicheverIsDeclaredFirst compiles the
// positions of the cases above twice, the schema that uses each declared before
// and after it, which is what visits it first, and holds the two registries
// equal: what a position's node holds of its branches' bounds is a fact of what
// it writes, not of which lowering reached it.
func TestRefinement_AnOwnedNodeHoldsTheSameWhicheverIsDeclaredFirst(t *testing.T) {
	t.Parallel()
	for name, schema := range map[string]string{
		"object variant":   "{type: [object, array], minProperties: 1, allOf: [{maxProperties: 2}]}",
		"string variant":   "{type: [object, string], allOf: [{minProperties: 2, minLength: 2}]}",
		"top type's alias": "{minItems: 1, allOf: [{minLength: 2}]}",
	} {
		use := "    Use: {type: object, properties: {x: {$ref: '#/components/schemas/X'}}}\n"
		decl := "    X: " + schema + "\n"
		first, diags := parseFull(t, openapitest.ComponentSpecVer("3.1.0", use+decl))
		openapitest.RequireNoErrorDiags(t, diags)
		second, diags := parseFull(t, openapitest.ComponentSpecVer("3.1.0", decl+use))
		openapitest.RequireNoErrorDiags(t, diags)
		assert.Empty(t, cmp.Diff(first.Types, second.Types), name)
	}
}
