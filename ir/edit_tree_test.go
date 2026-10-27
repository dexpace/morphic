package ir_test

import (
	"fmt"
	"math/rand/v2"
	"slices"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dexpace/morphic/ir"
	"github.com/dexpace/morphic/ir/irtest"
)

// treeOp returns an operation whose summary is its ID and whose one parameter
// is named after it.
func treeOp(id ir.OpID) ir.Operation {
	return ir.Operation{ID: id, Docs: ir.Docs{Summary: string(id)}, Params: []ir.Parameter{{ID: ir.ParamID(id + "/p")}}}
}

// treeGroup returns a group whose summary is its ID.
func treeGroup(id ir.GroupID, ops []ir.Operation, groups ...ir.OperationGroup) ir.OperationGroup {
	return ir.OperationGroup{ID: id, Docs: ir.Docs{Summary: string(id)}, Operations: ops, Groups: groups}
}

// treeService returns a service whose summary is its ID.
func treeService(id ir.ServiceID, groups ...ir.OperationGroup) ir.Service {
	return ir.Service{ID: id, Docs: ir.Docs{Summary: string(id)}, Groups: groups}
}

// treeDoc returns a document of two services, each node's summary its ID:
// s/a holds g/a, with operations o/a1, o/a2 and o/a3 and a group g/a/n holding
// o/n, and g/b holding o/b; s/b holds g/c holding o/c. Each call builds new
// nodes.
func treeDoc() *ir.Document {
	return &ir.Document{IRVersion: ir.IRVersion, Name: "tree", Services: []ir.Service{
		treeService("s/a",
			treeGroup("g/a", []ir.Operation{treeOp("o/a1"), treeOp("o/a2"), treeOp("o/a3")},
				treeGroup("g/a/n", []ir.Operation{treeOp("o/n")})),
			treeGroup("g/b", []ir.Operation{treeOp("o/b")})),
		treeService("s/b", treeGroup("g/c", []ir.Operation{treeOp("o/c")})),
	}}
}

// render spells the service tree of doc: a service as its ID and its groups in
// brackets, a group as its ID, its operations in parentheses and its groups in
// brackets, and an operation as its ID, each node's summary after an equals
// sign when it is not the node's ID. Nesting past depth levels is spelled as
// an ellipsis.
func render(doc *ir.Document) string {
	parts := make([]string, 0, len(doc.Services))
	for _, s := range doc.Services {
		parts = append(parts, named(string(s.ID), s.Docs)+"["+renderGroups(s.Groups, 8)+"]")
	}
	return strings.Join(parts, " ")
}

// renderGroups spells groups as render does, depth levels deep.
func renderGroups(groups []ir.OperationGroup, depth int) string {
	if depth == 0 {
		return "..."
	}
	parts := make([]string, 0, len(groups))
	for _, g := range groups {
		ops := make([]string, 0, len(g.Operations))
		for _, op := range g.Operations {
			ops = append(ops, named(string(op.ID), op.Docs))
		}
		part := named(string(g.ID), g.Docs) + "(" + strings.Join(ops, " ") + ")"
		if len(g.Groups) > 0 {
			part += "[" + renderGroups(g.Groups, depth-1) + "]"
		}
		parts = append(parts, part)
	}
	return strings.Join(parts, " ")
}

// named spells a node by its ID, and its summary when that is not its ID.
func named(id string, docs ir.Docs) string {
	if docs.Summary == id {
		return id
	}
	return id + "=" + docs.Summary
}

// changed sets the summary of the docs it is handed.
func changed(docs *ir.Docs) { docs.Summary = "changed" }

// TestEditor_WritesTheTreeAndNotTheDocument pins each write to the service
// tree: the tree the edit's document holds after it, and that the edited
// document still holds what it held, byte for byte.
func TestEditor_WritesTheTreeAndNotTheDocument(t *testing.T) {
	t.Parallel()
	updateOp := func(id ir.OpID) func(*ir.Editor) {
		return func(e *ir.Editor) { e.UpdateOperation(id, func(op *ir.Operation) { changed(&op.Docs) }) }
	}
	tests := []struct {
		name  string
		write func(*ir.Editor)
		want  string
	}{
		{"update an operation", updateOp("o/a2"),
			"s/a[g/a(o/a1 o/a2=changed o/a3)[g/a/n(o/n)] g/b(o/b)] s/b[g/c(o/c)]"},
		{"update a group", func(e *ir.Editor) {
			e.UpdateGroup("g/a", func(g *ir.OperationGroup) { changed(&g.Docs) })
		}, "s/a[g/a=changed(o/a1 o/a2 o/a3)[g/a/n(o/n)] g/b(o/b)] s/b[g/c(o/c)]"},
		{"update a nested group", func(e *ir.Editor) {
			e.UpdateGroup("g/a/n", func(g *ir.OperationGroup) { changed(&g.Docs) })
		}, "s/a[g/a(o/a1 o/a2 o/a3)[g/a/n=changed(o/n)] g/b(o/b)] s/b[g/c(o/c)]"},
		{"update a service", func(e *ir.Editor) {
			e.UpdateService("s/b", func(s *ir.Service) { changed(&s.Docs) })
		}, "s/a[g/a(o/a1 o/a2 o/a3)[g/a/n(o/n)] g/b(o/b)] s/b=changed[g/c(o/c)]"},
		{"remove an operation", func(e *ir.Editor) { e.RemoveOperation("o/a1") },
			"s/a[g/a(o/a2 o/a3)[g/a/n(o/n)] g/b(o/b)] s/b[g/c(o/c)]"},
		{"remove a group with what it holds", func(e *ir.Editor) { e.RemoveGroup("g/a") },
			"s/a[g/b(o/b)] s/b[g/c(o/c)]"},
		{"remove a nested group", func(e *ir.Editor) { e.RemoveGroup("g/a/n") },
			"s/a[g/a(o/a1 o/a2 o/a3) g/b(o/b)] s/b[g/c(o/c)]"},
		{"remove a service", func(e *ir.Editor) { e.RemoveService("s/a") }, "s/b[g/c(o/c)]"},
		{"remove a group's last operation, which leaves the group", func(e *ir.Editor) { e.RemoveOperation("o/b") },
			"s/a[g/a(o/a1 o/a2 o/a3)[g/a/n(o/n)] g/b()] s/b[g/c(o/c)]"},
		{"remove a service's last group, which leaves the service", func(e *ir.Editor) { e.RemoveGroup("g/c") },
			"s/a[g/a(o/a1 o/a2 o/a3)[g/a/n(o/n)] g/b(o/b)] s/b[]"},
		{"update past a removal", func(e *ir.Editor) {
			e.RemoveOperation("o/a1")
			updateOp("o/a3")(e)
			e.RemoveOperation("o/a2")
		}, "s/a[g/a(o/a3=changed)[g/a/n(o/n)] g/b(o/b)] s/b[g/c(o/c)]"},
		{"update before a removal", func(e *ir.Editor) {
			updateOp("o/a3")(e)
			e.RemoveOperation("o/a1")
			e.UpdateOperation("o/a3", func(op *ir.Operation) { op.Docs.Summary += " twice" })
		}, "s/a[g/a(o/a2 o/a3=changed twice)[g/a/n(o/n)] g/b(o/b)] s/b[g/c(o/c)]"},
		{"update a group past a removed sibling", func(e *ir.Editor) {
			e.RemoveGroup("g/a")
			e.UpdateGroup("g/b", func(g *ir.OperationGroup) { changed(&g.Docs) })
			updateOp("o/b")(e)
		}, "s/a[g/b=changed(o/b=changed)] s/b[g/c(o/c)]"},
		{"update a service past a removed one", func(e *ir.Editor) {
			e.RemoveService("s/a")
			updateOp("o/c")(e)
			e.UpdateService("s/b", func(s *ir.Service) { changed(&s.Docs) })
		}, "s/b=changed[g/c(o/c=changed)]"},
		{"update a group, then what it holds", func(e *ir.Editor) {
			e.UpdateGroup("g/a", func(g *ir.OperationGroup) { changed(&g.Docs) })
			updateOp("o/a1")(e)
			e.RemoveGroup("g/a/n")
		}, "s/a[g/a=changed(o/a1=changed o/a2 o/a3) g/b(o/b)] s/b[g/c(o/c)]"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			doc := treeDoc()
			before := irtest.Encoded(t, doc)

			e := ir.NewEditor(doc)
			tt.write(e)
			out, diags := e.Finish()

			assert.Empty(t, diags)
			assert.Equal(t, tt.want, render(out))
			assert.Equal(t, before, irtest.Encoded(t, doc), "the edited document changed")
		})
	}
}

// TestEditor_EmptiedTreeSlicesAreNil pins that a removal emptying a slice of
// the tree leaves it nil, as the document decodes one whose slice was empty,
// rather than empty and not nil.
func TestEditor_EmptiedTreeSlicesAreNil(t *testing.T) {
	t.Parallel()
	e := ir.NewEditor(treeDoc())
	e.RemoveOperation("o/b")
	e.RemoveGroup("g/c")
	e.RemoveService("s/a")
	out, diags := e.Finish()

	require.Empty(t, diags)
	assert.Equal(t, "s/b[]", render(out))
	assert.Nil(t, out.Services[0].Groups)

	e = ir.NewEditor(treeDoc())
	e.RemoveOperation("o/b")
	e.RemoveService("s/a")
	e.RemoveService("s/b")
	out, diags = e.Finish()

	require.Empty(t, diags)
	assert.Nil(t, out.Services)
}

// TestEditor_CopiesOnlyTheTreePathItWrites pins that a write to the tree
// copies the slices on the path to what it changes and nothing else: what an
// update or a removal does not reach stays shared with the edited document,
// and a read or a refused write copies nothing.
func TestEditor_CopiesOnlyTheTreePathItWrites(t *testing.T) {
	t.Parallel()
	doc := treeDoc()
	e := ir.NewEditor(doc)
	e.UpdateOperation("o/a2", func(op *ir.Operation) { changed(&op.Docs) })
	out, diags := e.Finish()
	require.Empty(t, diags)

	in, got := doc.Services[0].Groups, out.Services[0].Groups
	assert.NotSame(t, &doc.Services[0], &out.Services[0], "the services on the path were not copied")
	assert.NotSame(t, &in[0], &got[0], "the groups on the path were not copied")
	assert.NotSame(t, &in[0].Operations[1], &got[0].Operations[1], "the operations on the path were not copied")
	assert.NotSame(t, &in[0].Operations[1].Params[0], &got[0].Operations[1].Params[0], "the change shares the node")
	assert.Same(t, &in[0].Operations[0].Params[0], &got[0].Operations[0].Params[0], "an unchanged operation was copied")
	assert.Same(t, &in[0].Groups[0], &got[0].Groups[0], "a group off the path was copied")
	assert.Same(t, &in[1].Operations[0], &got[1].Operations[0], "a sibling group's operations were copied")
	assert.Same(t, &doc.Services[1].Groups[0], &out.Services[1].Groups[0], "another service's groups were copied")

	removed := treeDoc()
	e = ir.NewEditor(removed)
	e.RemoveOperation("o/a1")
	out, diags = e.Finish()
	require.Empty(t, diags)
	assert.Same(t, &removed.Services[0].Groups[0].Groups[0], &out.Services[0].Groups[0].Groups[0],
		"a removal copied a slice it drops nothing from")
	assert.Same(t, &removed.Services[1].Groups[0], &out.Services[1].Groups[0], "a removal copied another service")

	read := treeDoc()
	e = ir.NewEditor(read)
	_, _ = e.Operation("o/a2")
	_, _ = e.Group("g/a")
	_, _ = e.Service("s/a")
	e.UpdateOperation("o/a2", func(op *ir.Operation) { op.ID = "o/moved" })
	e.UpdateGroup("g/a/n", func(g *ir.OperationGroup) { g.ID = "g/moved" })
	e.UpdateService("s/b", func(s *ir.Service) { s.ID = "s/moved" })
	e.RemoveOperation("o/absent")
	out, diags = e.Finish()
	require.Len(t, diags, 4)
	assert.Same(t, &read.Services[0], &out.Services[0], "a read or a refused write copied the services")
}

// TestEditor_ReadsTheTreeAsTheEditHoldsIt pins that Operation, Group and
// Service answer with what the edit holds: a node changed or emptied as
// written, no node once removed or held inside one removed, and what the
// edited document holds otherwise, inside a change as outside one.
func TestEditor_ReadsTheTreeAsTheEditHoldsIt(t *testing.T) {
	t.Parallel()
	doc := treeDoc()
	e := ir.NewEditor(doc)
	e.UpdateOperation("o/a2", func(op *ir.Operation) { changed(&op.Docs) })
	e.RemoveOperation("o/a1")
	e.RemoveGroup("g/a/n")
	e.RemoveService("s/b")

	op, held := e.Operation("o/a2")
	assert.True(t, held)
	assert.Equal(t, "changed", op.Docs.Summary)
	for _, id := range []ir.OpID{"o/a1", "o/n", "o/c", "o/absent"} {
		_, held = e.Operation(id)
		assert.False(t, held, "operation %s", id)
	}
	group, held := e.Group("g/a")
	require.True(t, held)
	assert.Equal(t, "g/a(o/a2=changed o/a3)", renderGroups([]ir.OperationGroup{group}, 8))
	for _, id := range []ir.GroupID{"g/a/n", "g/c", "g/absent"} {
		_, held = e.Group(id)
		assert.False(t, held, "group %s", id)
	}
	svc, held := e.Service("s/a")
	require.True(t, held)
	assert.Equal(t, "s/a[g/a(o/a2=changed o/a3) g/b(o/b)]", render(&ir.Document{Services: []ir.Service{svc}}))
	_, held = e.Service("s/b")
	assert.False(t, held)
	untouched, held := e.Operation("o/b")
	assert.True(t, held)
	assert.Equal(t, doc.Services[0].Groups[1].Operations[0].Docs, untouched.Docs)

	e.UpdateGroup("g/b", func(g *ir.OperationGroup) {
		read, inEdit := e.Operation("o/a2")
		g.Docs.Summary = fmt.Sprint(inEdit, " ", read.Docs.Summary)
	})
	group, _ = e.Group("g/b")
	assert.Equal(t, "true changed", group.Docs.Summary)
	_, diags := e.Finish()
	assert.Empty(t, diags)
}

// TestEditor_RefusesTreeWritesOnlyABugMakes pins each write to the tree
// refused for what it asks alone: the one message Finish reports, and that the
// refused write leaves the edit's document as a control edit making only the
// writes before it leaves its own.
func TestEditor_RefusesTreeWritesOnlyABugMakes(t *testing.T) {
	t.Parallel()
	removeGroup := func(e *ir.Editor) { e.RemoveGroup("g/a") }
	tests := []struct {
		name    string
		before  func(*ir.Editor)
		refused func(*ir.Editor)
		want    string
	}{
		{"update an absent operation", nil, func(e *ir.Editor) { e.UpdateOperation("o/x", func(*ir.Operation) {}) },
			`update rejected: no operation "o/x" to update; an edit adds no operation`},
		{"remove an absent group", nil, func(e *ir.Editor) { e.RemoveGroup("g/x") },
			`remove rejected: no group "g/x" to remove; an edit adds no group`},
		{"update an absent service", nil, func(e *ir.Editor) { e.UpdateService("s/x", func(*ir.Service) {}) },
			`update rejected: no service "s/x" to update; an edit adds no service`},
		{"remove a removed operation", func(e *ir.Editor) { e.RemoveOperation("o/b") },
			func(e *ir.Editor) { e.RemoveOperation("o/b") }, `remove rejected: no operation "o/b" to remove`},
		{"update an operation inside a removed group", removeGroup,
			func(e *ir.Editor) { e.UpdateOperation("o/n", func(*ir.Operation) {}) }, `update rejected: no operation "o/n"`},
		{"remove a group inside a removed group", removeGroup,
			func(e *ir.Editor) { e.RemoveGroup("g/a/n") }, `remove rejected: no group "g/a/n"`},
		{"remove a removed service", func(e *ir.Editor) { e.RemoveService("s/b") },
			func(e *ir.Editor) { e.RemoveService("s/b") }, `remove rejected: no service "s/b"`},
		{"update an operation with no change", nil, func(e *ir.Editor) { e.UpdateOperation("o/a1", nil) },
			`update rejected: no change for "o/a1"`},
		{"update a group with no change", nil, func(e *ir.Editor) { e.UpdateGroup("g/a", nil) },
			`update rejected: no change for "g/a"`},
		{"update moving an operation", nil, func(e *ir.Editor) {
			e.UpdateOperation("o/a1", func(op *ir.Operation) {
				op.ID = "o/moved"
				changed(&op.Docs)
			})
		}, `update rejected: the change moved "o/a1" to "o/moved"; a node keeps its ID`},
		{"update emptying a group's ID", nil, func(e *ir.Editor) {
			e.UpdateGroup("g/b", func(g *ir.OperationGroup) { *g = ir.OperationGroup{} })
		}, `update rejected: the change moved "g/b" to ""; a node keeps its ID`},
		{"update moving a service", nil, func(e *ir.Editor) {
			e.UpdateService("s/b", func(s *ir.Service) { s.ID = "s/a" })
		}, `update rejected: the change moved "s/b" to "s/a"; a node keeps its ID`},
		{"update setting a service's groups", nil, func(e *ir.Editor) {
			e.UpdateService("s/a", func(s *ir.Service) { s.Groups = []ir.OperationGroup{} })
		}, `update rejected: the change of "s/a" set its Groups, whose nodes the edit changes by their own IDs`},
		{"update setting a group's groups", nil, func(e *ir.Editor) {
			e.UpdateGroup("g/b", func(g *ir.OperationGroup) { g.Groups = []ir.OperationGroup{treeGroup("g/new", nil)} })
		}, `update rejected: the change of "g/b" set its Groups`},
		{"update setting a group's operations", nil, func(e *ir.Editor) {
			e.UpdateGroup("g/a", func(g *ir.OperationGroup) {
				changed(&g.Docs)
				g.Operations = []ir.Operation{}
			})
		}, `update rejected: the change of "g/a" set its Operations`},
		{"update adding a parameter", nil, func(e *ir.Editor) {
			e.UpdateOperation("o/a1", func(op *ir.Operation) { op.Params = append(op.Params, ir.Parameter{ID: "o/x/p"}) })
		}, `update rejected: the change of "o/a1" declares the ir.ParamID "o/x/p", which the node does not; ` +
			`an edit adds no ID`},
		{"update after a refused update", func(e *ir.Editor) { e.UpdateGroup("g/a", nil) }, func(e *ir.Editor) {
			e.UpdateService("s/b", func(s *ir.Service) { s.Groups = nil; s.ID = "s/c" })
		}, `update rejected: the change moved "s/b" to "s/c"`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			doc, control := treeDoc(), treeDoc()
			e, c := ir.NewEditor(doc), ir.NewEditor(control)
			if tt.before != nil {
				tt.before(e)
				tt.before(c)
			}
			tt.refused(e)

			out, diags := e.Finish()
			want, ctrl := c.Finish()

			got := refusals(t, diags)
			require.Len(t, got, len(refusals(t, ctrl))+1, "one refused write, one refusal: %v", got)
			assert.Contains(t, got[len(got)-1], "internal: edit: "+tt.want)
			assert.Equal(t, irtest.Encoded(t, want), irtest.Encoded(t, out), "the refused write changed the edit")
			assert.Equal(t, irtest.Encoded(t, treeDoc()), irtest.Encoded(t, doc), "the edited document changed")
		})
	}
}

// TestEditor_RefusesAnIDTheTreeHoldsTwice pins the edit of a tree holding one
// ID at two nodes, which irverify reports: neither is read or written, as
// which is meant is unknown, until the edit removes all but one.
func TestEditor_RefusesAnIDTheTreeHoldsTwice(t *testing.T) {
	t.Parallel()
	doc := &ir.Document{Services: []ir.Service{
		treeService("s/x",
			treeGroup("g/x", []ir.Operation{treeOp("o/x")}),
			treeGroup("g/y", []ir.Operation{treeOp("o/x"), treeOp("o/y")})),
		treeService("s/x"),
	}}

	e := ir.NewEditor(doc)
	e.UpdateOperation("o/x", func(op *ir.Operation) { changed(&op.Docs) })
	e.RemoveService("s/x")
	_, opHeld := e.Operation("o/x")
	_, svcHeld := e.Service("s/x")
	e.RemoveGroup("g/x")
	e.UpdateOperation("o/x", func(op *ir.Operation) { changed(&op.Docs) })
	op, nowHeld := e.Operation("o/x")
	out, diags := e.Finish()

	assert.Equal(t, []string{
		`internal: edit: update rejected: 2 operations have the ID "o/x", so the edit cannot tell which to update`,
		`internal: edit: remove rejected: 2 services have the ID "s/x", so the edit cannot tell which to remove`,
	}, refusals(t, diags))
	assert.False(t, opHeld)
	assert.False(t, svcHeld)
	assert.True(t, nowHeld, "removing one of two operations at an ID leaves the other held")
	assert.Equal(t, "changed", op.Docs.Summary)
	assert.Equal(t, "s/x[g/y(o/x=changed o/y)] s/x[]", render(out))
}

// nestedGroups returns a service whose groups nest levels deep, one operation
// o/<level> in each, the top-level group at level 0.
func nestedGroups(levels int) ir.Service {
	var inner []ir.OperationGroup
	for level := levels - 1; level >= 0; level-- {
		id := fmt.Sprint(level)
		inner = []ir.OperationGroup{{ID: ir.GroupID("g/" + id), Groups: inner,
			Operations: []ir.Operation{{ID: ir.OpID("o/" + id)}}}}
	}
	return ir.Service{ID: "s/deep", Groups: inner}
}

// TestEditor_ReachesTheTreeForEachOperationReaches pins the edit's reach to
// ForEachOperation's: an operation in a group nested past MaxGroupDepth is
// neither read nor written, and a write naming one says the cap is why, while
// one on the cap is reached.
func TestEditor_ReachesTheTreeForEachOperationReaches(t *testing.T) {
	t.Parallel()
	doc := &ir.Document{Services: []ir.Service{nestedGroups(ir.MaxGroupDepth + 2)}}
	var walked []ir.OpID
	require.True(t, ir.ForEachOperation(doc, func(op ir.Operation) { walked = append(walked, op.ID) }))

	e := ir.NewEditor(doc)
	var held []ir.OpID
	for level := range ir.MaxGroupDepth + 2 {
		if _, ok := e.Operation(ir.OpID(fmt.Sprint("o/", level))); ok {
			held = append(held, ir.OpID(fmt.Sprint("o/", level)))
		}
	}
	onCap, past := ir.OpID(fmt.Sprint("o/", ir.MaxGroupDepth)), ir.OpID(fmt.Sprint("o/", ir.MaxGroupDepth+1))
	e.UpdateOperation(onCap, func(op *ir.Operation) { changed(&op.Docs) })
	e.RemoveOperation(past)
	_, diags := e.Finish()

	assert.Equal(t, walked, held)
	assert.Len(t, held, ir.MaxGroupDepth+1)
	assert.Equal(t, []string{fmt.Sprintf(`internal: edit: remove rejected: no operation %q to remove; an edit adds `+
		`no operation, and reaches none in groups nested deeper than %d levels`, past, ir.MaxGroupDepth)},
		refusals(t, diags))
}

// TestEditor_EditsAGroupSliceInsideItself pins the edit of a group whose groups
// are the slice it sits in, which a compiler bug could build: it is reached
// once, as ForEachOperation reaches it, changed and removed without the edit
// looping or writing the edited document.
func TestEditor_EditsAGroupSliceInsideItself(t *testing.T) {
	t.Parallel()
	loop := []ir.OperationGroup{treeGroup("g/loop", []ir.Operation{treeOp("o/loop")})}
	loop[0].Groups = loop
	doc := &ir.Document{Services: []ir.Service{treeService("s/a", loop...)}}

	e := ir.NewEditor(doc)
	e.UpdateOperation("o/loop", func(op *ir.Operation) { changed(&op.Docs) })
	e.UpdateGroup("g/loop", func(g *ir.OperationGroup) { changed(&g.Docs) })
	updated, _ := e.Finish()
	e = ir.NewEditor(doc)
	e.RemoveGroup("g/loop")
	removed, diags := e.Finish()

	// No check prints a group: one would recurse through the loop on failing.
	require.Empty(t, diags)
	got := updated.Services[0].Groups[0]
	assert.Equal(t, "changed", got.Docs.Summary)
	assert.Equal(t, "changed", got.Operations[0].Docs.Summary)
	if &loop[0] != &got.Groups[0] {
		t.Error("the slice inside the group is not the edited document's")
	}
	if removed.Services[0].Groups != nil {
		t.Errorf("removing the group left %d groups, not a nil slice", len(removed.Services[0].Groups))
	}
	assert.Equal(t, "g/loop", loop[0].Docs.Summary, "the edited document changed")
	assert.Equal(t, "o/loop", loop[0].Operations[0].Docs.Summary, "the edited document changed")
}

// TestEditor_EditsAroundATreeSliceTwoGroupsShare pins the edit of operations
// two groups share a slice of, which a compiler bug could build: an operation
// both reach is held twice, so neither, and one only the longer slice reaches
// is written into a copy of that slice alone.
func TestEditor_EditsAroundATreeSliceTwoGroupsShare(t *testing.T) {
	t.Parallel()
	shared := []ir.Operation{treeOp("o/both"), treeOp("o/one")}
	doc := &ir.Document{Services: []ir.Service{
		treeService("s/a", treeGroup("g/long", shared), treeGroup("g/short", shared[:1])),
	}}

	e := ir.NewEditor(doc)
	e.UpdateOperation("o/both", func(op *ir.Operation) { changed(&op.Docs) })
	e.UpdateOperation("o/one", func(op *ir.Operation) { changed(&op.Docs) })
	out, diags := e.Finish()

	assert.Equal(t, []string{`internal: edit: update rejected: 2 operations have the ID "o/both", ` +
		`so the edit cannot tell which to update`}, refusals(t, diags))
	assert.Equal(t, "s/a[g/long(o/both o/one=changed) g/short(o/both)]", render(out))
	assert.Equal(t, "o/one", shared[1].Docs.Summary, "the edited document changed")
	assert.Same(t, &shared[0], &out.Services[0].Groups[1].Operations[0], "the shorter slice was copied")
}

// groupOf returns a document of one service holding groups groups of n
// operations each, and the operations' IDs in order.
func groupOf(groups, n int) (*ir.Document, []ir.OpID) {
	svc := treeService("s/a")
	ids := make([]ir.OpID, 0, groups*n)
	for g := range groups {
		ops := make([]ir.Operation, 0, n)
		for i := range n {
			id := ir.OpID(fmt.Sprint("o/", g, "/", i))
			ops = append(ops, treeOp(id))
			ids = append(ids, id)
		}
		svc.Groups = append(svc.Groups, treeGroup(ir.GroupID(fmt.Sprint("g/", g)), ops))
	}
	return &ir.Document{Services: []ir.Service{svc}}, ids
}

// TestEditor_TreeWorkIsLinear holds what writes to the tree cost, as nodes
// stepped through and elements copied or moved, to growing linearly with how
// many they write: going from 2n operations to 4n adds no more than two and a
// half times what going from n to 2n adds, so the edit's fixed costs cancel. A
// slice copied at every write, a removal shifting what follows it at once, or
// a settle revisiting what it settled before grows quadratically. Each
// scenario's floor, what any edit must copy or move, holds the count to
// counting.
func TestEditor_TreeWorkIsLinear(t *testing.T) {
	t.Parallel()
	removeEach := func(e *ir.Editor, ids []ir.OpID) {
		for _, id := range ids {
			e.RemoveOperation(id)
		}
	}
	tests := []struct {
		name   string
		groups func(n int) (int, int)
		edit   func(e *ir.Editor, ids []ir.OpID)
		floor  func(n int) int
	}{
		{"empty one group", func(n int) (int, int) { return 1, n }, removeEach, func(n int) int { return n }},
		{"empty one group from its end", func(n int) (int, int) { return 1, n }, func(e *ir.Editor, ids []ir.OpID) {
			for _, id := range slices.Backward(ids) {
				e.RemoveOperation(id)
			}
		}, func(n int) int { return n }},
		{"remove every other operation", func(n int) (int, int) { return 1, n }, func(e *ir.Editor, ids []ir.OpID) {
			for i, id := range ids {
				if i%2 == 0 {
					e.RemoveOperation(id)
				}
			}
		}, func(n int) int { return n + n/2 }},
		{"update every operation", func(n int) (int, int) { return 1, n }, func(e *ir.Editor, ids []ir.OpID) {
			for _, id := range ids {
				e.UpdateOperation(id, func(op *ir.Operation) { changed(&op.Docs) })
			}
		}, func(n int) int { return n }},
		{"empty many groups", func(n int) (int, int) { return n, 2 }, removeEach, func(n int) int { return 2 * n }},
		{"empty many groups, reading after each removal", func(n int) (int, int) { return n, 2 },
			func(e *ir.Editor, ids []ir.OpID) {
				for _, id := range ids {
					e.RemoveOperation(id)
					_, _ = e.Service("s/a")
				}
			}, func(n int) int { return 2 * n }},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			work := func(n int) int {
				doc, ids := groupOf(tt.groups(n))
				e := ir.NewEditor(doc)
				tt.edit(e, ids)
				_, diags := e.Finish()
				require.Empty(t, diags)
				w := ir.TreeWork(e)
				require.GreaterOrEqual(t, w, tt.floor(n), "at %d operations the count misses what any edit copies or moves", n)
				return w
			}
			w1, w2, w4 := work(128), work(256), work(512)
			assert.LessOrEqual(t, 2*(w4-w2), 5*(w2-w1), "work %d, %d, %d grows faster than the operations", w1, w2, w4)
		})
	}
}

// TestEditor_TreeWorkCountsEveryStep holds the count TestEditor_TreeWorkIsLinear
// reads to every step of one removal, so no step's cost goes uncounted.
// Removing o/a1 from treeDoc settles g/a: 2 nodes stepped through to it, the
// services and s/a's groups copied (2 and 2), g/a's 1 group and 3 operations
// scanned, its operations copied (3) and the 2 left moved.
func TestEditor_TreeWorkCountsEveryStep(t *testing.T) {
	t.Parallel()
	e := ir.NewEditor(treeDoc())
	e.RemoveOperation("o/a1")
	_, diags := e.Finish()

	require.Empty(t, diags)
	assert.Equal(t, 2+2+2+1+3+3+2, ir.TreeWork(e))
}

// TestEditor_TreeEditsAgreeWithAPlainRewrite runs random edits over random
// trees and holds every result to the same edits applied, without an edit,
// to a copy of the tree: what each write leaves, what every read returns
// after it, and the edited document left as it was.
func TestEditor_TreeEditsAgreeWithAPlainRewrite(t *testing.T) {
	t.Parallel()
	for seed := range uint64(300) {
		r := rand.New(rand.NewPCG(seed, 1098))
		doc := randomTree(r)
		before := render(doc)
		model := &ir.Document{Services: cloneServices(doc.Services)}
		e := ir.NewEditor(doc)
		for step := range 24 {
			ids := treeIDs(model)
			if len(ids) == 0 {
				break
			}
			pick := ids[r.IntN(len(ids))]
			remove := r.IntN(3) == 0
			applyEdit(e, pick, remove, fmt.Sprint(step))
			applyRewrite(model, pick, remove, fmt.Sprint(step))
			requireReadsAgree(t, e, model, fmt.Sprintf("seed %d step %d (%s, remove %v)", seed, step, pick.id, remove))
		}
		out, diags := e.Finish()
		require.Empty(t, diags, "seed %d", seed)
		require.Equal(t, render(model), render(out), "seed %d", seed)
		require.Equal(t, before, render(doc), "seed %d: the edited document changed", seed)
	}
}

// treeID is a node of the tree by class and ID.
type treeID struct {
	class string
	id    string
}

// randomTree returns a tree of up to three services, each group holding up to
// three operations and up to two groups, three levels deep at most, every ID
// distinct.
func randomTree(r *rand.Rand) *ir.Document {
	next := 0
	id := func(prefix string) string {
		next++
		return fmt.Sprint(prefix, next)
	}
	var groups func(depth int) []ir.OperationGroup
	groups = func(depth int) []ir.OperationGroup {
		if depth == 3 {
			return nil
		}
		var out []ir.OperationGroup
		for range r.IntN(3) {
			var ops []ir.Operation
			for range r.IntN(4) {
				ops = append(ops, treeOp(ir.OpID(id("o/"))))
			}
			out = append(out, treeGroup(ir.GroupID(id("g/")), ops, groups(depth+1)...))
		}
		return out
	}
	doc := &ir.Document{}
	for range 1 + r.IntN(3) {
		s := id("s/")
		doc.Services = append(doc.Services, treeService(ir.ServiceID(s), groups(0)...))
	}
	return doc
}

// cloneServices returns a copy of services sharing nothing the test writes.
func cloneServices(services []ir.Service) []ir.Service {
	out := slices.Clone(services)
	for i := range out {
		out[i].Groups = cloneGroups(out[i].Groups)
	}
	return out
}

// cloneGroups returns a copy of groups sharing nothing the test writes.
func cloneGroups(groups []ir.OperationGroup) []ir.OperationGroup {
	out := slices.Clone(groups)
	for i := range out {
		out[i].Operations = slices.Clone(out[i].Operations)
		out[i].Groups = cloneGroups(out[i].Groups)
	}
	return out
}

// treeIDs returns every node of doc's tree, in walk order.
func treeIDs(doc *ir.Document) []treeID {
	var ids []treeID
	var walk func([]ir.OperationGroup)
	walk = func(groups []ir.OperationGroup) {
		for _, g := range groups {
			ids = append(ids, treeID{"group", string(g.ID)})
			for _, op := range g.Operations {
				ids = append(ids, treeID{"operation", string(op.ID)})
			}
			walk(g.Groups)
		}
	}
	for _, s := range doc.Services {
		ids = append(ids, treeID{"service", string(s.ID)})
		walk(s.Groups)
	}
	return ids
}

// applyEdit removes the node at pick through e, or appends mark to its summary.
func applyEdit(e *ir.Editor, pick treeID, remove bool, mark string) {
	switch {
	case pick.class == "service" && remove:
		e.RemoveService(ir.ServiceID(pick.id))
	case pick.class == "service":
		e.UpdateService(ir.ServiceID(pick.id), func(s *ir.Service) { s.Docs.Summary += mark })
	case pick.class == "group" && remove:
		e.RemoveGroup(ir.GroupID(pick.id))
	case pick.class == "group":
		e.UpdateGroup(ir.GroupID(pick.id), func(g *ir.OperationGroup) { g.Docs.Summary += mark })
	case remove:
		e.RemoveOperation(ir.OpID(pick.id))
	default:
		e.UpdateOperation(ir.OpID(pick.id), func(op *ir.Operation) { op.Docs.Summary += mark })
	}
}

// applyRewrite does to doc, in place, what applyEdit does through an edit.
func applyRewrite(doc *ir.Document, pick treeID, remove bool, mark string) {
	if pick.class == "service" {
		i := slices.IndexFunc(doc.Services, func(s ir.Service) bool { return string(s.ID) == pick.id })
		if remove {
			doc.Services = slices.Delete(doc.Services, i, i+1)
		} else {
			doc.Services[i].Docs.Summary += mark
		}
		return
	}
	for i := range doc.Services {
		rewriteGroups(&doc.Services[i].Groups, pick, remove, mark)
	}
}

// rewriteGroups does to the groups at groups, and what they hold, what
// applyRewrite does.
func rewriteGroups(groups *[]ir.OperationGroup, pick treeID, remove bool, mark string) {
	for i := range *groups {
		g := &(*groups)[i]
		if pick.class == "group" && string(g.ID) == pick.id {
			if remove {
				*groups = slices.Delete(*groups, i, i+1)
				return
			}
			g.Docs.Summary += mark
			return
		}
		if j := slices.IndexFunc(g.Operations, func(op ir.Operation) bool { return string(op.ID) == pick.id }); j >= 0 &&
			pick.class == "operation" {
			if remove {
				g.Operations = slices.Delete(g.Operations, j, j+1)
			} else {
				g.Operations[j].Docs.Summary += mark
			}
			return
		}
		rewriteGroups(&g.Groups, pick, remove, mark)
	}
}

// requireReadsAgree holds every read of e to the node model holds at its ID,
// spelled by render, and to holding none where model holds none.
func requireReadsAgree(t *testing.T, e *ir.Editor, model *ir.Document, at string) {
	t.Helper()
	held := map[treeID]bool{}
	for _, id := range treeIDs(model) {
		held[id] = true
	}
	for _, s := range model.Services {
		got, ok := e.Service(s.ID)
		require.True(t, ok, "%s: service %s", at, s.ID)
		require.Equal(t, render(&ir.Document{Services: []ir.Service{s}}),
			render(&ir.Document{Services: []ir.Service{got}}), "%s: service %s", at, s.ID)
	}
	var check func([]ir.OperationGroup)
	check = func(groups []ir.OperationGroup) {
		for _, g := range groups {
			got, ok := e.Group(g.ID)
			require.True(t, ok, "%s: group %s", at, g.ID)
			require.Equal(t, renderGroups([]ir.OperationGroup{g}, 8), renderGroups([]ir.OperationGroup{got}, 8),
				"%s: group %s", at, g.ID)
			for _, op := range g.Operations {
				got, ok := e.Operation(op.ID)
				require.True(t, ok, "%s: operation %s", at, op.ID)
				require.Equal(t, op.Docs, got.Docs, "%s: operation %s", at, op.ID)
			}
			check(g.Groups)
		}
	}
	for _, s := range model.Services {
		check(s.Groups)
	}
	for i := range 64 {
		for _, class := range []string{"service", "group", "operation"} {
			id := treeID{class, fmt.Sprint(class[:1], "/", i)}
			if held[id] {
				continue
			}
			_, sOK := e.Service(ir.ServiceID(id.id))
			_, gOK := e.Group(ir.GroupID(id.id))
			_, oOK := e.Operation(ir.OpID(id.id))
			require.False(t, class == "service" && sOK || class == "group" && gOK || class == "operation" && oOK,
				"%s: %s %s is held though the model holds none", at, class, id.id)
		}
	}
}
