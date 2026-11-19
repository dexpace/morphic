// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package irverify_test

import (
	"strconv"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dexpace/morphic/ir"
	"github.com/dexpace/morphic/ir/irtest"
	"github.com/dexpace/morphic/ir/irverify"
)

// examplesDoc returns a document whose one model carries each value as an
// example, with the scalar a constructor value names declared so that nothing
// but the values can be wrong. The walk reaches example i at
// document.Types[t/x/M].Examples[i].Value.
func examplesDoc(values ...ir.Value) *ir.Document {
	scalar := &ir.Scalar{ID: "type/x/S", Name: named("s"), Base: &ir.TypeRef{Target: "type/primitive/string"}}
	primitive := &ir.Primitive{ID: "type/primitive/string", PrimitiveKind: ir.PrimitiveString, Provenance: ir.Provenance{Source: ir.NoSource}}
	model := &ir.Model{ID: "type/x/M", Name: named("m")}
	for i := range values {
		model.Examples = append(model.Examples, ir.Example{Value: &values[i]})
	}
	return &ir.Document{
		IRVersion: ir.IRVersion,
		Types:     ir.TypeRegistry{scalar.ID: scalar, model.ID: model, primitive.ID: primitive},
	}
}

// constructorValueOf returns a constructor value over constructorValue.
func constructorValueOf(constructorValue *ir.ConstructorValue) ir.Value {
	return ir.Value{Kind: ir.ValueConstructor, Constructor: constructorValue}
}

// listValue returns a list value over values.
func listValue(values []ir.Value) ir.Value { return ir.Value{Kind: ir.ValueList, List: values} }

// cyclePaths returns where Verify reports ir/cyclic-structure, and requires that
// it reports nothing else: a fixture built for one defect that draws another has
// stopped testing what its name says. Nothing else includes ir/walk-truncated,
// which is all an unguarded walk reports for a list that holds itself (#736).
func cyclePaths(t *testing.T, doc *ir.Document) []string {
	t.Helper()
	got := verifyDeclared(doc)
	for _, violation := range got {
		require.Equal(t, "ir/cyclic-structure", violation.Code, "%s at %s", violation.Message, violation.Path)
	}
	return violationPaths(got, "ir/cyclic-structure")
}

// cyclicDocs are the self-containing shapes the ir package can hold: one for
// every pointer and slice type that reaches itself, which
// TestVerify_CyclicDocsCoverEverySelfReachableType holds the table to, and a
// branching form of some, since an unguarded walk descends a branching cycle
// exponentially. Each is paired with where Verify must report it.
var cyclicDocs = []struct {
	name string
	// types are the self-reachable types the row builds a cycle through.
	types []string
	doc   func() *ir.Document
	want  []string
}{
	{
		name:  "a constructor that is its own argument",
		types: []string{"*ir.ConstructorValue"},
		doc:   selfArgumentDoc,
		want:  []string{"document.Types[type/x/M].Examples[0].Value.Constructor.Args[0].Constructor"},
	},
	{
		name: "a constructor that is both of its arguments",
		doc: func() *ir.Document {
			constructorValue := &ir.ConstructorValue{Scalar: "type/x/S", Name: "from"}
			constructorValue.Args = []ir.Value{constructorValueOf(constructorValue), constructorValueOf(constructorValue)}
			return examplesDoc(constructorValueOf(constructorValue))
		},
		want: []string{
			"document.Types[type/x/M].Examples[0].Value.Constructor.Args[0].Constructor",
			"document.Types[type/x/M].Examples[0].Value.Constructor.Args[1].Constructor",
		},
	},
	{
		name: "two constructors that are each other's argument",
		doc: func() *ir.Document {
			a := &ir.ConstructorValue{Scalar: "type/x/S", Name: "a"}
			b := &ir.ConstructorValue{Scalar: "type/x/S", Name: "b"}
			a.Args = []ir.Value{constructorValueOf(b)}
			b.Args = []ir.Value{constructorValueOf(a)}
			return examplesDoc(constructorValueOf(a))
		},
		want: []string{"document.Types[type/x/M].Examples[0].Value.Constructor.Args[0].Constructor.Args[0].Constructor"},
	},
	{
		// GitHub #736: a list that is its own element, which a guard on pointers
		// alone does not see.
		name:  "a list that is its own element",
		types: []string{"[]ir.Value"},
		doc: func() *ir.Document {
			values := make([]ir.Value, 1)
			values[0] = listValue(values)
			return examplesDoc(listValue(values))
		},
		want: []string{"document.Types[type/x/M].Examples[0].Value.List[0].List"},
	},
	{
		name: "a list that is each of its three elements",
		doc: func() *ir.Document {
			values := make([]ir.Value, 3)
			for i := range values {
				values[i] = listValue(values)
			}
			return examplesDoc(listValue(values))
		},
		want: []string{
			"document.Types[type/x/M].Examples[0].Value.List[0].List",
			"document.Types[type/x/M].Examples[0].Value.List[1].List",
			"document.Types[type/x/M].Examples[0].Value.List[2].List",
		},
	},
	{
		name: "a list whose prefix holds the list it is a prefix of",
		doc: func() *ir.Document {
			values := []ir.Value{{}, {Kind: ir.ValueNull}}
			values[0] = listValue(values[:1])
			return examplesDoc(listValue(values[:1]))
		},
		want: []string{"document.Types[type/x/M].Examples[0].Value.List[0].List"},
	},
	{
		name:  "an object that is its own member",
		types: []string{"[]ir.Field"},
		doc: func() *ir.Document {
			fields := make([]ir.Field, 1)
			fields[0] = ir.Field{Name: "a", Value: ir.Value{Kind: ir.ValueObject, Object: fields}}
			return examplesDoc(ir.Value{Kind: ir.ValueObject, Object: fields})
		},
		want: []string{"document.Types[type/x/M].Examples[0].Value.Object[0].Value.Object"},
	},
	{
		name:  "a group that is each of its two subgroups",
		types: []string{"[]ir.OperationGroup"},
		doc: func() *ir.Document {
			operationGroups := []ir.OperationGroup{{ID: "operation-group/x/A", Name: named("a")}, {ID: "operation-group/x/B", Name: named("b")}}
			operationGroups[0].Groups, operationGroups[1].Groups = operationGroups, operationGroups
			return groupsDoc(operationGroups)
		},
		want: []string{
			"document.Services[0].Groups[0].Groups",
			"document.Services[0].Groups[1].Groups",
		},
	},
}

// selfArgumentDoc is GitHub #573's reproducer: a constructor whose one argument
// is a value holding the same constructor, as the first example of a model.
func selfArgumentDoc() *ir.Document {
	constructorValue := &ir.ConstructorValue{Scalar: "type/x/S", Name: "from"}
	constructorValue.Args = []ir.Value{constructorValueOf(constructorValue)}
	return examplesDoc(constructorValueOf(constructorValue))
}

// groupsDoc returns a document whose one service holds operationGroups as its
// groups.
func groupsDoc(operationGroups []ir.OperationGroup) *ir.Document {
	return &ir.Document{
		IRVersion: ir.IRVersion,
		Services:  []ir.Service{{ID: "service/x/S", Name: named("s"), Groups: operationGroups}},
	}
}

// TestVerify_CyclicStructureIsAViolation holds every self-containing shape to
// both halves of the claim #539's round-trip oracle rests on: Verify reports it,
// and the encoder would have refused it. Asking the encoder is what keeps the
// shapes honest: a fixture the encoder accepts is not a cycle, whatever the
// walk makes of it.
func TestVerify_CyclicStructureIsAViolation(t *testing.T) {
	t.Parallel()
	for _, c := range cyclicDocs {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			doc := c.doc()
			assert.Equal(t, c.want, cyclePaths(t, doc))
			assert.True(t, irtest.RefusedAsCyclic(doc), "the encoder must refuse what Verify calls a cycle")
		})
	}
}

// TestVerify_CyclicStructureNamesWhereItWasEntered holds the message to the one
// fact the path alone cannot give: where the walk first went inside the
// reference it met again.
func TestVerify_CyclicStructureNamesWhereItWasEntered(t *testing.T) {
	t.Parallel()
	got := verifyDeclared(selfArgumentDoc())
	require.Len(t, got, 1)
	assert.Contains(t, got[0].Message, "*ir.ConstructorValue")
	assert.Contains(t, got[0].Message, strconv.Quote("document.Types[type/x/M].Examples[0].Value.Constructor"))
}

// sharedDocs are documents that hold a reference in two places without either
// holding the other: the walk meets them twice, and so does the encoder, without
// trouble. Each is the control to a cyclic one above.
var sharedDocs = []struct {
	name string
	doc  func() *ir.Document
}{
	{
		name: "a constructor two examples share",
		doc: func() *ir.Document {
			constructorValue := &ir.ConstructorValue{Scalar: "type/x/S", Name: "from"}
			return examplesDoc(constructorValueOf(constructorValue), constructorValueOf(constructorValue))
		},
	},
	{
		name: "a constructor that is two arguments of another",
		doc: func() *ir.Document {
			leaf := &ir.ConstructorValue{Scalar: "type/x/S", Name: "leaf"}
			root := &ir.ConstructorValue{Scalar: "type/x/S", Name: "root", Args: []ir.Value{constructorValueOf(leaf), constructorValueOf(leaf)}}
			return examplesDoc(constructorValueOf(root))
		},
	},
	{
		name: "a list two examples share",
		doc: func() *ir.Document {
			values := []ir.Value{{Kind: ir.ValueNull}}
			return examplesDoc(listValue(values), listValue(values))
		},
	},
	{
		name: "a list that holds a prefix of itself",
		doc: func() *ir.Document {
			values := []ir.Value{{Kind: ir.ValueNull}, {}}
			values[1] = listValue(values[:1])
			return examplesDoc(listValue(values))
		},
	},
	{
		name: "an object two examples share",
		doc: func() *ir.Document {
			fields := []ir.Field{{Name: "a", Value: ir.Value{Kind: ir.ValueNull}}}
			return examplesDoc(ir.Value{Kind: ir.ValueObject, Object: fields}, ir.Value{Kind: ir.ValueObject, Object: fields})
		},
	},
}

// TestVerify_SharedReferenceIsNotACycle is the other side of the rule: a
// reference met twice is a cycle only when the second meeting is inside the
// first, and the encoder agrees.
func TestVerify_SharedReferenceIsNotACycle(t *testing.T) {
	t.Parallel()
	for _, c := range sharedDocs {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			doc := c.doc()
			assert.Empty(t, cyclePaths(t, doc))
			assert.False(t, irtest.RefusedAsCyclic(doc), "the encoder must accept what Verify does not call a cycle")
		})
	}
}

// TestVerify_RegistryKeysCannotMakeASharedPointerACycle builds the one place a
// path spelling could lie. A registry key is any string, so the entry under
// "type/x/M]." is written document.Types[t/x/M].], which reads as nested below
// document.Types[t/x/M] to anything that tells ancestry from the rendered path.
// The one model both keys hold is shared, not contained, so the only defect is
// the key that disagrees with the node.
func TestVerify_RegistryKeysCannotMakeASharedPointerACycle(t *testing.T) {
	t.Parallel()
	doc := examplesDoc()
	doc.Types["type/x/M]."] = doc.Types["type/x/M"]

	got := verifyDeclared(doc)
	assert.Empty(t, violationPaths(got, "ir/cyclic-structure"))
	assert.Contains(t, codesOf(got), "ir/type-id-mismatch", "the key that disagrees is still reported")
	assert.False(t, irtest.RefusedAsCyclic(doc))
}

// TestVerify_CyclicStructureIsReportedOnceAtEachWayIn pins what a list reachable
// two ways does: it is walked from each, as every shared slice is, so it is
// reported from each, at the path that way meets itself.
func TestVerify_CyclicStructureIsReportedOnceAtEachWayIn(t *testing.T) {
	t.Parallel()
	values := make([]ir.Value, 1)
	values[0] = listValue(values)
	doc := examplesDoc(listValue(values), listValue(values))

	assert.Equal(t, []string{
		"document.Types[type/x/M].Examples[0].Value.List[0].List",
		"document.Types[type/x/M].Examples[1].Value.List[0].List",
	}, cyclePaths(t, doc))
	assert.True(t, irtest.RefusedAsCyclic(doc))
}

// TestVerify_CyclicStructureInADeepDocumentIsStillFound guards the wiring rather
// than the rule: checkCycles is one of the walking checks, so the cut a deep
// document draws is reported once for all of them, and the cycle beside it is
// still found.
func TestVerify_CyclicStructureInADeepDocumentIsStillFound(t *testing.T) {
	t.Parallel()
	got := verifyDeclared(deepDocWithCycle())
	var truncations int
	for _, violation := range got {
		if violation.Code == "ir/walk-truncated" {
			truncations++
		}
	}
	assert.Equal(t, 1, truncations)
	assert.NotEmpty(t, violationPaths(got, "ir/cyclic-structure"), "a cycle in a document that is also deep is still found")
}

// deepDocWithCycle returns a document that nests past ir.MaxWalkDepth in one
// example and holds a self-containing list in another.
func deepDocWithCycle() *ir.Document {
	deep := ir.Value{Kind: ir.ValueNull}
	for range ir.MaxWalkDepth {
		deep = listValue([]ir.Value{deep})
	}
	values := make([]ir.Value, 1)
	values[0] = listValue(values)
	return examplesDoc(deep, listValue(values))
}

// TestVerify_CyclicDocsCoverEverySelfReachableType holds the table above to what
// the ir sources declare. A pointer, slice or map type that can come to hold
// itself is a place a cycle can form, so a type added to the IR that can is a
// type the table has to build a cycle through, and this fails until it does.
func TestVerify_CyclicDocsCoverEverySelfReachableType(t *testing.T) {
	t.Parallel()
	var covered []string
	for _, c := range cyclicDocs {
		covered = append(covered, c.types...)
	}
	assert.ElementsMatch(t, irverify.SelfReachableReferences(t), covered,
		"cyclicDocs must build a cycle through exactly the types that can reach themselves")
}
