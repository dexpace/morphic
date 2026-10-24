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
// doc.Types[t/x/M].Examples[i].Value.
func examplesDoc(values ...ir.Value) *ir.Document {
	scalar := &ir.Scalar{ID: "t/x/S", Name: named("s"), Base: &ir.TypeRef{Target: "t/prim/string"}}
	prim := &ir.Primitive{ID: "t/prim/string", Prim: ir.PrimString}
	model := &ir.Model{ID: "t/x/M", Name: named("m")}
	for i := range values {
		model.Examples = append(model.Examples, ir.Example{Value: &values[i]})
	}
	return &ir.Document{
		IRVersion: ir.IRVersion,
		Types:     ir.TypeRegistry{scalar.ID: scalar, model.ID: model, prim.ID: prim},
	}
}

// ctorValue returns a constructor value over c.
func ctorValue(c *ir.CtorValue) ir.Value { return ir.Value{Kind: ir.ValueCtor, Ctor: c} }

// listValue returns a list value over l.
func listValue(l []ir.Value) ir.Value { return ir.Value{Kind: ir.ValueList, List: l} }

// cyclePaths returns where Verify reports ir/cyclic-structure, and requires that
// it reports nothing else: a fixture built for one defect that draws another has
// stopped testing what its name says. Nothing else includes ir/walk-truncated,
// which is all an unguarded walk reports for a list that holds itself (#736).
func cyclePaths(t *testing.T, doc *ir.Document) []string {
	t.Helper()
	got := verifyDeclared(doc)
	for _, v := range got {
		require.Equal(t, "ir/cyclic-structure", v.Code, "%s at %s", v.Message, v.Path)
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
		types: []string{"*ir.CtorValue"},
		doc:   selfArgumentDoc,
		want:  []string{"doc.Types[t/x/M].Examples[0].Value.Ctor.Args[0].Ctor"},
	},
	{
		name: "a constructor that is both of its arguments",
		doc: func() *ir.Document {
			c := &ir.CtorValue{Scalar: "t/x/S", Name: "from"}
			c.Args = []ir.Value{ctorValue(c), ctorValue(c)}
			return examplesDoc(ctorValue(c))
		},
		want: []string{
			"doc.Types[t/x/M].Examples[0].Value.Ctor.Args[0].Ctor",
			"doc.Types[t/x/M].Examples[0].Value.Ctor.Args[1].Ctor",
		},
	},
	{
		name: "two constructors that are each other's argument",
		doc: func() *ir.Document {
			a := &ir.CtorValue{Scalar: "t/x/S", Name: "a"}
			b := &ir.CtorValue{Scalar: "t/x/S", Name: "b"}
			a.Args = []ir.Value{ctorValue(b)}
			b.Args = []ir.Value{ctorValue(a)}
			return examplesDoc(ctorValue(a))
		},
		want: []string{"doc.Types[t/x/M].Examples[0].Value.Ctor.Args[0].Ctor.Args[0].Ctor"},
	},
	{
		// GitHub #736: a list that is its own element, which a guard on pointers
		// alone does not see.
		name:  "a list that is its own element",
		types: []string{"[]ir.Value"},
		doc: func() *ir.Document {
			l := make([]ir.Value, 1)
			l[0] = listValue(l)
			return examplesDoc(listValue(l))
		},
		want: []string{"doc.Types[t/x/M].Examples[0].Value.List[0].List"},
	},
	{
		name: "a list that is each of its three elements",
		doc: func() *ir.Document {
			l := make([]ir.Value, 3)
			for i := range l {
				l[i] = listValue(l)
			}
			return examplesDoc(listValue(l))
		},
		want: []string{
			"doc.Types[t/x/M].Examples[0].Value.List[0].List",
			"doc.Types[t/x/M].Examples[0].Value.List[1].List",
			"doc.Types[t/x/M].Examples[0].Value.List[2].List",
		},
	},
	{
		name: "a list whose prefix holds the list it is a prefix of",
		doc: func() *ir.Document {
			l := []ir.Value{{}, {Kind: ir.ValueNull}}
			l[0] = listValue(l[:1])
			return examplesDoc(listValue(l[:1]))
		},
		want: []string{"doc.Types[t/x/M].Examples[0].Value.List[0].List"},
	},
	{
		name:  "an object that is its own member",
		types: []string{"[]ir.Field"},
		doc: func() *ir.Document {
			f := make([]ir.Field, 1)
			f[0] = ir.Field{Name: "a", Value: ir.Value{Kind: ir.ValueObject, Object: f}}
			return examplesDoc(ir.Value{Kind: ir.ValueObject, Object: f})
		},
		want: []string{"doc.Types[t/x/M].Examples[0].Value.Object[0].Value.Object"},
	},
	{
		name:  "a group that is each of its two subgroups",
		types: []string{"[]ir.OperationGroup"},
		doc: func() *ir.Document {
			g := []ir.OperationGroup{{ID: "g/x/A", Name: named("a")}, {ID: "g/x/B", Name: named("b")}}
			g[0].Groups, g[1].Groups = g, g
			return groupsDoc(g)
		},
		want: []string{
			"doc.Services[0].Groups[0].Groups",
			"doc.Services[0].Groups[1].Groups",
		},
	},
}

// selfArgumentDoc is GitHub #573's reproducer: a constructor whose one argument
// is a value holding the same constructor, as the first example of a model.
func selfArgumentDoc() *ir.Document {
	c := &ir.CtorValue{Scalar: "t/x/S", Name: "from"}
	c.Args = []ir.Value{ctorValue(c)}
	return examplesDoc(ctorValue(c))
}

// groupsDoc returns a document whose one service holds g as its groups.
func groupsDoc(g []ir.OperationGroup) *ir.Document {
	return &ir.Document{
		IRVersion: ir.IRVersion,
		Services:  []ir.Service{{ID: "s/x/S", Name: named("s"), Groups: g}},
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
	assert.Contains(t, got[0].Message, "*ir.CtorValue")
	assert.Contains(t, got[0].Message, strconv.Quote("doc.Types[t/x/M].Examples[0].Value.Ctor"))
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
			c := &ir.CtorValue{Scalar: "t/x/S", Name: "from"}
			return examplesDoc(ctorValue(c), ctorValue(c))
		},
	},
	{
		name: "a constructor that is two arguments of another",
		doc: func() *ir.Document {
			leaf := &ir.CtorValue{Scalar: "t/x/S", Name: "leaf"}
			root := &ir.CtorValue{Scalar: "t/x/S", Name: "root", Args: []ir.Value{ctorValue(leaf), ctorValue(leaf)}}
			return examplesDoc(ctorValue(root))
		},
	},
	{
		name: "a list two examples share",
		doc: func() *ir.Document {
			l := []ir.Value{{Kind: ir.ValueNull}}
			return examplesDoc(listValue(l), listValue(l))
		},
	},
	{
		name: "a list that holds a prefix of itself",
		doc: func() *ir.Document {
			l := []ir.Value{{Kind: ir.ValueNull}, {}}
			l[1] = listValue(l[:1])
			return examplesDoc(listValue(l))
		},
	},
	{
		name: "an object two examples share",
		doc: func() *ir.Document {
			f := []ir.Field{{Name: "a", Value: ir.Value{Kind: ir.ValueNull}}}
			return examplesDoc(ir.Value{Kind: ir.ValueObject, Object: f}, ir.Value{Kind: ir.ValueObject, Object: f})
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
// "t/x/M]." is written doc.Types[t/x/M].], which reads as nested below
// doc.Types[t/x/M] to anything that tells ancestry from the rendered path. The
// one model both keys hold is shared, not contained, so the only defect is the
// key that disagrees with the node.
func TestVerify_RegistryKeysCannotMakeASharedPointerACycle(t *testing.T) {
	t.Parallel()
	doc := examplesDoc()
	doc.Types["t/x/M]."] = doc.Types["t/x/M"]

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
	l := make([]ir.Value, 1)
	l[0] = listValue(l)
	doc := examplesDoc(listValue(l), listValue(l))

	assert.Equal(t, []string{
		"doc.Types[t/x/M].Examples[0].Value.List[0].List",
		"doc.Types[t/x/M].Examples[1].Value.List[0].List",
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
	for _, v := range got {
		if v.Code == "ir/walk-truncated" {
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
	l := make([]ir.Value, 1)
	l[0] = listValue(l)
	return examplesDoc(deep, listValue(l))
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
