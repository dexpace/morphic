// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package ir_test

import (
	"fmt"
	"maps"
	"reflect"
	"slices"
	"strconv"
	"strings"
	"testing"

	"github.com/google/go-cmp/cmp"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dexpace/morphic/ir"
	"github.com/dexpace/morphic/ir/irtest"
)

// editedDoc returns a document of three types: a model whose one property is a
// list of the scalar, the list, and the scalar. Each call builds new nodes.
func editedDoc() *ir.Document {
	return &ir.Document{
		IRVersion: ir.IRVersion,
		Name:      "edited",
		Types: ir.TypeRegistry{
			"t/model": &ir.Model{
				ID: "t/model", Docs: ir.Docs{Summary: " A model. "},
				Properties: []ir.Property{{ID: "t/model/p/tags", Type: ir.TypeRef{Target: "t/list"}}},
			},
			"t/list": &ir.List{
				ID: "t/list", Tags: []string{"kept"},
				Elem: ir.TypeRef{Target: "t/scalar"},
			},
			"t/scalar": &ir.Scalar{
				ID:          "t/scalar",
				Constraints: &ir.Constraints{Pattern: "^a$"},
			},
		},
	}
}

// refusals returns the messages of diags, each required to be an EditRefused
// error locating no source.
func refusals(t *testing.T, diags []ir.Diagnostic) []string {
	t.Helper()
	messages := make([]string, 0, len(diags))
	for _, d := range diags {
		require.Equal(t, ir.EditRefused, d.Code, d.Message)
		require.Equal(t, ir.SeverityError, d.Severity, d.Message)
		require.Equal(t, ir.NoSource, d.Provenance.Source, d.Message)
		messages = append(messages, d.Message)
	}
	return messages
}

// TestEditor_WritesTheEditedRegistryAndNotTheDocument pins each write: what the
// edit's document holds after it, and that the edited document still holds
// what it held, node for node and byte for byte.
func TestEditor_WritesTheEditedRegistryAndNotTheDocument(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name  string
		write func(*ir.Editor)
		want  func(t *testing.T, types ir.TypeRegistry)
	}{
		{"remove", func(e *ir.Editor) { e.RemoveType("t/scalar") },
			func(t *testing.T, types ir.TypeRegistry) {
				t.Helper()
				assert.Equal(t, []ir.TypeID{"t/list", "t/model"}, slices.Sorted(maps.Keys(types)))
			}},
		{"replace with another kind", func(e *ir.Editor) {
			e.ReplaceType(&ir.Primitive{ID: "t/scalar", Prim: ir.PrimString})
		}, func(t *testing.T, types ir.TypeRegistry) {
			t.Helper()
			assert.Equal(t, ir.KindPrimitive, types["t/scalar"].Kind())
		}},
		{"update", func(e *ir.Editor) {
			e.UpdateType("t/model", func(td ir.TypeDef) {
				m, _ := td.(*ir.Model)
				m.Docs.Summary = strings.TrimSpace(m.Docs.Summary)
				m.Properties[0].Type.Target = "t/scalar"
			})
		}, func(t *testing.T, types ir.TypeRegistry) {
			t.Helper()
			m, ok := types["t/model"].(*ir.Model)
			require.True(t, ok)
			assert.Equal(t, "A model.", m.Docs.Summary)
			assert.Equal(t, ir.TypeID("t/scalar"), m.Properties[0].Type.Target)
		}},
		{"update twice", func(e *ir.Editor) {
			for _, tag := range []string{"first", "second"} {
				e.UpdateType("t/list", func(td ir.TypeDef) { td.Common().Tags = append(td.Common().Tags, tag) })
			}
		}, func(t *testing.T, types ir.TypeRegistry) {
			t.Helper()
			assert.Equal(t, []string{"kept", "first", "second"}, types["t/list"].Common().Tags)
		}},
		{"update reading the edit", func(e *ir.Editor) {
			e.RemoveType("t/scalar")
			e.UpdateType("t/model", func(td ir.TypeDef) {
				_, held := e.Type("t/scalar")
				td.Common().Docs.Summary = fmt.Sprint("scalar held: ", held)
			})
		}, func(t *testing.T, types ir.TypeRegistry) {
			t.Helper()
			assert.Equal(t, "scalar held: false", types["t/model"].Common().Docs.Summary)
		}},
		{"update a replaced node", func(e *ir.Editor) {
			e.ReplaceType(&ir.List{ID: "t/list", Elem: ir.TypeRef{Target: "t/model"}})
			e.UpdateType("t/list", func(td ir.TypeDef) { td.Common().Access = "internal" })
		}, func(t *testing.T, types ir.TypeRegistry) {
			t.Helper()
			l, ok := types["t/list"].(*ir.List)
			require.True(t, ok)
			assert.Equal(t, ir.TypeID("t/model"), l.Elem.Target)
			assert.Equal(t, "internal", l.Access)
		}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			doc := editedDoc()
			before, nodes := irtest.Encoded(t, doc), maps.Clone(doc.Types)

			e := ir.NewEditor(doc)
			tt.write(e)
			out, diags := e.Finish()

			assert.Empty(t, diags)
			tt.want(t, out.Types)
			assert.Equal(t, before, irtest.Encoded(t, doc), "the edited document changed")
			require.Len(t, doc.Types, len(nodes), "the edited document holds other types")
			for id, td := range nodes {
				assert.Same(t, td, doc.Types[id], "the edited document holds another node at %s", id)
			}
			for id, td := range out.Types {
				assert.Equal(t, id, td.Common().ID, "a node sits at a key its ID does not name")
			}
		})
	}
}

// containers returns the address of each container of doc an edit copies at
// its first write into it, keyed by field.
func containers(doc *ir.Document) map[string]uintptr {
	out := map[string]uintptr{}
	for _, field := range keyedFields {
		out[field] = reflect.ValueOf(doc).Elem().FieldByName(field).Pointer()
	}
	return out
}

// TestEditor_FinishReadsNoTypeUnlessTheEditWroteOne pins that Finish holds the
// types an edit took to their keys only when it took one: an edit of the rest of
// a document pays nothing for how many types it holds. It is not run in
// parallel: AllocsPerRun measures the process.
func TestEditor_FinishReadsNoTypeUnlessTheEditWroteOne(t *testing.T) {
	allocs := func(types int) float64 {
		doc := &ir.Document{Types: ir.TypeRegistry{}}
		for i := range types {
			id := ir.TypeID(fmt.Sprint("t/", i))
			doc.Types[id] = &ir.Any{ID: id}
		}
		return testing.AllocsPerRun(10, func() { _, _ = ir.NewEditor(doc).Finish() })
	}
	assert.Equal(t, allocs(1), allocs(1024))
}

// TestEditor_FinishSharesWhatTheEditLeft pins Finish's document: a new one,
// sharing with the document edited every container the edit wrote nothing
// into, and only those, and every node it did not write.
func TestEditor_FinishSharesWhatTheEditLeft(t *testing.T) {
	t.Parallel()
	doc := filledDoc(t)

	untouched, _ := ir.NewEditor(doc).Finish()
	e := ir.NewEditor(doc)
	e.RemoveType("t/a")
	e.RemoveOperation("o/a1")
	e.UpdateDocument(func(d *ir.Document) { d.Name = "renamed" })
	edited, diags := e.Finish()
	require.Empty(t, diags)

	require.NotSame(t, doc, untouched)
	assert.Equal(t, containers(doc), containers(untouched), "an edit that writes nothing copies nothing")
	written := containers(edited)
	for field, addr := range containers(doc) {
		assert.Equal(t, field != "Types" && field != "Services", addr == written[field], "%s shared", field)
	}
	assert.Same(t, &doc.Services[1].Groups[0], &edited.Services[1].Groups[0], "a node the edit does not write is shared")
	assert.Equal(t, "renamed", edited.Name)
	assert.NotEqual(t, "renamed", doc.Name)
}

// TestEditor_TypeReadsTheEdit pins that Type answers with what the edit holds:
// a written node, no node once removed, and the edited document's otherwise.
func TestEditor_TypeReadsTheEdit(t *testing.T) {
	t.Parallel()
	doc := editedDoc()
	replacement := &ir.Any{ID: "t/list"}

	e := ir.NewEditor(doc)
	e.ReplaceType(replacement)
	e.RemoveType("t/scalar")
	listed, listHeld := e.Type("t/list")
	_, scalarHeld := e.Type("t/scalar")
	model, modelHeld := e.Type("t/model")
	_, absentHeld := e.Type("t/absent")

	assert.True(t, listHeld)
	assert.Same(t, replacement, listed)
	assert.False(t, scalarHeld)
	assert.True(t, modelHeld)
	assert.Same(t, doc.Types["t/model"], model)
	assert.False(t, absentHeld)
}

// TestEditor_RefusesWritesOnlyABugMakes pins each write refused for what it
// asks alone: the one message Finish reports, and that the refused write leaves
// the edit's document as a control edit making only the writes before it
// leaves its own. The tests after it pin the refusals that need more context.
func TestEditor_RefusesWritesOnlyABugMakes(t *testing.T) {
	t.Parallel()
	removeScalar := func(e *ir.Editor, _ *ir.Document) { e.RemoveType("t/scalar") }
	tests := []struct {
		name    string
		before  func(e *ir.Editor, doc *ir.Document)
		refused func(e *ir.Editor, doc *ir.Document)
		want    string
	}{
		{"replace with nil", nil, func(e *ir.Editor, _ *ir.Document) { e.ReplaceType(nil) },
			"replace rejected: nil type definition"},
		{"replace with a typed nil", nil, func(e *ir.Editor, _ *ir.Document) { e.ReplaceType((*ir.Model)(nil)) },
			"replace rejected: nil type definition"},
		{"replace an absent type", nil, func(e *ir.Editor, _ *ir.Document) {
			e.ReplaceType(&ir.Any{ID: "t/new"})
		}, `replace rejected: no type "t/new" to replace; an edit adds no type`},
		{"replace a removed type", removeScalar, func(e *ir.Editor, _ *ir.Document) {
			e.ReplaceType(&ir.Any{ID: "t/scalar"})
		}, `replace rejected: no type "t/scalar" to replace; an edit adds no type`},
		{"replace with the document's own node", nil, func(e *ir.Editor, doc *ir.Document) {
			e.ReplaceType(doc.Types["t/scalar"])
		}, `replace rejected: "t/scalar" is the edited document's own node, which an edit never changes`},
		{"replace with the document's own node after an update", func(e *ir.Editor, _ *ir.Document) {
			e.UpdateType("t/scalar", func(td ir.TypeDef) { td.Common().Access = "internal" })
		}, func(e *ir.Editor, doc *ir.Document) {
			e.ReplaceType(doc.Types["t/scalar"])
		}, `replace rejected: "t/scalar" is the edited document's own node`},
		{"remove an absent type", nil, func(e *ir.Editor, _ *ir.Document) { e.RemoveType("t/new") },
			`remove rejected: no type "t/new" to remove; an edit adds no type`},
		{"remove twice", removeScalar, removeScalar,
			`remove rejected: no type "t/scalar" to remove`},
		{"update an absent type", nil, func(e *ir.Editor, _ *ir.Document) {
			e.UpdateType("t/new", func(ir.TypeDef) {})
		}, `update rejected: no type "t/new" to update; an edit adds no type`},
		{"update a removed type", removeScalar, func(e *ir.Editor, _ *ir.Document) {
			e.UpdateType("t/scalar", func(ir.TypeDef) {})
		}, `update rejected: no type "t/scalar" to update`},
		{"update with no change", nil, func(e *ir.Editor, _ *ir.Document) { e.UpdateType("t/model", nil) },
			`update rejected: no change for "t/model"`},
		{"update moving the type", nil, func(e *ir.Editor, _ *ir.Document) {
			e.UpdateType("t/model", func(td ir.TypeDef) {
				td.Common().ID = "t/moved"
				td.Common().Docs.Summary = "moved"
			})
		}, `update rejected: the change moved "t/model" to "t/moved"; a node keeps its ID`},
		{"update emptying the ID", nil, func(e *ir.Editor, _ *ir.Document) {
			e.UpdateType("t/model", func(td ir.TypeDef) { *td.Common() = ir.TypeCommon{} })
		}, `update rejected: the change moved "t/model" to ""`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			doc, control := editedDoc(), editedDoc()
			e, c := ir.NewEditor(doc), ir.NewEditor(control)
			if tt.before != nil {
				tt.before(e, doc)
				tt.before(c, control)
			}
			tt.refused(e, doc)

			out, diags := e.Finish()
			want, ctrl := c.Finish()

			require.Empty(t, ctrl, "a write before the refused one was refused as well, so the case tests the wrong write")
			got := refusals(t, diags)
			require.Len(t, got, 1, "one refused write, one refusal: %v", got)
			assert.Contains(t, got[0], "internal: edit: "+tt.want)
			assert.Equal(t, irtest.Encoded(t, want), irtest.Encoded(t, out), "the refused write changed the edit")
			assert.Equal(t, irtest.Encoded(t, editedDoc()), irtest.Encoded(t, doc), "the edited document changed")
		})
	}
}

// TestEditor_UpdateRefusesAChangeWritingOrEndingTheEdit pins that such a
// change is refused whole: the call it made, then the update. The edit goes on
// as a control without that update does, writes and updates alike, and a
// refusal made before the change reaches the Finish that ends the edit.
func TestEditor_UpdateRefusesAChangeWritingOrEndingTheEdit(t *testing.T) {
	t.Parallel()
	const inside = "called from inside an UpdateType change"
	tests := []struct {
		name string
		call func(e *ir.Editor)
		want string
	}{
		{"a remove", func(e *ir.Editor) { e.RemoveType("t/scalar") },
			`remove rejected: ` + inside + ` ("t/scalar"); a change alters its node alone`},
		{"a replace", func(e *ir.Editor) { e.ReplaceType(&ir.Any{ID: "t/list"}) },
			`replace rejected: ` + inside + ` ("t/list")`},
		{"a replace refused for itself", func(e *ir.Editor) { e.ReplaceType(nil) },
			`replace rejected: nil type definition`},
		{"an update", func(e *ir.Editor) {
			e.UpdateType("t/list", func(td ir.TypeDef) { td.Common().Access = "internal" })
		}, `update rejected: ` + inside + ` ("t/list")`},
		{"finish", func(e *ir.Editor) { e.Finish() },
			`finish rejected: ` + inside + `; a change alters its node alone`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			doc := editedDoc()
			e, c := ir.NewEditor(doc), ir.NewEditor(editedDoc())
			e.RemoveType("t/absent")
			e.UpdateType("t/model", func(td ir.TypeDef) {
				td.Common().Docs.Summary = "changed"
				tt.call(e)
			})
			for _, ed := range []*ir.Editor{e, c} {
				ed.RemoveType("t/scalar")
				ed.UpdateType("t/list", func(td ir.TypeDef) { td.Common().Tags = append(td.Common().Tags, "after") })
			}

			out, diags := e.Finish()
			want, ctrl := c.Finish()

			require.Empty(t, ctrl)
			got := refusals(t, diags)
			require.Len(t, got, 3, "the refusal before the change, the call and the update: %v", got)
			assert.Contains(t, got[0], `remove rejected: no type "t/absent"`)
			assert.Contains(t, got[1], "internal: edit: "+tt.want)
			assert.Equal(t, `internal: edit: update rejected: the change of "t/model" wrote to the edit `+
				`or ended it; a change alters its node alone`, got[2])
			assert.Equal(t, irtest.Encoded(t, want), irtest.Encoded(t, out), "the change or a call it made changed the edit")
			assert.Equal(t, irtest.Encoded(t, editedDoc()), irtest.Encoded(t, doc), "the edited document changed")
		})
	}
}

// editCall is one call an edit takes, with the node it names spelled as its
// refusals spell it.
type editCall struct {
	verb, at string
	call     func(*ir.Editor)
}

// writes returns a call of every write an edit takes, each at a node
// filledDoc holds and updates do not touch, and Finish.
func writes() []editCall {
	return []editCall{
		{"remove", `"t/a"`, func(e *ir.Editor) { e.RemoveType("t/a") }},
		{"update", `"t/a"`, func(e *ir.Editor) { e.UpdateType("t/a", func(ir.TypeDef) {}) }},
		{"replace", `"t/a"`, func(e *ir.Editor) { e.ReplaceType(&ir.Any{ID: "t/a"}) }},
		{"remove", `"s/b"`, func(e *ir.Editor) { e.RemoveService("s/b") }},
		{"update", `"s/b"`, func(e *ir.Editor) { e.UpdateService("s/b", func(*ir.Service) {}) }},
		{"remove", `"g/b"`, func(e *ir.Editor) { e.RemoveGroup("g/b") }},
		{"update", `"g/b"`, func(e *ir.Editor) { e.UpdateGroup("g/b", func(*ir.OperationGroup) {}) }},
		{"remove", `"o/b"`, func(e *ir.Editor) { e.RemoveOperation("o/b") }},
		{"update", `"o/b"`, func(e *ir.Editor) { e.UpdateOperation("o/b", func(*ir.Operation) {}) }},
		{"remove", `"v/b"`, func(e *ir.Editor) { e.RemoveServer("v/b") }},
		{"update", `"v/b"`, func(e *ir.Editor) { e.UpdateServer("v/b", func(*ir.Server) {}) }},
		{"remove", `"c/b"`, func(e *ir.Editor) { e.RemoveChannel("c/b") }},
		{"update", `"c/b"`, func(e *ir.Editor) { e.UpdateChannel("c/b", func(*ir.Channel) {}) }},
		{"remove", `"m/b"`, func(e *ir.Editor) { e.RemoveMessage("m/b") }},
		{"update", `"m/b"`, func(e *ir.Editor) { e.UpdateMessage("m/b", func(*ir.Message) {}) }},
		{"remove", `"a/b"`, func(e *ir.Editor) { e.RemoveAuthScheme("a/b") }},
		{"update", `"a/b"`, func(e *ir.Editor) { e.UpdateAuthScheme("a/b", func(*ir.AuthScheme) {}) }},
		{"update", "the document", func(e *ir.Editor) { e.UpdateDocument(func(*ir.Document) {}) }},
		{"finish", "", func(e *ir.Editor) { e.Finish() }},
	}
}

// updates returns an update of each kind of node, at a node filledDoc holds
// that the calls of writes do not name, running inner from its change.
func updates(inner func()) []editCall {
	return []editCall{
		{"UpdateType", `"t/a"`, func(e *ir.Editor) { e.UpdateType("t/a", func(ir.TypeDef) { inner() }) }},
		{"UpdateService", `"s/a"`, func(e *ir.Editor) { e.UpdateService("s/a", func(*ir.Service) { inner() }) }},
		{"UpdateGroup", `"g/a"`, func(e *ir.Editor) { e.UpdateGroup("g/a", func(*ir.OperationGroup) { inner() }) }},
		{"UpdateOperation", `"o/a1"`, func(e *ir.Editor) { e.UpdateOperation("o/a1", func(*ir.Operation) { inner() }) }},
		{"UpdateServer", `"v/a"`, func(e *ir.Editor) { e.UpdateServer("v/a", func(*ir.Server) { inner() }) }},
		{"UpdateChannel", `"c/a"`, func(e *ir.Editor) { e.UpdateChannel("c/a", func(*ir.Channel) { inner() }) }},
		{"UpdateMessage", `"m/a"`, func(e *ir.Editor) { e.UpdateMessage("m/a", func(*ir.Message) { inner() }) }},
		{"UpdateAuthScheme", `"a/a"`, func(e *ir.Editor) { e.UpdateAuthScheme("a/a", func(*ir.AuthScheme) { inner() }) }},
		{"UpdateDocument", "the document", func(e *ir.Editor) { e.UpdateDocument(func(*ir.Document) { inner() }) }},
	}
}

// TestEditor_EveryUpdateRefusesAChangeWritingOrEndingTheEdit pins, for an
// update of every kind of node and a call of every write and of Finish from
// its change, that the call is refused naming the update, the update is
// refused with it, and the edit ends as one making neither does.
func TestEditor_EveryUpdateRefusesAChangeWritingOrEndingTheEdit(t *testing.T) {
	t.Parallel()
	for i := range updates(nil) {
		for j, inner := range writes() {
			var e *ir.Editor
			outer := updates(func() { inner.call(e) })[i]
			t.Run(outer.verb+"/"+inner.verb+" "+inner.at, func(t *testing.T) {
				t.Parallel()
				doc := filledDoc(t)
				e = ir.NewEditor(doc)
				outer.call(e)
				out, diags := e.Finish()

				want := inner.verb + " rejected: called from inside an " + outer.verb + " change (" + inner.at + ")"
				if j == len(writes())-1 {
					want = "finish rejected: called from inside an " + outer.verb + " change"
				}
				assert.Equal(t, []string{
					"internal: edit: " + want + "; a change alters its node alone",
					"internal: edit: update rejected: the change of " + outer.at + " wrote to the edit or ended it; " +
						"a change alters its node alone",
				}, refusals(t, diags))
				assert.Equal(t, irtest.Encoded(t, doc), irtest.Encoded(t, out), "the change or a call it made changed the edit")
			})
		}
	}
}

// TestEditor_RefusesEveryWriteAfterFinish pins that Finish ends the edit for
// every write: each is refused, and the document Finish returned is unchanged.
func TestEditor_RefusesEveryWriteAfterFinish(t *testing.T) {
	t.Parallel()
	e := ir.NewEditor(filledDoc(t))
	out, diags := e.Finish()
	require.Empty(t, diags)
	before := irtest.Encoded(t, out)
	calls := writes()
	calls = calls[:len(calls)-1]
	want := make([]string, 0, len(calls))
	for _, c := range calls {
		c.call(e)
		want = append(want, "internal: edit: "+c.verb+" rejected: the edit is finished ("+c.at+")")
	}

	again, diags := e.Finish()

	assert.Equal(t, want, refusals(t, diags))
	assert.Same(t, out, again)
	assert.Equal(t, before, irtest.Encoded(t, again), "a write after Finish changed its document")
}

// TestEditor_FinishInsideAChangeEndsNothing pins what Finish returns from
// inside an UpdateType change, which a pass could return as its own result:
// every refusal not yet reported, its own among them, beside the edit's
// document, with the edit still open. The Finish that ends the edit reports
// them again.
func TestEditor_FinishInsideAChangeEndsNothing(t *testing.T) {
	t.Parallel()
	e := ir.NewEditor(editedDoc())
	e.RemoveType("t/absent")
	var (
		inner      *ir.Document
		innerDiags []ir.Diagnostic
	)
	e.UpdateType("t/model", func(ir.TypeDef) { inner, innerDiags = e.Finish() })
	e.RemoveType("t/scalar")

	out, diags := e.Finish()

	const (
		absent = `internal: edit: remove rejected: no type "t/absent" to remove; an edit adds no type`
		finish = `internal: edit: finish rejected: called from inside an UpdateType change; ` +
			`a change alters its node alone`
		update = `internal: edit: update rejected: the change of "t/model" wrote to the edit or ended it; ` +
			`a change alters its node alone`
	)
	assert.Equal(t, []string{absent, finish}, refusals(t, innerDiags))
	assert.Same(t, out, inner)
	assert.Equal(t, []string{absent, finish, update}, refusals(t, diags))
	assert.NotContains(t, out.Types, ir.TypeID("t/scalar"), "the edit refused a write after the change")
}

// TestEditor_UpdateLeavesTheEditUsableAfterAPanickingChange pins that a
// change panicking out of UpdateType applies nothing, and leaves an edit that,
// for a caller recovering the panic, takes writes and ends with no refusal.
func TestEditor_UpdateLeavesTheEditUsableAfterAPanickingChange(t *testing.T) {
	t.Parallel()
	doc := editedDoc()
	e := ir.NewEditor(doc)
	assert.PanicsWithValue(t, "change failed", func() {
		e.UpdateType("t/model", func(td ir.TypeDef) {
			td.Common().Docs.Summary = "changed"
			panic("change failed")
		})
	})
	e.RemoveType("t/scalar")

	out, diags := e.Finish()

	assert.Empty(t, diags)
	assert.NotContains(t, out.Types, ir.TypeID("t/scalar"), "a write after the panic was refused")
	assert.Same(t, doc.Types["t/model"], out.Types["t/model"], "the panicking change was applied")
}

// TestEditor_NilEntryIsReplacedNotUpdated pins the edit of a registry entry
// holding nil, untyped or typed, which irverify reports: an update has nothing
// to copy and is refused, while a replacement or a removal takes the entry as
// any other.
func TestEditor_NilEntryIsReplacedNotUpdated(t *testing.T) {
	t.Parallel()
	doc := &ir.Document{Types: ir.TypeRegistry{"t/a": nil, "t/b": (*ir.Model)(nil)}}
	replacement := &ir.Any{ID: "t/a"}

	e := ir.NewEditor(doc)
	e.UpdateType("t/a", func(ir.TypeDef) {})
	e.UpdateType("t/b", func(ir.TypeDef) {})
	e.ReplaceType(replacement)
	e.RemoveType("t/b")
	out, diags := e.Finish()

	assert.Equal(t, []string{
		`internal: edit: update rejected: "t/a" holds a nil type definition, which has nothing to change`,
		`internal: edit: update rejected: "t/b" holds a nil type definition, which has nothing to change`,
	}, refusals(t, diags))
	assert.Equal(t, ir.TypeRegistry{"t/a": replacement}, out.Types)
	assert.Len(t, doc.Types, 2)
}

// TestEditor_UpdateRefusesANodeAtAnotherIDsKey pins the update of an entry
// whose node names another ID, which irverify reports: it is refused as what it
// is, not as a change that moved the node, and Finish reports nothing for the
// entry the edit never took.
func TestEditor_UpdateRefusesANodeAtAnotherIDsKey(t *testing.T) {
	t.Parallel()
	doc := &ir.Document{Types: ir.TypeRegistry{"t/a": &ir.Any{ID: "t/b"}, "t/c": &ir.Any{ID: "t/c"}}}

	e := ir.NewEditor(doc)
	e.UpdateType("t/a", func(ir.TypeDef) {})
	e.RemoveType("t/c")
	_, diags := e.Finish()

	assert.Equal(t, []string{`internal: edit: update rejected: ` +
		`"t/a" holds the node of "t/b", which belongs at its own ID`}, refusals(t, diags))
}

// TestEditor_FinishRefusesANodeMovedAfterTheEditTookIt pins that a node the
// edit took and its caller changed afterwards, off the key its ID named, is
// reported when the edit ends, and the edited document's node put back at that
// key: one value replaced at two IDs, and a node an update's change kept.
func TestEditor_FinishRefusesANodeMovedAfterTheEditTookIt(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name  string
		write func(e *ir.Editor)
		moved ir.TypeID
		want  string
	}{
		{"one value replaced at two IDs", func(e *ir.Editor) {
			n := &ir.Any{ID: "t/list"}
			e.ReplaceType(n)
			n.ID = "t/scalar"
			e.ReplaceType(n)
		}, "t/list", `finish: "t/list" holds a node moved to "t/scalar" after the edit took it, ` +
			`so the edited document's node stays; a type keeps its ID`},
		{"an updated node kept", func(e *ir.Editor) {
			var kept ir.TypeDef
			e.UpdateType("t/model", func(td ir.TypeDef) { kept = td })
			kept.Common().ID = "t/moved"
		}, "t/model", `finish: "t/model" holds a node moved to "t/moved" after the edit took it, ` +
			`so the edited document's node stays`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			doc := editedDoc()
			e := ir.NewEditor(doc)
			tt.write(e)

			out, diags := e.Finish()
			_, again := e.Finish()

			got := refusals(t, diags)
			require.Len(t, got, 1, "one moved node, one refusal: %v", got)
			assert.Contains(t, got[0], "internal: edit: "+tt.want)
			assert.Empty(t, again, "a moved node is reported once")
			assert.Same(t, doc.Types[tt.moved], out.Types[tt.moved], "the moved node stayed in the edit's document")
			for id, td := range out.Types {
				assert.Equal(t, id, td.Common().ID, "a node sits at a key its ID does not name")
			}
			assert.Equal(t, irtest.Encoded(t, editedDoc()), irtest.Encoded(t, doc), "the edited document changed")
		})
	}
}

// TestEditor_UpdateKeepsEmptyApartFromAbsent pins that the copy keeps an empty
// slice or map empty and an absent one absent, which an omitzero field tells
// apart, and shares neither empty one with the node, so appending to the
// copy's slice or setting a key in its map writes nothing the node can see.
func TestEditor_UpdateKeepsEmptyApartFromAbsent(t *testing.T) {
	t.Parallel()
	node := &ir.Model{ID: "t/m", Tags: make([]string, 0, 4), WireNameByFormat: map[string]string{}}

	cp, ok := updatedCopy(t, node).(*ir.Model)

	require.True(t, ok)
	assert.NotNil(t, cp.Tags)
	assert.Empty(t, cp.Tags)
	assert.NotNil(t, cp.WireNameByFormat)
	assert.Empty(t, cp.WireNameByFormat)
	assert.Nil(t, cp.Namespace)
	assert.Nil(t, cp.Properties)
	cp.Tags = append(cp.Tags, "appended")
	cp.WireNameByFormat["json"] = "written"
	require.Equal(t, []string{"appended"}, cp.Tags)
	assert.Empty(t, node.Tags[:1][0], "the copy appended into the node's array")
	assert.Empty(t, node.WireNameByFormat, "the copy wrote into the node's map")
}

// TestEditor_FinishReportsEachRefusalOnce pins that Finish ends the edit: a
// later call returns the same document, with only the refusals made since, and
// every write after the first call is refused, the document unchanged.
func TestEditor_FinishReportsEachRefusalOnce(t *testing.T) {
	t.Parallel()
	e := ir.NewEditor(editedDoc())
	e.RemoveType("t/absent")
	first, firstDiags := e.Finish()
	before := irtest.Encoded(t, first)

	e.RemoveType("t/scalar")
	e.ReplaceType(&ir.Any{ID: "t/list"})
	e.UpdateType("t/model", func(td ir.TypeDef) { td.Common().Access = "internal" })
	second, secondDiags := e.Finish()
	third, thirdDiags := e.Finish()

	assert.Equal(t, []string{`internal: edit: remove rejected: ` +
		`no type "t/absent" to remove; an edit adds no type`}, refusals(t, firstDiags))
	assert.Equal(t, []string{
		`internal: edit: remove rejected: the edit is finished ("t/scalar")`,
		`internal: edit: replace rejected: the edit is finished ("t/list")`,
		`internal: edit: update rejected: the edit is finished ("t/model")`,
	}, refusals(t, secondDiags))
	assert.Empty(t, thirdDiags)
	assert.Same(t, first, second)
	assert.Same(t, first, third)
	assert.Equal(t, before, irtest.Encoded(t, first), "a write after Finish changed its document")
}

// TestNewEditor_NilDocumentIsRefused pins that an edit of no document still
// ends in one, an empty one, beside the refusal saying why, and holds no node
// of any kind.
func TestNewEditor_NilDocumentIsRefused(t *testing.T) {
	t.Parallel()
	e := ir.NewEditor(nil)
	e.RemoveType("t/x")
	e.RemoveOperation("o/x")
	e.UpdateChannel("c/x", func(*ir.Channel) {})
	_, typeHeld := e.Type("t/x")
	_, svcHeld := e.Service("s/x")
	_, groupHeld := e.Group("g/x")
	_, opHeld := e.Operation("o/x")
	_, serverHeld := e.Server("v/x")
	_, chHeld := e.Channel("c/x")
	_, msgHeld := e.Message("m/x")
	_, authHeld := e.AuthScheme("a/x")

	out, diags := e.Finish()

	require.NotNil(t, out)
	assert.Empty(t, cmp.Diff(&ir.Document{}, out))
	assert.Equal(t, []string{
		"internal: edit: edit rejected: no document to edit",
		`internal: edit: remove rejected: no type "t/x" to remove; an edit adds no type`,
		`internal: edit: remove rejected: no operation "o/x" to remove; an edit adds no operation`,
		`internal: edit: update rejected: no channel "c/x" to update; an edit adds no channel`,
	}, refusals(t, diags))
	assert.False(t, typeHeld || svcHeld || groupHeld || opHeld || serverHeld || chHeld || msgHeld || authHeld)
}

// references returns the address of every pointer, non-empty slice and map v
// reaches, each with the path it was first reached at.
func references(v any) map[uintptr]string {
	refs := map[uintptr]string{}
	ir.WalkValues(v, "node", ir.VisitFunc(func(rv reflect.Value, at ir.WalkPath) bool {
		path := at.String()
		switch rv.Kind() {
		case reflect.Pointer, reflect.Map:
			if !rv.IsNil() {
				refs[rv.Pointer()] = path
			}
		case reflect.Slice:
			if rv.Len() > 0 {
				refs[rv.Pointer()] = path
			}
		default:
		}
		return true
	}))
	return refs
}

// updatedCopy returns the node an edit holds at td's own ID after an update
// that changes nothing, which is the copy UpdateType made of td.
func updatedCopy(t *testing.T, td ir.TypeDef) ir.TypeDef {
	t.Helper()
	id := td.Common().ID
	e := ir.NewEditor(&ir.Document{Types: ir.TypeRegistry{id: td}})
	e.UpdateType(id, func(ir.TypeDef) {})
	out, diags := e.Finish()
	require.Empty(t, diags)
	return out.Types[id]
}

// TestEditor_UpdateCopiesEveryKindWhole holds UpdateType's copy, for a node of
// every kind with every field filled and byte payloads set, to equalling the
// node and sharing no reference with it, so no change to it reaches the node.
func TestEditor_UpdateCopiesEveryKindWhole(t *testing.T) {
	t.Parallel()
	for _, k := range allKinds {
		t.Run(string(k), func(t *testing.T) {
			t.Parallel()
			td := newFilled(t, k)
			c := td.Common()
			c.Examples = append(c.Examples, ir.Example{Value: &ir.Value{Kind: ir.ValueBytes, Bytes: []byte("raw")}})
			c.Unmodeled = ir.Unmodeled{"x-raw": {Reason: ir.ReasonVendorExtension, Value: ir.RawValue(`{"a":1}`)}}

			cp := updatedCopy(t, td)

			assert.Empty(t, cmp.Diff(td, cp), "the copy differs from the node")
			original := references(td)
			require.NotEmpty(t, original)
			for addr, path := range references(cp) {
				if at, shared := original[addr]; shared {
					t.Errorf("the copy's %s is the node's %s", path, at)
				}
			}
		})
	}
}

// TestEditor_CopiesEveryIRTypeWhole holds every type a node an update copies
// reaches, a TypeDef of any kind or a Document's own fields, to what the copy
// can copy: no kind it would share (an interface, an array, a channel, a
// function), and no unexported field, which reflection cannot set and the copy
// would leave shared. A Document's Types are left out of its copy, holding the
// one interface the IR declares, which the edit copies node by node.
func TestEditor_CopiesEveryIRTypeWhole(t *testing.T) {
	t.Parallel()
	queue := make([]reflect.Type, 0, len(allKinds)+1)
	for _, k := range allKinds {
		td, ok := ir.NewTypeDef(k)
		require.True(t, ok)
		queue = append(queue, reflect.TypeOf(td))
	}
	queue = append(queue, reflect.TypeFor[ir.Document]())
	seen := map[reflect.Type]bool{}
	for len(queue) > 0 {
		typ := queue[len(queue)-1]
		queue = queue[:len(queue)-1]
		if seen[typ] {
			continue
		}
		seen[typ] = true
		switch typ.Kind() {
		case reflect.Pointer, reflect.Slice:
			queue = append(queue, typ.Elem())
		case reflect.Map:
			queue = append(queue, typ.Key(), typ.Elem())
			assert.Contains(t, []reflect.Kind{reflect.String, reflect.Int}, typ.Key().Kind(),
				"%s: the copy keeps a map key as it is", typ)
		case reflect.Struct:
			for f := range typ.Fields() {
				assert.True(t, f.IsExported(), "%s.%s is unexported", typ, f.Name)
				if typ != reflect.TypeFor[ir.Document]() || f.Name != "Types" {
					queue = append(queue, f.Type)
				}
			}
		case reflect.Interface, reflect.Array, reflect.Chan, reflect.Func, reflect.UnsafePointer:
			t.Errorf("%s is a %s, which the copy would share", typ, typ.Kind())
		default: // a scalar is its own copy
		}
	}
	for _, node := range []reflect.Type{reflect.TypeFor[ir.Service](), reflect.TypeFor[ir.OperationGroup](),
		reflect.TypeFor[ir.Operation](), reflect.TypeFor[ir.Server](), reflect.TypeFor[ir.Channel](),
		reflect.TypeFor[ir.Message](), reflect.TypeFor[ir.AuthScheme]()} {
		assert.True(t, seen[node], "the walk must reach %s", node)
	}
	assert.Greater(t, len(seen), len(allKinds), "the walk must reach past the kinds themselves")
}

// TestEditor_UpdateCopiesEveryNodeWhole holds the copy an update of every kind
// of node but a type hands its change, every field filled, to equalling the
// node with the fields the edit keys apart left out, and sharing no reference
// with it.
func TestEditor_UpdateCopiesEveryNodeWhole(t *testing.T) {
	t.Parallel()
	raw := ir.Unmodeled{"x-raw": {Reason: ir.ReasonVendorExtension, Value: ir.RawValue(`{"a":1}`)}}
	tests := []struct {
		name   string
		update func(e *ir.Editor, handed func(node any))
		node   func(doc *ir.Document) any
	}{
		{"service", func(e *ir.Editor, h func(any)) { e.UpdateService("s/a", func(s *ir.Service) { h(*s) }) },
			func(doc *ir.Document) any { s := doc.Services[0]; s.Groups = nil; return s }},
		{"group", func(e *ir.Editor, h func(any)) {
			e.UpdateGroup("g/a", func(g *ir.OperationGroup) { h(*g) })
		}, func(doc *ir.Document) any {
			g := doc.Services[0].Groups[0]
			g.Groups, g.Operations = nil, nil
			return g
		}},
		{"operation", func(e *ir.Editor, h func(any)) {
			e.UpdateOperation("o/a", func(op *ir.Operation) { h(*op) })
		}, func(doc *ir.Document) any { return doc.Services[0].Groups[0].Operations[0] }},
		{"server", func(e *ir.Editor, h func(any)) { e.UpdateServer("v/a", func(s *ir.Server) { h(*s) }) },
			func(doc *ir.Document) any { return doc.Servers["v/a"] }},
		{"channel", func(e *ir.Editor, h func(any)) { e.UpdateChannel("c/a", func(c *ir.Channel) { h(*c) }) },
			func(doc *ir.Document) any { return doc.Channels["c/a"] }},
		{"message", func(e *ir.Editor, h func(any)) { e.UpdateMessage("m/a", func(m *ir.Message) { h(*m) }) },
			func(doc *ir.Document) any { return doc.Messages["m/a"] }},
		{"auth scheme", func(e *ir.Editor, h func(any)) {
			e.UpdateAuthScheme("a/a", func(a *ir.AuthScheme) { h(*a) })
		}, func(doc *ir.Document) any { return doc.Auth["a/a"] }},
		{"document", func(e *ir.Editor, h func(any)) { e.UpdateDocument(func(d *ir.Document) { h(*d) }) },
			func(doc *ir.Document) any {
				own := *doc
				own.Services, own.Types, own.Servers, own.Channels, own.Messages, own.Auth = nil, nil, nil, nil, nil, nil
				return own
			}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			doc := wholeDoc(t, raw)
			var (
				copied   any
				original = references(tt.node(doc))
			)
			e := ir.NewEditor(doc)
			tt.update(e, func(node any) {
				copied = node
				require.NotEmpty(t, original)
				for addr, path := range references(node) {
					if at, shared := original[addr]; shared {
						t.Errorf("the copy's %s is the node's %s", path, at)
					}
				}
			})
			_, diags := e.Finish()

			require.Empty(t, diags)
			assert.Empty(t, cmp.Diff(tt.node(doc), copied), "the copy differs from the node")
		})
	}
}

// wholeDoc returns a document with every field of each node filled, the nodes
// at the IDs TestEditor_UpdateCopiesEveryNodeWhole updates, each node holding
// raw as what it leaves unmodeled.
func wholeDoc(t *testing.T, raw ir.Unmodeled) *ir.Document {
	t.Helper()
	doc := &ir.Document{}
	irtest.Fill(t, doc)
	doc.Unmodeled = raw
	svc := &doc.Services[0]
	svc.ID, svc.Unmodeled = "s/a", raw
	group := &svc.Groups[0]
	group.ID, group.Unmodeled, group.Groups = "g/a", raw, nil
	op := &group.Operations[0]
	op.ID, op.Unmodeled = "o/a", raw
	op.Examples = append(op.Examples, ir.Example{Value: &ir.Value{Kind: ir.ValueBytes, Bytes: []byte("raw")}})
	for k, s := range doc.Servers {
		delete(doc.Servers, k)
		s.ID, s.Unmodeled = "v/a", raw
		doc.Servers["v/a"] = s
	}
	for k, c := range doc.Channels {
		delete(doc.Channels, k)
		c.ID, c.Unmodeled = "c/a", raw
		doc.Channels["c/a"] = c
	}
	for k, m := range doc.Messages {
		delete(doc.Messages, k)
		m.ID, m.Unmodeled = "m/a", raw
		doc.Messages["m/a"] = m
	}
	for k, a := range doc.Auth {
		delete(doc.Auth, k)
		a.ID, a.Unmodeled = "a/a", raw
		doc.Auth["a/a"] = a
	}
	return doc
}

// TestEditor_UpdateKeepsAliasingAndEndsACycle pins that the copy shares one
// copy wherever the node shares one pointer, map or slice, keeps slices of two
// lengths over one array apart, and copies a value containing itself, as a
// compiler bug could build, into the same cycle rather than recursing until the
// depth cap refuses it.
func TestEditor_UpdateKeepsAliasingAndEndsACycle(t *testing.T) {
	t.Parallel()
	shared := &ir.Value{Kind: ir.ValueString, Str: "shared"}
	list := []ir.Value{{Kind: ir.ValueString, Str: "listed"}}
	extensions := ir.Unmodeled{"x-a": {Reason: ir.ReasonVendorExtension, Value: ir.RawValue(`1`)}}
	ctor := &ir.CtorValue{Scalar: "t/s", Name: "now"}
	ctor.Args = []ir.Value{{Kind: ir.ValueCtor, Ctor: ctor}}
	base := []string{"a", "b", "c"}
	node := &ir.Scalar{ID: "t/s", Tags: base[:2], Namespace: base[:3], Examples: []ir.Example{
		{Name: "a", Value: shared, Input: &ir.Value{Kind: ir.ValueList, List: list}, Unmodeled: extensions},
		{Name: "b", Value: shared, Input: &ir.Value{Kind: ir.ValueList, List: list}, Unmodeled: extensions},
		{Name: "c", Value: &ir.Value{Kind: ir.ValueCtor, Ctor: ctor}},
	}}

	cp, ok := updatedCopy(t, node).(*ir.Scalar)

	require.True(t, ok)
	examples := cp.Examples
	assert.Same(t, examples[0].Value, examples[1].Value, "one shared pointer, one shared copy")
	assert.NotSame(t, shared, examples[0].Value)
	assert.Same(t, &examples[0].Input.List[0], &examples[1].Input.List[0], "one shared slice, one shared copy")
	assert.NotSame(t, &list[0], &examples[0].Input.List[0])
	assert.Equal(t, reflect.ValueOf(examples[0].Unmodeled).Pointer(), reflect.ValueOf(examples[1].Unmodeled).Pointer(),
		"one shared map, one shared copy")
	assert.NotEqual(t, reflect.ValueOf(extensions).Pointer(), reflect.ValueOf(examples[0].Unmodeled).Pointer())
	assert.Equal(t, []string{"a", "b"}, cp.Tags, "two lengths over one array are two slices")
	assert.Equal(t, []string{"a", "b", "c"}, cp.Namespace)
	assert.NotSame(t, &base[0], &cp.Tags[0])
	copied := examples[2].Value.Ctor
	assert.NotSame(t, ctor, copied)
	assert.Same(t, copied, copied.Args[0].Ctor, "the cycle comes out as the same cycle")
}

// nestedValue returns a list value nested depth lists deep.
func nestedValue(depth int) *ir.Value {
	v := &ir.Value{Kind: ir.ValueString, Str: "leaf"}
	for range depth {
		v = &ir.Value{Kind: ir.ValueList, List: []ir.Value{*v}}
	}
	return v
}

// TestEditor_UpdateRefusesANodeTooDeepToCopy pins the copy's bound: a node
// with a step past ir.MaxWalkDepth is refused and left as it was, and one whose
// deepest step lands on the cap is copied.
func TestEditor_UpdateRefusesANodeTooDeepToCopy(t *testing.T) {
	t.Parallel()
	// A property's default sits 5 steps below the node, plus 2 a level (a List
	// and its element), and its fields a step further: at onCap levels the
	// deepest step is the cap itself. The walk, stepping into the same fields,
	// is cut at the same depth.
	onCap := (ir.MaxWalkDepth - 6) / 2
	for _, tc := range []struct {
		depth   int
		refused bool
	}{{onCap, false}, {onCap + 1, true}} {
		t.Run(fmt.Sprint(tc.depth), func(t *testing.T) {
			t.Parallel()
			node := &ir.Model{ID: "t/deep",
				Properties: []ir.Property{{ID: "t/deep/p", Default: nestedValue(tc.depth)}}}
			doc := &ir.Document{Types: ir.TypeRegistry{"t/deep": node}}

			e := ir.NewEditor(doc)
			e.UpdateType("t/deep", func(td ir.TypeDef) { td.Common().Access = "internal" })
			out, diags := e.Finish()

			walkCut := ir.WalkValues(node, "node", ir.VisitFunc(func(reflect.Value, ir.WalkPath) bool { return true }))
			require.Equal(t, tc.refused, walkCut, "the fixture is not on the cap the walk keeps")
			if !tc.refused {
				assert.Empty(t, diags)
				assert.Equal(t, "internal", out.Types["t/deep"].Common().Access)
				return
			}
			assert.Equal(t, []string{fmt.Sprintf(`internal: edit: update rejected: `+
				`"t/deep" nests deeper than %d levels, past what an edit copies`, ir.MaxWalkDepth)},
				refusals(t, diags))
			assert.Same(t, node, out.Types["t/deep"])
		})
	}
}

// ExampleNewEditor removes the types nothing references, as a filter pass
// would: the document it edits keeps them, and the one Finish returns does not.
func ExampleNewEditor() {
	doc := &ir.Document{Types: ir.TypeRegistry{
		"t/kept": &ir.List{ID: "t/kept", Elem: ir.TypeRef{Target: "t/elem"}},
		"t/elem": &ir.Primitive{ID: "t/elem", Prim: ir.PrimString},
		"t/lost": &ir.Any{ID: "t/lost"},
	}}
	referenced := map[ir.TypeID]bool{"t/kept": true} // say an operation names it
	for _, td := range doc.Types {
		for id := range ir.TypeEdges(td) {
			referenced[id] = true
		}
	}

	e := ir.NewEditor(doc)
	for _, id := range slices.Sorted(maps.Keys(doc.Types)) {
		if !referenced[id] {
			e.RemoveType(id)
		}
	}
	out, diags := e.Finish()

	fmt.Println(slices.Sorted(maps.Keys(out.Types)), len(diags))
	fmt.Println(slices.Sorted(maps.Keys(doc.Types)))
	// Output:
	// [t/elem t/kept] 0
	// [t/elem t/kept t/lost]
}

// wireNames returns n wire-name overrides, a map whose entries the copy keeps
// nothing of its own for.
func wireNames(n int) map[string]string {
	names := map[string]string{}
	for i := range n {
		names["format"+strconv.Itoa(i)] = "name"
	}
	return names
}

// editedOnce returns a document holding a model for each entry of ids, with as
// many wire-name overrides as the entry gives, and an edit of it that has
// updated each model once.
func editedOnce(t *testing.T, ids map[ir.TypeID]int) (*ir.Document, *ir.Editor) {
	t.Helper()
	doc := &ir.Document{Types: ir.TypeRegistry{}}
	for id, n := range ids {
		model := &ir.Model{ID: id}
		if n > 0 {
			model.WireNameByFormat = wireNames(n)
		}
		doc.Types[id] = model
	}
	e := ir.NewEditor(doc)
	for id := range ids {
		e.UpdateType(id, func(ir.TypeDef) {})
	}
	return doc, e
}

// updating returns a call updating id in e, changing nothing.
func updating(e *ir.Editor, id ir.TypeID) func() {
	return func() { e.UpdateType(id, func(ir.TypeDef) {}) }
}

// TestEditor_CopiesMapEntriesWithoutAllocatingEach holds a copy's cost per map
// entry at nothing beyond what the copy keeps, here nothing: an edit reads
// entries into storage it keeps, where reflect.MapIter.Key and Value copy each
// into an allocation of its own. Both maps fit one map group, so six more
// entries must cost no more. It is not run in parallel: AllocsPerRun measures
// the process.
func TestEditor_CopiesMapEntriesWithoutAllocatingEach(t *testing.T) {
	_, e := editedOnce(t, map[ir.TypeID]int{"t/few": 2, "t/many": 8})
	assert.Equal(t, testing.AllocsPerRun(10, updating(e, "t/few")), testing.AllocsPerRun(10, updating(e, "t/many")))
	_, diags := e.Finish()
	assert.Empty(t, diags)
}

// TestEditor_KeepsEntryStorageFromCopyToCopy holds the storage an edit reads
// map entries into to being set up once for the edit, not once for each copy:
// in an edit that has copied before, a node's map costs only its own copy,
// less than in an edit copying for the first time each time. It is not run in
// parallel: AllocsPerRun measures the process.
func TestEditor_KeepsEntryStorageFromCopyToCopy(t *testing.T) {
	ids := map[ir.TypeID]int{"t/plain": 0, "t/mapped": 8}
	doc, e := editedOnce(t, ids)
	kept := testing.AllocsPerRun(10, updating(e, "t/mapped")) - testing.AllocsPerRun(10, updating(e, "t/plain"))
	afresh := func(id ir.TypeID) func() {
		return func() { ir.NewEditor(doc).UpdateType(id, func(ir.TypeDef) {}) }
	}
	first := testing.AllocsPerRun(10, afresh("t/mapped")) - testing.AllocsPerRun(10, afresh("t/plain"))
	assert.Less(t, kept, first, "an edit that has copied a map before sets up nothing for the next")
}
