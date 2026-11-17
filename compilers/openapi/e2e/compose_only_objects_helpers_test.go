// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package e2e_test

import (
	"encoding/json/jsontext"
	"maps"
	"slices"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dexpace/morphic/compilers/openapi/internal/diag"
	"github.com/dexpace/morphic/ir"
)

// topSchema is the schema that admits every kind, which the cases below name.
const topSchema = "AnyT: {}"

// composeEntries writes a and b as the two entries of one allOf, in the order
// given when swapped is false and the other way round otherwise, and returns
// the index each takes: a test that expects an entry kept names it by that
// index, which an entry order moves.
func composeEntries(swapped bool, a, b string) (list string, ia, ib int) {
	if swapped {
		return "[" + b + ", " + a + "]", 1, 0
	}
	return "[" + a + ", " + b + "]", 0, 1
}

// entryKey is the key an inline allOf entry is kept under at index i.
func entryKey(i int) string { return "openapi:allOf/" + string(rune('0'+i)) }

// entryPointer is the pointer of the allOf entry at index i of the component
// named, where a $ref entry kept under openapi:$ref is written.
func entryPointer(name string, i int) jsontext.Pointer {
	return jsontext.Pointer("/components/schemas/" + name + "/allOf/" + string(rune('0'+i)))
}

// requireModelsNameModels holds every Model of doc to the rule that a base or
// a mixin is a Model: the check runs over the whole document, so a model the
// case does not name is held to it too.
func requireModelsNameModels(t *testing.T, doc *ir.Document) {
	t.Helper()
	for _, id := range slices.Sorted(maps.Keys(doc.Types)) {
		model, ok := doc.Types[id].(*ir.Model)
		if !ok {
			continue
		}
		parents := slices.Clone(model.Mixins)
		if model.Base != nil {
			parents = append(parents, *model.Base)
		}
		for _, parent := range parents {
			target, held := doc.Types[parent.Target]
			require.True(t, held, "%s names %s, which the document holds", id, parent.Target)
			assert.Equal(t, ir.KindModel, target.Kind(), "%s composes %s, which is no model", id, parent.Target)
		}
	}
}

// requireScalarOver holds the node named to be a Scalar whose Base is target,
// and returns it.
func requireScalarOver(t *testing.T, doc *ir.Document, name string, target ir.TypeID) *ir.Scalar {
	t.Helper()
	scalar, ok := kindNode(t, doc, componentID(name)).(*ir.Scalar)
	require.True(t, ok, "%s is a use of its target, not a model", name)
	require.NotNil(t, scalar.Base, name)
	assert.Equal(t, target, scalar.Base.Target, name)
	return scalar
}

// requireModelOf holds the node named to be a Model and returns it.
func requireModelOf(t *testing.T, doc *ir.Document, name string) *ir.Model {
	t.Helper()
	model, ok := kindNode(t, doc, componentID(name)).(*ir.Model)
	require.True(t, ok, "%s is a model", name)
	return model
}

// degradedKept is the Unmodeled entries a node is expected to hold, each kept for
// want of a home, for a case that asserts the keys and their reason together.
func degradedKept(keys ...string) map[string]ir.UnmodeledReason {
	kept := map[string]ir.UnmodeledReason{}
	for _, key := range keys {
		kept[key] = ir.ReasonDegradedLowering
	}
	return kept
}

// degradedInfosUnder counts the infos reporting a construct a node had no home
// for, at or under pointer.
func degradedInfosUnder(diags []ir.Diagnostic, pointer string) int {
	n := 0
	for _, d := range diags {
		at := string(d.Provenance.Pointer)
		if d.Severity == ir.SeverityInfo && d.Code == diag.DegradedConstruct &&
			(at == pointer || strings.HasPrefix(at, pointer+"/")) {
			n++
		}
	}
	return n
}

// degradedInfosAt is degradedInfosUnder for the component named.
func degradedInfosAt(diags []ir.Diagnostic, name string) int {
	return degradedInfosUnder(diags, "/components/schemas/"+name)
}

// disjointFinding is the warning the conjuncts of the component named draw
// where no instance satisfies them.
func disjointFinding(name string) wantFinding {
	return wantFinding{
		code: diag.DegradedConstruct, pointer: "/components/schemas/" + name, contains: []string{disjoint},
	}
}
