// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

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

// treeOperation returns an operation whose summary is its ID and whose one parameter
// is named after it.
func treeOperation(id ir.OperationID) ir.Operation {
	return ir.Operation{ID: id, Docs: ir.Docs{Summary: string(id)}, Params: []ir.Parameter{{ID: ir.ParameterID(id + "/p")}}}
}

// treeGroup returns a group whose summary is its ID.
func treeGroup(id ir.GroupID, operations []ir.Operation, groups ...ir.OperationGroup) ir.OperationGroup {
	return ir.OperationGroup{ID: id, Docs: ir.Docs{Summary: string(id)}, Operations: operations, Groups: groups}
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
		treeService("service/a",
			treeGroup("operation-group/a", []ir.Operation{treeOperation("o/a1"), treeOperation("o/a2"), treeOperation("o/a3")},
				treeGroup("operation-group/a/n", []ir.Operation{treeOperation("o/n")})),
			treeGroup("operation-group/b", []ir.Operation{treeOperation("o/b")})),
		treeService("service/b", treeGroup("operation-group/c", []ir.Operation{treeOperation("o/c")})),
	}}
}

// render spells the service tree of doc: a service as its ID and its groups in
// brackets, a group as its ID, its operations in parentheses and its groups in
// brackets, and an operation as its ID, each node's summary after an equals
// sign when it is not the node's ID. Nesting past depth levels is spelled as
// an ellipsis.
func render(doc *ir.Document) string {
	parts := make([]string, 0, len(doc.Services))
	for _, service := range doc.Services {
		parts = append(parts, named(string(service.ID), service.Docs)+"["+renderGroups(service.Groups, 8)+"]")
	}
	return strings.Join(parts, " ")
}

// renderGroups spells groups as render does, depth levels deep.
func renderGroups(groups []ir.OperationGroup, depth int) string {
	if depth == 0 {
		return "..."
	}
	parts := make([]string, 0, len(groups))
	for _, operationGroup := range groups {
		operations := make([]string, 0, len(operationGroup.Operations))
		for _, operation := range operationGroup.Operations {
			operations = append(operations, named(string(operation.ID), operation.Docs))
		}
		part := named(string(operationGroup.ID), operationGroup.Docs) + "(" + strings.Join(operations, " ") + ")"
		if len(operationGroup.Groups) > 0 {
			part += "[" + renderGroups(operationGroup.Groups, depth-1) + "]"
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
	updateOperation := func(id ir.OperationID) func(*ir.Editor) {
		return func(editor *ir.Editor) {
			editor.UpdateOperation(id, func(operation *ir.Operation) { changed(&operation.Docs) })
		}
	}
	tests := []struct {
		name  string
		write func(*ir.Editor)
		want  string
	}{
		{"update an operation", updateOperation("o/a2"),
			"service/a[operation-group/a(o/a1 o/a2=changed o/a3)[operation-group/a/n(o/n)] operation-group/b(o/b)] service/b[operation-group/c(o/c)]"},
		{"update a group", func(editor *ir.Editor) {
			editor.UpdateGroup("operation-group/a", func(operationGroup *ir.OperationGroup) { changed(&operationGroup.Docs) })
		}, "service/a[operation-group/a=changed(o/a1 o/a2 o/a3)[operation-group/a/n(o/n)] operation-group/b(o/b)] service/b[operation-group/c(o/c)]"},
		{"update a nested group", func(editor *ir.Editor) {
			editor.UpdateGroup("operation-group/a/n", func(operationGroup *ir.OperationGroup) { changed(&operationGroup.Docs) })
		}, "service/a[operation-group/a(o/a1 o/a2 o/a3)[operation-group/a/n=changed(o/n)] operation-group/b(o/b)] service/b[operation-group/c(o/c)]"},
		{"update a service", func(editor *ir.Editor) {
			editor.UpdateService("service/b", func(service *ir.Service) { changed(&service.Docs) })
		}, "service/a[operation-group/a(o/a1 o/a2 o/a3)[operation-group/a/n(o/n)] operation-group/b(o/b)] service/b=changed[operation-group/c(o/c)]"},
		{"remove an operation", func(editor *ir.Editor) { editor.RemoveOperation("o/a1") },
			"service/a[operation-group/a(o/a2 o/a3)[operation-group/a/n(o/n)] operation-group/b(o/b)] service/b[operation-group/c(o/c)]"},
		{"remove a group with what it holds", func(editor *ir.Editor) { editor.RemoveGroup("operation-group/a") },
			"service/a[operation-group/b(o/b)] service/b[operation-group/c(o/c)]"},
		{"remove a nested group", func(editor *ir.Editor) { editor.RemoveGroup("operation-group/a/n") },
			"service/a[operation-group/a(o/a1 o/a2 o/a3) operation-group/b(o/b)] service/b[operation-group/c(o/c)]"},
		{"remove a service", func(editor *ir.Editor) { editor.RemoveService("service/a") }, "service/b[operation-group/c(o/c)]"},
		{"remove a group's last operation, which leaves the group", func(editor *ir.Editor) { editor.RemoveOperation("o/b") },
			"service/a[operation-group/a(o/a1 o/a2 o/a3)[operation-group/a/n(o/n)] operation-group/b()] service/b[operation-group/c(o/c)]"},
		{"remove a service's last group, which leaves the service", func(editor *ir.Editor) { editor.RemoveGroup("operation-group/c") },
			"service/a[operation-group/a(o/a1 o/a2 o/a3)[operation-group/a/n(o/n)] operation-group/b(o/b)] service/b[]"},
		{"update past a removal", func(editor *ir.Editor) {
			editor.RemoveOperation("o/a1")
			updateOperation("o/a3")(editor)
			editor.RemoveOperation("o/a2")
		}, "service/a[operation-group/a(o/a3=changed)[operation-group/a/n(o/n)] operation-group/b(o/b)] service/b[operation-group/c(o/c)]"},
		{"update before a removal", func(editor *ir.Editor) {
			updateOperation("o/a3")(editor)
			editor.RemoveOperation("o/a1")
			editor.UpdateOperation("o/a3", func(operation *ir.Operation) { operation.Docs.Summary += " twice" })
		}, "service/a[operation-group/a(o/a2 o/a3=changed twice)[operation-group/a/n(o/n)] operation-group/b(o/b)] service/b[operation-group/c(o/c)]"},
		{"update a group past a removed sibling", func(editor *ir.Editor) {
			editor.RemoveGroup("operation-group/a")
			editor.UpdateGroup("operation-group/b", func(operationGroup *ir.OperationGroup) { changed(&operationGroup.Docs) })
			updateOperation("o/b")(editor)
		}, "service/a[operation-group/b=changed(o/b=changed)] service/b[operation-group/c(o/c)]"},
		{"update a service past a removed one", func(editor *ir.Editor) {
			editor.RemoveService("service/a")
			updateOperation("o/c")(editor)
			editor.UpdateService("service/b", func(service *ir.Service) { changed(&service.Docs) })
		}, "service/b=changed[operation-group/c(o/c=changed)]"},
		{"update a group, then what it holds", func(editor *ir.Editor) {
			editor.UpdateGroup("operation-group/a", func(operationGroup *ir.OperationGroup) { changed(&operationGroup.Docs) })
			updateOperation("o/a1")(editor)
			editor.RemoveGroup("operation-group/a/n")
		}, "service/a[operation-group/a=changed(o/a1=changed o/a2 o/a3) operation-group/b(o/b)] service/b[operation-group/c(o/c)]"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			doc := treeDoc()
			before := irtest.Encoded(t, doc)

			editor := ir.NewEditor(doc)
			tt.write(editor)
			out, diags := editor.Finish()

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
	editor := ir.NewEditor(treeDoc())
	editor.RemoveOperation("o/b")
	editor.RemoveGroup("operation-group/c")
	editor.RemoveService("service/a")
	out, diags := editor.Finish()

	require.Empty(t, diags)
	assert.Equal(t, "service/b[]", render(out))
	assert.Nil(t, out.Services[0].Groups)

	editor = ir.NewEditor(treeDoc())
	editor.RemoveOperation("o/b")
	editor.RemoveService("service/a")
	editor.RemoveService("service/b")
	out, diags = editor.Finish()

	require.Empty(t, diags)
	assert.Nil(t, out.Services)
}

// TestEditor_BuildsOnlyTheTreePathItWrites pins that a write to the tree builds
// the slices on the path to what it changes and nothing else: what an update
// or a removal does not reach stays shared with the edited document, and a
// refused write, or a read after none, builds nothing.
func TestEditor_BuildsOnlyTheTreePathItWrites(t *testing.T) {
	t.Parallel()
	doc := treeDoc()
	editor := ir.NewEditor(doc)
	editor.UpdateOperation("o/a2", func(operation *ir.Operation) { changed(&operation.Docs) })
	out, diags := editor.Finish()
	require.Empty(t, diags)

	in, got := doc.Services[0].Groups, out.Services[0].Groups
	assert.NotSame(t, &doc.Services[0], &out.Services[0], "the services on the path were not built")
	assert.NotSame(t, &in[0], &got[0], "the groups on the path were not built")
	assert.NotSame(t, &in[0].Operations[1], &got[0].Operations[1], "the operations on the path were not built")
	assert.NotSame(t, &in[0].Operations[1].Params[0], &got[0].Operations[1].Params[0], "the change shares the node")
	assert.Same(t, &in[0].Operations[0].Params[0], &got[0].Operations[0].Params[0], "an unchanged operation was copied deep")
	assert.Same(t, &in[0].Groups[0], &got[0].Groups[0], "a group off the path was built")
	assert.Same(t, &in[1].Operations[0], &got[1].Operations[0], "a sibling group's operations were built")
	assert.Same(t, &doc.Services[1].Groups[0], &out.Services[1].Groups[0], "another service's groups were built")

	removed := treeDoc()
	editor = ir.NewEditor(removed)
	editor.RemoveOperation("o/a1")
	out, diags = editor.Finish()
	require.Empty(t, diags)
	assert.Same(t, &removed.Services[0].Groups[0].Groups[0], &out.Services[0].Groups[0].Groups[0],
		"a removal built a slice it drops nothing from")
	assert.Same(t, &removed.Services[1].Groups[0], &out.Services[1].Groups[0], "a removal built another service")

	nested := treeDoc()
	editor = ir.NewEditor(nested)
	editor.RemoveGroup("operation-group/a/n")
	out, diags = editor.Finish()
	require.Empty(t, diags)
	assert.Same(t, &nested.Services[0].Groups[0].Operations[0], &out.Services[0].Groups[0].Operations[0],
		"removing a nested group built the operations beside it")

	read := treeDoc()
	editor = ir.NewEditor(read)
	_, _ = editor.Operation("o/a2")
	_, _ = editor.Group("operation-group/a")
	_, _ = editor.Service("service/a")
	editor.UpdateOperation("o/a2", func(operation *ir.Operation) { operation.ID = "o/moved" })
	editor.UpdateGroup("operation-group/a/n", func(operationGroup *ir.OperationGroup) { operationGroup.ID = "operation-group/moved" })
	editor.UpdateService("service/b", func(service *ir.Service) { service.ID = "service/moved" })
	editor.RemoveOperation("o/absent")
	out, diags = editor.Finish()
	require.Len(t, diags, 4)
	assert.Same(t, &read.Services[0], &out.Services[0], "a read or a refused write built the services")
}

// TestEditor_ReadsTheTreeAsTheEditHoldsIt pins that Operation, Group and
// Service answer with what the edit holds: a node changed or emptied as
// written, no node once removed or held inside one removed, and what the
// edited document holds otherwise, inside a change as outside one.
func TestEditor_ReadsTheTreeAsTheEditHoldsIt(t *testing.T) {
	t.Parallel()
	doc := treeDoc()
	editor := ir.NewEditor(doc)
	editor.UpdateOperation("o/a2", func(operation *ir.Operation) { changed(&operation.Docs) })
	editor.RemoveOperation("o/a1")
	editor.RemoveGroup("operation-group/a/n")
	editor.RemoveService("service/b")

	irOperation, held := editor.Operation("o/a2")
	assert.True(t, held)
	assert.Equal(t, "changed", irOperation.Docs.Summary)
	for _, id := range []ir.OperationID{"o/a1", "o/n", "o/c", "o/absent"} {
		_, held = editor.Operation(id)
		assert.False(t, held, "operation %s", id)
	}
	group, held := editor.Group("operation-group/a")
	require.True(t, held)
	assert.Equal(t, "operation-group/a(o/a2=changed o/a3)", renderGroups([]ir.OperationGroup{group}, 8))
	for _, id := range []ir.GroupID{"operation-group/a/n", "operation-group/c", "operation-group/absent"} {
		_, held = editor.Group(id)
		assert.False(t, held, "group %s", id)
	}
	svc, held := editor.Service("service/a")
	require.True(t, held)
	assert.Equal(t, "service/a[operation-group/a(o/a2=changed o/a3) operation-group/b(o/b)]", render(&ir.Document{Services: []ir.Service{svc}}))
	_, held = editor.Service("service/b")
	assert.False(t, held)
	untouched, held := editor.Operation("o/b")
	assert.True(t, held)
	assert.Equal(t, doc.Services[0].Groups[1].Operations[0].Docs, untouched.Docs)

	editor.UpdateGroup("operation-group/b", func(operationGroup *ir.OperationGroup) {
		read, inEdit := editor.Operation("o/a2")
		operationGroup.Docs.Summary = fmt.Sprint(inEdit, " ", read.Docs.Summary)
	})
	group, _ = editor.Group("operation-group/b")
	assert.Equal(t, "true changed", group.Docs.Summary)
	_, diags := editor.Finish()
	assert.Empty(t, diags)
}

// TestEditor_AReadIsNotChangedByWhatFollows pins that what a read of a group
// or service returns stays what the edit held then, whatever the edit does
// next, and whether or not it had written there before the read: a pass may
// range over a read while it edits. The writes before a read differ by case,
// since a slice the edit already built is the one it could write in place.
func TestEditor_AReadIsNotChangedByWhatFollows(t *testing.T) {
	t.Parallel()
	before := map[string]func(*ir.Editor){
		"nothing written": func(*ir.Editor) {},
		"an operation updated": func(editor *ir.Editor) {
			editor.UpdateOperation("o/a3", func(operation *ir.Operation) { changed(&operation.Docs) })
		},
		"an operation removed and read": func(editor *ir.Editor) {
			editor.RemoveOperation("o/a3")
			_, _ = editor.Group("operation-group/a")
		},
	}
	after := map[string]func(*ir.Editor){
		"an update": func(editor *ir.Editor) {
			editor.UpdateOperation("o/a1", func(operation *ir.Operation) { operation.Docs.Summary = "later" })
		},
		"a removal":          func(editor *ir.Editor) { editor.RemoveOperation("o/a1") },
		"a removal and read": func(editor *ir.Editor) { editor.RemoveOperation("o/a1"); _, _ = editor.Group("operation-group/a") },
		"a nested removal":   func(editor *ir.Editor) { editor.RemoveGroup("operation-group/a/n"); _, _ = editor.Service("service/a") },
		"the end":            func(editor *ir.Editor) { editor.RemoveOperation("o/a1"); _, _ = editor.Finish() },
	}
	for first, write := range before {
		for then, next := range after {
			t.Run(first+", then "+then, func(t *testing.T) {
				t.Parallel()
				editor := ir.NewEditor(treeDoc())
				write(editor)
				group, held := editor.Group("operation-group/a")
				require.True(t, held)
				svc, held := editor.Service("service/a")
				require.True(t, held)
				wantGroup := renderGroups([]ir.OperationGroup{group}, 8)
				wantService := render(&ir.Document{Services: []ir.Service{svc}})

				next(editor)

				assert.Equal(t, wantGroup, renderGroups([]ir.OperationGroup{group}, 8))
				assert.Equal(t, wantService, render(&ir.Document{Services: []ir.Service{svc}}))
			})
		}
	}
}

// TestEditor_RangingOverAReadSeesEveryNode pins the loop that read being kept
// allows: removing each operation of a group read before, with a read between,
// meets every operation once and no zero one.
func TestEditor_RangingOverAReadSeesEveryNode(t *testing.T) {
	t.Parallel()
	editor := ir.NewEditor(treeDoc())
	editor.UpdateOperation("o/a3", func(operation *ir.Operation) { changed(&operation.Docs) })
	group, held := editor.Group("operation-group/a")
	require.True(t, held)

	seen := make([]ir.OperationID, 0, len(group.Operations))
	for _, operation := range group.Operations {
		seen = append(seen, operation.ID)
		editor.RemoveOperation(operation.ID)
		_, _ = editor.Group("operation-group/a")
	}
	out, diags := editor.Finish()

	require.Empty(t, diags)
	assert.Equal(t, []ir.OperationID{"o/a1", "o/a2", "o/a3"}, seen)
	assert.Equal(t, "service/a[operation-group/a()[operation-group/a/n(o/n)] operation-group/b(o/b)] service/b[operation-group/c(o/c)]", render(out))
}

// TestEditor_UpdatesItsNodeWhenItsChangeReadsTheTree pins an update whose
// change reads a group or service, which can build that node's slices anew: the
// change lands beside what the read built, not beside the slices the node held
// before it, and the edited document is left as it was.
func TestEditor_UpdatesItsNodeWhenItsChangeReadsTheTree(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name  string
		write func(*ir.Editor)
		want  string
	}{
		{"an operation after one removed before it", func(editor *ir.Editor) {
			editor.RemoveOperation("o/a1")
			editor.UpdateOperation("o/a3", func(operation *ir.Operation) {
				_, _ = editor.Group("operation-group/a")
				changed(&operation.Docs)
			})
		}, "service/a[operation-group/a(o/a2 o/a3=changed)[operation-group/a/n(o/n)] operation-group/b(o/b)] service/b[operation-group/c(o/c)]"},
		{"a group after one removed before it", func(editor *ir.Editor) {
			editor.RemoveGroup("operation-group/a")
			editor.UpdateGroup("operation-group/b", func(operationGroup *ir.OperationGroup) {
				_, _ = editor.Service("service/a")
				changed(&operationGroup.Docs)
			})
		}, "service/a[operation-group/b=changed(o/b)] service/b[operation-group/c(o/c)]"},
		{"a service after one removed before it", func(editor *ir.Editor) {
			editor.RemoveService("service/a")
			editor.UpdateService("service/b", func(service *ir.Service) {
				_, _ = editor.Service("service/b")
				changed(&service.Docs)
			})
		}, "service/b=changed[operation-group/c(o/c)]"},
		{"a group whose own read builds what it holds", func(editor *ir.Editor) {
			editor.RemoveOperation("o/a1")
			editor.UpdateGroup("operation-group/a", func(operationGroup *ir.OperationGroup) {
				_, _ = editor.Group("operation-group/a")
				changed(&operationGroup.Docs)
			})
		}, "service/a[operation-group/a=changed(o/a2 o/a3)[operation-group/a/n(o/n)] operation-group/b(o/b)] service/b[operation-group/c(o/c)]"},
		{"a service whose own read builds what it holds", func(editor *ir.Editor) {
			editor.RemoveGroup("operation-group/a")
			editor.UpdateService("service/a", func(service *ir.Service) {
				_, _ = editor.Service("service/a")
				changed(&service.Docs)
			})
		}, "service/a=changed[operation-group/b(o/b)] service/b[operation-group/c(o/c)]"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			doc := treeDoc()
			editor := ir.NewEditor(doc)
			tt.write(editor)
			out, diags := editor.Finish()

			require.Empty(t, diags)
			assert.Equal(t, tt.want, render(out))
			assert.Equal(t, irtest.Encoded(t, treeDoc()), irtest.Encoded(t, doc), "the edited document changed")
		})
	}
}

// TestEditor_AChangeKeepingItsNodeMovesNothing pins what a change that kept the
// copy it was handed can do once its update is over: writing the copy's own
// fields reaches nothing the edit holds, so it cannot move a node off its ID.
func TestEditor_AChangeKeepingItsNodeMovesNothing(t *testing.T) {
	t.Parallel()
	editor := ir.NewEditor(treeDoc())
	var (
		irOperation *ir.Operation
		grp         *ir.OperationGroup
		svc         *ir.Service
	)
	editor.UpdateOperation("o/a1", func(operation *ir.Operation) { irOperation = operation })
	editor.UpdateGroup("operation-group/b", func(operationGroup *ir.OperationGroup) { grp = operationGroup })
	editor.UpdateService("service/b", func(service *ir.Service) { svc = service })
	irOperation.ID, grp.ID, svc.ID = "o/moved", "operation-group/moved", "service/moved"
	changed(&irOperation.Docs)
	changed(&grp.Docs)
	changed(&svc.Docs)
	out, diags := editor.Finish()

	require.Empty(t, diags)
	assert.Equal(t, render(treeDoc()), render(out))
}

// TestEditor_FinishInsideAChangeSettlesTheTree pins the document Finish returns
// from inside a change, which a pass could return as its own result: it holds
// no node the edit removed, as the one the edit ends with does not.
func TestEditor_FinishInsideAChangeSettlesTheTree(t *testing.T) {
	t.Parallel()
	editor := ir.NewEditor(treeDoc())
	editor.RemoveOperation("o/a1")
	var inner string
	editor.UpdateOperation("o/a2", func(*ir.Operation) {
		doc, _ := editor.Finish()
		inner = render(doc)
	})
	out, _ := editor.Finish()

	const want = "service/a[operation-group/a(o/a2 o/a3)[operation-group/a/n(o/n)] operation-group/b(o/b)] service/b[operation-group/c(o/c)]"
	assert.Equal(t, want, inner)
	assert.Equal(t, want, render(out))
}

// TestEditor_RefusesTreeWritesOnlyABugMakes pins each write to the tree
// refused for what it asks alone: the one message Finish reports, and that the
// refused write leaves the edit's document as a control edit making only the
// writes before it leaves its own.
func TestEditor_RefusesTreeWritesOnlyABugMakes(t *testing.T) {
	t.Parallel()
	removeGroup := func(editor *ir.Editor) { editor.RemoveGroup("operation-group/a") }
	tests := []struct {
		name    string
		before  func(*ir.Editor)
		refused func(*ir.Editor)
		want    string
	}{
		{"update an absent operation", nil, func(editor *ir.Editor) { editor.UpdateOperation("o/x", func(*ir.Operation) {}) },
			`update rejected: no operation "o/x" to update; an edit adds no operation`},
		{"remove an absent group", nil, func(editor *ir.Editor) { editor.RemoveGroup("operation-group/x") },
			`remove rejected: no group "operation-group/x" to remove; an edit adds no group`},
		{"update an absent service", nil, func(editor *ir.Editor) { editor.UpdateService("service/x", func(*ir.Service) {}) },
			`update rejected: no service "service/x" to update; an edit adds no service`},
		{"remove a removed operation", func(editor *ir.Editor) { editor.RemoveOperation("o/b") },
			func(editor *ir.Editor) { editor.RemoveOperation("o/b") }, `remove rejected: no operation "o/b" to remove`},
		{"update an operation inside a removed group", removeGroup,
			func(editor *ir.Editor) { editor.UpdateOperation("o/n", func(*ir.Operation) {}) }, `update rejected: no operation "o/n"`},
		{"remove a group inside a removed group", removeGroup,
			func(editor *ir.Editor) { editor.RemoveGroup("operation-group/a/n") }, `remove rejected: no group "operation-group/a/n"`},
		{"remove a removed service", func(editor *ir.Editor) { editor.RemoveService("service/b") },
			func(editor *ir.Editor) { editor.RemoveService("service/b") }, `remove rejected: no service "service/b"`},
		{"update an operation with no change", nil, func(editor *ir.Editor) { editor.UpdateOperation("o/a1", nil) },
			`update rejected: no change for "o/a1"`},
		{"update a group with no change", nil, func(editor *ir.Editor) { editor.UpdateGroup("operation-group/a", nil) },
			`update rejected: no change for "operation-group/a"`},
		{"update moving an operation", nil, func(editor *ir.Editor) {
			editor.UpdateOperation("o/a1", func(operation *ir.Operation) {
				operation.ID = "o/moved"
				changed(&operation.Docs)
			})
		}, `update rejected: the change moved "o/a1" to "o/moved"; a node keeps its ID`},
		{"update emptying a group's ID", nil, func(editor *ir.Editor) {
			editor.UpdateGroup("operation-group/b", func(operationGroup *ir.OperationGroup) { *operationGroup = ir.OperationGroup{} })
		}, `update rejected: the change moved "operation-group/b" to ""; a node keeps its ID`},
		{"update moving a service", nil, func(editor *ir.Editor) {
			editor.UpdateService("service/b", func(service *ir.Service) { service.ID = "service/a" })
		}, `update rejected: the change moved "service/b" to "service/a"; a node keeps its ID`},
		{"update setting a service's groups", nil, func(editor *ir.Editor) {
			editor.UpdateService("service/a", func(service *ir.Service) { service.Groups = []ir.OperationGroup{} })
		}, `update rejected: the change of "service/a" set its Groups, whose nodes the edit changes by their own IDs`},
		{"update setting a group's groups", nil, func(editor *ir.Editor) {
			editor.UpdateGroup("operation-group/b", func(operationGroup *ir.OperationGroup) {
				operationGroup.Groups = []ir.OperationGroup{treeGroup("operation-group/new", nil)}
			})
		}, `update rejected: the change of "operation-group/b" set its Groups`},
		{"update setting a group's operations", nil, func(editor *ir.Editor) {
			editor.UpdateGroup("operation-group/a", func(operationGroup *ir.OperationGroup) {
				changed(&operationGroup.Docs)
				operationGroup.Operations = []ir.Operation{}
			})
		}, `update rejected: the change of "operation-group/a" set its Operations`},
		{"update adding a parameter", nil, func(editor *ir.Editor) {
			editor.UpdateOperation("o/a1", func(operation *ir.Operation) { operation.Params = append(operation.Params, ir.Parameter{ID: "o/x/p"}) })
		}, `update rejected: the change of "o/a1" declares the ir.ParameterID "o/x/p", which the node does not; ` +
			`an edit adds no ID`},
		{"update after a refused update", func(editor *ir.Editor) { editor.UpdateGroup("operation-group/a", nil) }, func(editor *ir.Editor) {
			editor.UpdateService("service/b", func(service *ir.Service) { service.Groups = nil; service.ID = "service/c" })
		}, `update rejected: the change moved "service/b" to "service/c"`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			doc, control := treeDoc(), treeDoc()
			editor, editorValue := ir.NewEditor(doc), ir.NewEditor(control)
			if tt.before != nil {
				tt.before(editor)
				tt.before(editorValue)
			}
			tt.refused(editor)

			out, diags := editor.Finish()
			want, ctrl := editorValue.Finish()

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
		treeService("service/x",
			treeGroup("operation-group/x", []ir.Operation{treeOperation("o/x")}),
			treeGroup("operation-group/y", []ir.Operation{treeOperation("o/x"), treeOperation("o/y")})),
		treeService("service/x"),
	}}

	editor := ir.NewEditor(doc)
	editor.UpdateOperation("o/x", func(operation *ir.Operation) { changed(&operation.Docs) })
	editor.RemoveService("service/x")
	_, operationHeld := editor.Operation("o/x")
	_, svcHeld := editor.Service("service/x")
	editor.RemoveGroup("operation-group/x")
	editor.UpdateOperation("o/x", func(operation *ir.Operation) { changed(&operation.Docs) })
	irOperation, nowHeld := editor.Operation("o/x")
	out, diags := editor.Finish()

	assert.Equal(t, []string{
		`internal: edit: update rejected: 2 operations have the ID "o/x", so the edit cannot tell which to update`,
		`internal: edit: remove rejected: 2 services have the ID "service/x", so the edit cannot tell which to remove`,
	}, refusals(t, diags))
	assert.False(t, operationHeld)
	assert.False(t, svcHeld)
	assert.True(t, nowHeld, "removing one of two operations at an ID leaves the other held")
	assert.Equal(t, "changed", irOperation.Docs.Summary)
	assert.Equal(t, "service/x[operation-group/y(o/x=changed o/y)] service/x[]", render(out))
}

// nestedGroups returns a service whose groups nest levels deep, one operation
// o/<level> in each, the top-level group at level 0.
func nestedGroups(levels int) ir.Service {
	var inner []ir.OperationGroup
	for level := levels - 1; level >= 0; level-- {
		id := fmt.Sprint(level)
		inner = []ir.OperationGroup{{ID: ir.GroupID("operation-group/" + id), Groups: inner,
			Operations: []ir.Operation{{ID: ir.OperationID("o/" + id)}}}}
	}
	return ir.Service{ID: "service/deep", Groups: inner}
}

// TestEditor_ReachesTheTreeForEachOperationReaches pins the edit's reach to
// ForEachOperation's: an operation in a group nested past MaxGroupDepth is
// neither read nor written, and a write naming one says the cap is why, while
// one on the cap is reached.
func TestEditor_ReachesTheTreeForEachOperationReaches(t *testing.T) {
	t.Parallel()
	doc := &ir.Document{Services: []ir.Service{nestedGroups(ir.MaxGroupDepth + 2)}}
	var walked []ir.OperationID
	require.True(t, ir.ForEachOperation(doc, func(operation ir.Operation) { walked = append(walked, operation.ID) }))

	editor := ir.NewEditor(doc)
	var held []ir.OperationID
	for level := range ir.MaxGroupDepth + 2 {
		if _, ok := editor.Operation(ir.OperationID(fmt.Sprint("o/", level))); ok {
			held = append(held, ir.OperationID(fmt.Sprint("o/", level)))
		}
	}
	onCap, past := ir.OperationID(fmt.Sprint("o/", ir.MaxGroupDepth)), ir.OperationID(fmt.Sprint("o/", ir.MaxGroupDepth+1))
	editor.UpdateOperation(onCap, func(operation *ir.Operation) { changed(&operation.Docs) })
	editor.RemoveOperation(past)
	out, diags := editor.Finish()

	assert.Equal(t, walked, held)
	assert.Len(t, held, ir.MaxGroupDepth+1)
	last := out.Services[0].Groups
	for range ir.MaxGroupDepth {
		last = last[0].Groups
	}
	assert.Len(t, last[0].Groups, 1, "the update on the cap dropped the group past it, which the edit never read")
	summaries := map[ir.OperationID]string{}
	ir.ForEachOperation(out, func(operation ir.Operation) { summaries[operation.ID] = operation.Docs.Summary })
	assert.Equal(t, "changed", summaries[onCap], "the update on the cap did not land")
	ir.ForEachOperation(doc, func(operation ir.Operation) { assert.Empty(t, operation.Docs.Summary, "the edited document changed") })
	got := refusals(t, diags)
	require.Len(t, got, 1)
	assert.Contains(t, got[0], fmt.Sprintf(`internal: edit: remove rejected: no operation %q to remove; an edit adds `+
		`no operation, and reaches none in groups`, past))
	assert.Contains(t, got[0], fmt.Sprint(ir.MaxGroupDepth), "the refusal names the depth bound")
	assert.Contains(t, got[0], fmt.Sprint(ir.MaxWalkSteps), "and the step bound beside it")
}

// TestEditor_EditsAGroupSliceInsideItself pins the edit of a group whose groups
// are the slice it sits in, which a compiler bug could build: it is reached
// once, as ForEachOperation reaches it, changed and removed without the edit
// looping or writing the edited document.
func TestEditor_EditsAGroupSliceInsideItself(t *testing.T) {
	t.Parallel()
	loop := []ir.OperationGroup{treeGroup("operation-group/loop", []ir.Operation{treeOperation("o/loop")})}
	loop[0].Groups = loop
	doc := &ir.Document{Services: []ir.Service{treeService("service/a", loop...)}}

	editor := ir.NewEditor(doc)
	editor.UpdateOperation("o/loop", func(operation *ir.Operation) { changed(&operation.Docs) })
	editor.UpdateGroup("operation-group/loop", func(operationGroup *ir.OperationGroup) { changed(&operationGroup.Docs) })
	updated, _ := editor.Finish()
	editor = ir.NewEditor(doc)
	editor.RemoveGroup("operation-group/loop")
	removed, diags := editor.Finish()

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
	assert.Equal(t, "operation-group/loop", loop[0].Docs.Summary, "the edited document changed")
	assert.Equal(t, "o/loop", loop[0].Operations[0].Docs.Summary, "the edited document changed")
}

// TestEditor_EditsAroundATreeSliceTwoGroupsShare pins the edit of operations
// two groups share a slice of, which a compiler bug could build: an operation
// both reach is held twice, so neither, and one only the longer slice reaches
// is written into a new slice of that one alone.
func TestEditor_EditsAroundATreeSliceTwoGroupsShare(t *testing.T) {
	t.Parallel()
	shared := []ir.Operation{treeOperation("o/both"), treeOperation("o/one")}
	doc := &ir.Document{Services: []ir.Service{
		treeService("service/a", treeGroup("operation-group/long", shared), treeGroup("operation-group/short", shared[:1])),
	}}

	editor := ir.NewEditor(doc)
	editor.UpdateOperation("o/both", func(operation *ir.Operation) { changed(&operation.Docs) })
	editor.UpdateOperation("o/one", func(operation *ir.Operation) { changed(&operation.Docs) })
	out, diags := editor.Finish()

	assert.Equal(t, []string{`internal: edit: update rejected: 2 operations have the ID "o/both", ` +
		`so the edit cannot tell which to update`}, refusals(t, diags))
	assert.Equal(t, "service/a[operation-group/long(o/both o/one=changed) operation-group/short(o/both)]", render(out))
	assert.Equal(t, "o/one", shared[1].Docs.Summary, "the edited document changed")
	assert.Same(t, &shared[0], &out.Services[0].Groups[1].Operations[0], "the shorter slice was built")
}

// TestEditor_ReadsAGroupSliceTwoGroupsShareFromEach pins the edit of a slice of
// groups two groups share, which a compiler bug could build: it is reached from
// each, as ForEachOperation reaches it, so what it holds is held twice and is
// neither read nor written.
func TestEditor_ReadsAGroupSliceTwoGroupsShareFromEach(t *testing.T) {
	t.Parallel()
	shared := []ir.OperationGroup{treeGroup("operation-group/inner", []ir.Operation{treeOperation("o/inner")})}
	doc := &ir.Document{Services: []ir.Service{
		treeService("service/a", treeGroup("operation-group/x", nil, shared...), treeGroup("operation-group/y", nil, shared...)),
	}}
	var walked []ir.OperationID
	ir.ForEachOperation(doc, func(operation ir.Operation) { walked = append(walked, operation.ID) })
	require.Equal(t, []ir.OperationID{"o/inner", "o/inner"}, walked)

	editor := ir.NewEditor(doc)
	_, operationHeld := editor.Operation("o/inner")
	_, groupHeld := editor.Group("operation-group/inner")
	editor.UpdateOperation("o/inner", func(operation *ir.Operation) { changed(&operation.Docs) })
	_, diags := editor.Finish()

	assert.False(t, operationHeld)
	assert.False(t, groupHeld)
	assert.Equal(t, []string{`internal: edit: update rejected: 2 operations have the ID "o/inner", ` +
		`so the edit cannot tell which to update`}, refusals(t, diags))
}

// groupOf returns a document of one service holding groups groups of n
// operations each, and the operations' IDs in order.
func groupOf(groups, n int) (*ir.Document, []ir.OperationID) {
	svc := treeService("service/a")
	ids := make([]ir.OperationID, 0, groups*n)
	for g := range groups {
		operations := make([]ir.Operation, 0, n)
		for i := range n {
			id := ir.OperationID(fmt.Sprint("o/", g, "/", i))
			operations = append(operations, treeOperation(id))
			ids = append(ids, id)
		}
		svc.Groups = append(svc.Groups, treeGroup(ir.GroupID(fmt.Sprint("operation-group/", g)), operations))
	}
	return &ir.Document{Services: []ir.Service{svc}}, ids
}

// TestEditor_TreeWorkIsLinear holds what writes to the tree cost, as nodes
// stepped through and elements built, to growing linearly with how many they
// write: going from 2n operations to 4n adds no more than two and a half times
// what going from n to 2n adds, so the edit's fixed costs cancel. A slice built
// at every write, or a removal shifting what follows it at once, grows
// quadratically. Each scenario's floor, what any edit must build, holds the
// count to counting.
func TestEditor_TreeWorkIsLinear(t *testing.T) {
	t.Parallel()
	removeEach := func(editor *ir.Editor, ids []ir.OperationID) {
		for _, id := range ids {
			editor.RemoveOperation(id)
		}
	}
	tests := []struct {
		name   string
		groups func(n int) (int, int)
		edit   func(editor *ir.Editor, ids []ir.OperationID)
		floor  func(n int) int
	}{
		{"empty one group", func(n int) (int, int) { return 1, n }, removeEach, func(n int) int { return n }},
		{"empty one group from its end", func(n int) (int, int) { return 1, n }, func(editor *ir.Editor, ids []ir.OperationID) {
			for _, id := range slices.Backward(ids) {
				editor.RemoveOperation(id)
			}
		}, func(n int) int { return n }},
		{"remove every other operation", func(n int) (int, int) { return 1, n }, func(editor *ir.Editor, ids []ir.OperationID) {
			for i, id := range ids {
				if i%2 == 0 {
					editor.RemoveOperation(id)
				}
			}
		}, func(n int) int { return n + n/2 }},
		{"update every operation", func(n int) (int, int) { return 1, n }, func(editor *ir.Editor, ids []ir.OperationID) {
			for _, id := range ids {
				editor.UpdateOperation(id, func(operation *ir.Operation) { changed(&operation.Docs) })
			}
		}, func(n int) int { return n }},
		{"empty many groups", func(n int) (int, int) { return n, 2 }, removeEach, func(n int) int { return 2 * n }},
		{"empty many groups, reading each after each removal", func(n int) (int, int) { return n, 2 },
			func(editor *ir.Editor, ids []ir.OperationID) {
				for i, id := range ids {
					editor.RemoveOperation(id)
					_, _ = editor.Group(ir.GroupID(fmt.Sprint("operation-group/", i/2)))
				}
			}, func(n int) int { return 2 * n }},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			work := func(n int) int {
				doc, ids := groupOf(tt.groups(n))
				editor := ir.NewEditor(doc)
				tt.edit(editor, ids)
				_, diags := editor.Finish()
				require.Empty(t, diags)
				w := ir.TreeWork(editor)
				require.GreaterOrEqual(t, w, tt.floor(n), "at %d operations the count misses what any edit builds", n)
				return w
			}
			w1, w2, w4 := work(128), work(256), work(512)
			assert.LessOrEqual(t, 2*(w4-w2), 5*(w2-w1), "work %d, %d, %d grows faster than the operations", w1, w2, w4)
		})
	}
}

// TestEditor_TreeWorkCountsEveryStep holds the count TestEditor_TreeWorkIsLinear
// reads to every step of one removal, so no step's cost goes uncounted.
// Removing o/a1 from treeDoc steps through 3 nodes on the path to it, and
// Finish builds the path's 3 lists: 2 services, s/a's 2 groups and g/a's 3
// operations.
func TestEditor_TreeWorkCountsEveryStep(t *testing.T) {
	t.Parallel()
	editor := ir.NewEditor(treeDoc())
	editor.RemoveOperation("o/a1")
	_, diags := editor.Finish()

	require.Empty(t, diags)
	assert.Equal(t, 3+2+2+3, ir.TreeWork(editor))
}

// TestEditor_ARepeatedReadBuildsNothing holds a read to building only what
// changed since the one before: a second read of a service, or of a group in
// it, costs no work, a write steps through the path to its node and builds
// nothing, and the read after it builds the one list it changed.
func TestEditor_ARepeatedReadBuildsNothing(t *testing.T) {
	t.Parallel()
	editor := ir.NewEditor(treeDoc())
	editor.RemoveOperation("o/a1")
	_, _ = editor.Service("service/a")
	built := ir.TreeWork(editor)
	_, _ = editor.Service("service/a")
	_, _ = editor.Group("operation-group/a")
	assert.Equal(t, built, ir.TreeWork(editor), "a read built what no write had changed")

	editor.UpdateOperation("o/b", func(operation *ir.Operation) { changed(&operation.Docs) })
	stepped := ir.TreeWork(editor)
	assert.Equal(t, built+3, stepped, "a write built a slice, or stepped past the path to its node")
	group, _ := editor.Group("operation-group/b")
	assert.Equal(t, "changed", group.Operations[0].Docs.Summary)
	assert.Equal(t, stepped+1, ir.TreeWork(editor), "a read built more than the list that changed")
}

// TestEditor_AReadOfWhatNoWriteReachedAllocatesNothing pins that a read of a
// group or service no write reached copies nothing. It is not run in parallel:
// AllocsPerRun measures the process.
func TestEditor_AReadOfWhatNoWriteReachedAllocatesNothing(t *testing.T) {
	editor := ir.NewEditor(treeDoc())
	editor.UpdateOperation("o/c", func(operation *ir.Operation) { changed(&operation.Docs) })
	_, _ = editor.Group("operation-group/a")
	assert.Zero(t, testing.AllocsPerRun(10, func() { _, _ = editor.Group("operation-group/a") }))
	assert.Zero(t, testing.AllocsPerRun(10, func() { _, _ = editor.Service("service/a") }))
}

// TestEditor_TreeEditsAgreeWithAPlainRewrite runs random edits over random
// trees and holds every result to the same edits applied, without an edit,
// to a copy of the tree: what each write leaves, what every read returns
// after each batch of edits, each such read staying as it was to the end, and
// the edited document left as it was. A batch leaves lists stale for the
// updates after it, whose changes read a node first, which can build them
// under those updates; and every kind of edit must be made often enough to
// count.
func TestEditor_TreeEditsAgreeWithAPlainRewrite(t *testing.T) {
	t.Parallel()
	made := map[treeEdit]int{}
	for seed := range uint64(300) {
		r := rand.New(rand.NewPCG(seed, 1098))
		doc := randomTree(r)
		before, all := render(doc), treeIDs(doc)
		model := &ir.Document{Services: cloneServices(doc.Services)}
		editor := ir.NewEditor(doc)
		var kept []keptRead
		for step := range 12 {
			var edits []treeEdit
			for batch := range 1 + r.IntN(3) {
				ids := treeIDs(model)
				if len(ids) == 0 {
					break
				}
				edit := treeEdit{pick: ids[r.IntN(len(ids))], remove: r.IntN(3) == 0}
				read, mark := all[r.IntN(len(all))], fmt.Sprint(step, ".", batch)
				applyEdit(editor, edit, mark, func() { readNode(editor, read) })
				applyRewrite(model, edit.pick, edit.remove, mark)
				made[treeEdit{pick: treeID{class: edit.pick.class}, remove: edit.remove}]++
				edits = append(edits, edit)
			}
			requireReadsAgree(t, editor, model, all, fmt.Sprintf("seed %d step %d (%v)", seed, step, edits))
			for _, service := range model.Services {
				if got, ok := editor.Service(service.ID); ok {
					kept = append(kept, keptRead{got, render(&ir.Document{Services: []ir.Service{got}})})
				}
			}
		}
		out, diags := editor.Finish()
		require.Empty(t, diags, "seed %d", seed)
		require.Equal(t, render(model), render(out), "seed %d", seed)
		for _, read := range kept {
			require.Equal(t, read.want, render(&ir.Document{Services: []ir.Service{read.service}}), "seed %d", seed)
		}
		require.Equal(t, before, render(doc), "seed %d: the edited document changed", seed)
	}
	for _, class := range []string{"service", "group", "operation"} {
		for _, remove := range []bool{false, true} {
			assert.GreaterOrEqual(t, made[treeEdit{pick: treeID{class: class}, remove: remove}], 50,
				"too few %s edits removing %v to tell anything", class, remove)
		}
	}
}

// keptRead is a service an edit returned, and how it read then.
type keptRead struct {
	service ir.Service
	want    string
}

// treeEdit is one random edit: the node it picks, and whether it removes the
// node or updates it.
type treeEdit struct {
	pick   treeID
	remove bool
}

// readNode reads the node at id through editor, as a change may.
func readNode(editor *ir.Editor, id treeID) {
	switch id.class {
	case "service":
		_, _ = editor.Service(ir.ServiceID(id.id))
	case "group":
		_, _ = editor.Group(ir.GroupID(id.id))
	default:
		_, _ = editor.Operation(ir.OperationID(id.id))
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
			var operations []ir.Operation
			for range r.IntN(4) {
				operations = append(operations, treeOperation(ir.OperationID(id("o/"))))
			}
			out = append(out, treeGroup(ir.GroupID(id("operation-group/")), operations, groups(depth+1)...))
		}
		return out
	}
	doc := &ir.Document{}
	for range 1 + r.IntN(3) {
		s := id("service/")
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
		for _, operationGroup := range groups {
			ids = append(ids, treeID{"group", string(operationGroup.ID)})
			for _, operation := range operationGroup.Operations {
				ids = append(ids, treeID{"operation", string(operation.ID)})
			}
			walk(operationGroup.Groups)
		}
	}
	for _, service := range doc.Services {
		ids = append(ids, treeID{"service", string(service.ID)})
		walk(service.Groups)
	}
	return ids
}

// applyEdit removes the node edit picks through editor, or appends mark to its
// summary from a change that first calls read.
func applyEdit(editor *ir.Editor, edit treeEdit, mark string, read func()) {
	id := edit.pick.id
	switch {
	case edit.pick.class == "service" && edit.remove:
		editor.RemoveService(ir.ServiceID(id))
	case edit.pick.class == "service":
		editor.UpdateService(ir.ServiceID(id), func(service *ir.Service) { read(); service.Docs.Summary += mark })
	case edit.pick.class == "group" && edit.remove:
		editor.RemoveGroup(ir.GroupID(id))
	case edit.pick.class == "group":
		editor.UpdateGroup(ir.GroupID(id), func(operationGroup *ir.OperationGroup) { read(); operationGroup.Docs.Summary += mark })
	case edit.remove:
		editor.RemoveOperation(ir.OperationID(id))
	default:
		editor.UpdateOperation(ir.OperationID(id), func(operation *ir.Operation) { read(); operation.Docs.Summary += mark })
	}
}

// applyRewrite does to doc, in place, what applyEdit does through an edit.
func applyRewrite(doc *ir.Document, pick treeID, remove bool, mark string) {
	if pick.class == "service" {
		i := slices.IndexFunc(doc.Services, func(service ir.Service) bool { return string(service.ID) == pick.id })
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
		operationGroup := &(*groups)[i]
		if pick.class == "group" && string(operationGroup.ID) == pick.id {
			if remove {
				*groups = slices.Delete(*groups, i, i+1)
				return
			}
			operationGroup.Docs.Summary += mark
			return
		}
		if j := slices.IndexFunc(operationGroup.Operations, func(operation ir.Operation) bool { return string(operation.ID) == pick.id }); j >= 0 &&
			pick.class == "operation" {
			if remove {
				operationGroup.Operations = slices.Delete(operationGroup.Operations, j, j+1)
			} else {
				operationGroup.Operations[j].Docs.Summary += mark
			}
			return
		}
		rewriteGroups(&operationGroup.Groups, pick, remove, mark)
	}
}

// requireReadsAgree holds every read of editor to the node model holds at its
// ID, spelled by render, and every node of all model no longer holds to being
// held by editor no more.
func requireReadsAgree(t *testing.T, editor *ir.Editor, model *ir.Document, all []treeID, at string) {
	t.Helper()
	held := map[treeID]bool{}
	for _, id := range treeIDs(model) {
		held[id] = true
	}
	for _, service := range model.Services {
		got, ok := editor.Service(service.ID)
		require.True(t, ok, "%s: service %s", at, service.ID)
		require.Equal(t, render(&ir.Document{Services: []ir.Service{service}}),
			render(&ir.Document{Services: []ir.Service{got}}), "%s: service %s", at, service.ID)
	}
	var check func([]ir.OperationGroup)
	check = func(groups []ir.OperationGroup) {
		for _, operationGroup := range groups {
			got, ok := editor.Group(operationGroup.ID)
			require.True(t, ok, "%s: group %s", at, operationGroup.ID)
			require.Equal(t, renderGroups([]ir.OperationGroup{operationGroup}, 8), renderGroups([]ir.OperationGroup{got}, 8),
				"%s: group %s", at, operationGroup.ID)
			for _, operation := range operationGroup.Operations {
				got, ok := editor.Operation(operation.ID)
				require.True(t, ok, "%s: operation %s", at, operation.ID)
				require.Equal(t, operation.Docs, got.Docs, "%s: operation %s", at, operation.ID)
			}
			check(operationGroup.Groups)
		}
	}
	for _, service := range model.Services {
		check(service.Groups)
	}
	for _, id := range all {
		if held[id] {
			continue
		}
		var found bool
		switch id.class {
		case "service":
			_, found = editor.Service(ir.ServiceID(id.id))
		case "group":
			_, found = editor.Group(ir.GroupID(id.id))
		default:
			_, found = editor.Operation(ir.OperationID(id.id))
		}
		require.False(t, found, "%s: %s %s is held though the model holds none", at, id.class, id.id)
	}
}
