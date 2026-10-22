package ir_test

import (
	"encoding/json/v2"
	"fmt"
	"maps"
	"reflect"
	"slices"
	"strings"
	"testing"

	"github.com/google/go-cmp/cmp"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dexpace/morphic/ir"
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

// encoded returns doc's canonical bytes, which change when anything doc
// reaches does.
func encoded(t *testing.T, doc *ir.Document) string {
	t.Helper()
	raw, err := json.Marshal(doc)
	require.NoError(t, err)
	return string(raw)
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
			before, nodes := encoded(t, doc), maps.Clone(doc.Types)

			e := ir.Edit(doc)
			tt.write(e)
			out, diags := e.Done()

			assert.Empty(t, diags)
			tt.want(t, out.Types)
			assert.Equal(t, before, encoded(t, doc), "the edited document changed")
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

// TestEditor_DoneSharesWhatTheEditLeft pins Done's document: new, so a caller
// may set its fields, sharing every field but the registry with the document
// edited, and the registry itself until a write.
func TestEditor_DoneSharesWhatTheEditLeft(t *testing.T) {
	t.Parallel()
	doc := editedDoc()
	doc.Services = []ir.Service{{ID: "s/x"}}

	untouched, _ := ir.Edit(doc).Done()
	e := ir.Edit(doc)
	e.RemoveType("t/scalar")
	edited, _ := e.Done()
	edited.Name = "renamed"

	require.NotSame(t, doc, untouched)
	assert.Equal(t, reflect.ValueOf(doc.Types).Pointer(), reflect.ValueOf(untouched.Types).Pointer(),
		"an edit that writes nothing copies no registry")
	assert.NotEqual(t, reflect.ValueOf(doc.Types).Pointer(), reflect.ValueOf(edited.Types).Pointer())
	assert.Same(t, &doc.Services[0], &edited.Services[0], "a field the edit does not write is shared")
	assert.Same(t, doc.Types["t/model"], edited.Types["t/model"], "a node the edit does not write is shared")
	assert.Equal(t, "edited", doc.Name, "setting a field of Done's document set the edited one's")
}

// TestEditor_TypeReadsTheEdit pins that Type answers with what the edit holds:
// a written node, no node once removed, and the edited document's otherwise.
func TestEditor_TypeReadsTheEdit(t *testing.T) {
	t.Parallel()
	doc := editedDoc()
	replacement := &ir.Any{ID: "t/list"}

	e := ir.Edit(doc)
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

// TestEditor_RefusesWritesOnlyABugMakes pins every refusal: the one message
// Done reports, and that the refused write leaves the edit's document as a
// control edit making only the writes before it leaves its own.
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
		}, `update rejected: the change moved "t/model" to "t/moved"; a type keeps its ID`},
		{"update emptying the ID", nil, func(e *ir.Editor, _ *ir.Document) {
			e.UpdateType("t/model", func(td ir.TypeDef) { *td.Common() = ir.TypeCommon{} })
		}, `update rejected: the change moved "t/model" to ""`},
		{"a change using the edit", nil, func(e *ir.Editor, _ *ir.Document) {
			e.UpdateType("t/model", func(ir.TypeDef) { e.RemoveType("t/scalar") })
		}, `remove rejected: an UpdateType change used the edit ("t/scalar"); a change alters its node alone`},
		{"a change ending the edit", nil, func(e *ir.Editor, _ *ir.Document) {
			e.UpdateType("t/model", func(ir.TypeDef) { e.Done() })
		}, `update rejected: the change of "t/model" ended the edit`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			doc, control := editedDoc(), editedDoc()
			e, c := ir.Edit(doc), ir.Edit(control)
			if tt.before != nil {
				tt.before(e, doc)
				tt.before(c, control)
			}
			tt.refused(e, doc)

			out, diags := e.Done()
			want, ctrl := c.Done()

			require.Empty(t, ctrl, "a write before the refused one was refused as well, so the case tests the wrong write")
			got := refusals(t, diags)
			require.Len(t, got, 1, "one refused write, one refusal: %v", got)
			assert.Contains(t, got[0], "internal: the type edit refused a write: "+tt.want)
			assert.Equal(t, encoded(t, want), encoded(t, out), "the refused write changed the edit")
			assert.Equal(t, encoded(t, editedDoc()), encoded(t, doc), "the edited document changed")
		})
	}
}

// TestEditor_NilEntryIsReplacedNotUpdated pins the edit of a registry entry
// holding nil, untyped or typed, which irverify reports: an update has nothing
// to copy and is refused, while a replacement or a removal takes the entry as
// any other.
func TestEditor_NilEntryIsReplacedNotUpdated(t *testing.T) {
	t.Parallel()
	doc := &ir.Document{Types: ir.TypeRegistry{"t/a": nil, "t/b": (*ir.Model)(nil)}}
	replacement := &ir.Any{ID: "t/a"}

	e := ir.Edit(doc)
	e.UpdateType("t/a", func(ir.TypeDef) {})
	e.UpdateType("t/b", func(ir.TypeDef) {})
	e.ReplaceType(replacement)
	e.RemoveType("t/b")
	out, diags := e.Done()

	assert.Equal(t, []string{
		`internal: the type edit refused a write: update rejected: "t/a" holds a nil type definition, which has nothing to change`,
		`internal: the type edit refused a write: update rejected: "t/b" holds a nil type definition, which has nothing to change`,
	}, refusals(t, diags))
	assert.Equal(t, ir.TypeRegistry{"t/a": replacement}, out.Types)
	assert.Len(t, doc.Types, 2)
}

// TestEditor_UpdateRefusesANodeAtAnotherIDsKey pins the update of an entry
// whose node names another ID, which irverify reports: it is refused as what it
// is, not as a change that moved the node, and Done reports nothing for the
// entry the edit never took.
func TestEditor_UpdateRefusesANodeAtAnotherIDsKey(t *testing.T) {
	t.Parallel()
	doc := &ir.Document{Types: ir.TypeRegistry{"t/a": &ir.Any{ID: "t/b"}, "t/c": &ir.Any{ID: "t/c"}}}

	e := ir.Edit(doc)
	e.UpdateType("t/a", func(ir.TypeDef) {})
	e.RemoveType("t/c")
	_, diags := e.Done()

	assert.Equal(t, []string{`internal: the type edit refused a write: update rejected: ` +
		`"t/a" holds the node of "t/b", which belongs at its own ID`}, refusals(t, diags))
}

// TestEditor_DoneRefusesANodeMovedAfterTheEditTookIt pins that a node the
// edit took and its caller changed afterwards, off the key its ID named, is
// reported when the edit ends: one value replaced at two IDs, and a node an
// update's change kept.
func TestEditor_DoneRefusesANodeMovedAfterTheEditTookIt(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name  string
		write func(e *ir.Editor)
		want  string
	}{
		{"one value replaced at two IDs", func(e *ir.Editor) {
			n := &ir.Any{ID: "t/list"}
			e.ReplaceType(n)
			n.ID = "t/scalar"
			e.ReplaceType(n)
		}, `done: "t/list" holds a node moved to "t/scalar" after the edit took it; a type keeps its ID`},
		{"an updated node kept", func(e *ir.Editor) {
			var kept ir.TypeDef
			e.UpdateType("t/model", func(td ir.TypeDef) { kept = td })
			kept.Common().ID = "t/moved"
		}, `done: "t/model" holds a node moved to "t/moved" after the edit took it; a type keeps its ID`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			doc := editedDoc()
			e := ir.Edit(doc)
			tt.write(e)

			_, diags := e.Done()
			_, again := e.Done()

			assert.Equal(t, []string{"internal: the type edit refused a write: " + tt.want}, refusals(t, diags))
			assert.Empty(t, again, "a moved node is reported once")
			assert.Equal(t, encoded(t, editedDoc()), encoded(t, doc), "the edited document changed")
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

// TestEditor_DoneReportsEachRefusalOnce pins that Done ends the edit: a later
// call returns the same document, with only the refusals made since, and every
// write after the first call is refused, the document unchanged.
func TestEditor_DoneReportsEachRefusalOnce(t *testing.T) {
	t.Parallel()
	e := ir.Edit(editedDoc())
	e.RemoveType("t/absent")
	first, firstDiags := e.Done()
	before := encoded(t, first)

	e.RemoveType("t/scalar")
	e.ReplaceType(&ir.Any{ID: "t/list"})
	e.UpdateType("t/model", func(td ir.TypeDef) { td.Common().Access = "internal" })
	second, secondDiags := e.Done()
	third, thirdDiags := e.Done()

	assert.Equal(t, []string{`internal: the type edit refused a write: remove rejected: ` +
		`no type "t/absent" to remove; an edit adds no type`}, refusals(t, firstDiags))
	assert.Equal(t, []string{
		`internal: the type edit refused a write: remove rejected: the edit is done ("t/scalar")`,
		`internal: the type edit refused a write: replace rejected: the edit is done ("t/list")`,
		`internal: the type edit refused a write: update rejected: the edit is done ("t/model")`,
	}, refusals(t, secondDiags))
	assert.Empty(t, thirdDiags)
	assert.Same(t, first, second)
	assert.Same(t, first, third)
	assert.Equal(t, before, encoded(t, first), "a write after Done changed its document")
}

// TestEdit_NilDocumentIsRefused pins that an edit of no document still ends in
// one, an empty one, beside the refusal saying why.
func TestEdit_NilDocumentIsRefused(t *testing.T) {
	t.Parallel()
	e := ir.Edit(nil)
	e.RemoveType("t/x")

	out, diags := e.Done()

	require.NotNil(t, out)
	assert.Empty(t, cmp.Diff(&ir.Document{}, out))
	assert.Equal(t, []string{
		"internal: the type edit refused a write: edit rejected: no document to edit",
		`internal: the type edit refused a write: remove rejected: no type "t/x" to remove; an edit adds no type`,
	}, refusals(t, diags))
}

// references returns the address of every pointer, non-empty slice and map v
// reaches, each with the path it was first reached at.
func references(v any) map[uintptr]string {
	refs := map[uintptr]string{}
	ir.WalkValues(v, "node", func(rv reflect.Value, path string) bool {
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
	})
	return refs
}

// updatedCopy returns the node an edit holds at td's own ID after an update
// that changes nothing, which is the copy UpdateType made of td.
func updatedCopy(t *testing.T, td ir.TypeDef) ir.TypeDef {
	t.Helper()
	id := td.Common().ID
	e := ir.Edit(&ir.Document{Types: ir.TypeRegistry{id: td}})
	e.UpdateType(id, func(ir.TypeDef) {})
	out, diags := e.Done()
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

// TestEditor_CopiesEveryIRTypeWhole holds every type a TypeDef reaches to what
// the copy UpdateType makes can copy: no kind it would share (an interface, an
// array, a channel, a function), and no unexported field, which reflection
// cannot set and the copy would leave shared.
func TestEditor_CopiesEveryIRTypeWhole(t *testing.T) {
	t.Parallel()
	queue := make([]reflect.Type, 0, len(allKinds))
	for _, k := range allKinds {
		td, ok := ir.NewTypeDef(k)
		require.True(t, ok)
		queue = append(queue, reflect.TypeOf(td))
	}
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
				queue = append(queue, f.Type)
			}
		case reflect.Interface, reflect.Array, reflect.Chan, reflect.Func, reflect.UnsafePointer:
			t.Errorf("%s is a %s, which the copy would share", typ, typ.Kind())
		default: // a scalar is its own copy
		}
	}
	assert.Greater(t, len(seen), len(allKinds), "the walk must reach past the kinds themselves")
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

			e := ir.Edit(doc)
			e.UpdateType("t/deep", func(td ir.TypeDef) { td.Common().Access = "internal" })
			out, diags := e.Done()

			walkCut := ir.WalkValues(node, "node", func(reflect.Value, string) bool { return true })
			require.Equal(t, tc.refused, walkCut, "the fixture is not on the cap the walk keeps")
			if !tc.refused {
				assert.Empty(t, diags)
				assert.Equal(t, "internal", out.Types["t/deep"].Common().Access)
				return
			}
			assert.Equal(t, []string{fmt.Sprintf(`internal: the type edit refused a write: update rejected: `+
				`"t/deep" nests deeper than %d levels, past what an edit copies`, ir.MaxWalkDepth)},
				refusals(t, diags))
			assert.Same(t, node, out.Types["t/deep"])
		})
	}
}

// ExampleEdit removes the types nothing references, as a filter pass would:
// the document it edits keeps them, and the one Done returns does not.
func ExampleEdit() {
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

	e := ir.Edit(doc)
	for _, id := range slices.Sorted(maps.Keys(doc.Types)) {
		if !referenced[id] {
			e.RemoveType(id)
		}
	}
	out, diags := e.Done()

	fmt.Println(slices.Sorted(maps.Keys(out.Types)), len(diags))
	fmt.Println(slices.Sorted(maps.Keys(doc.Types)))
	// Output:
	// [t/elem t/kept] 0
	// [t/elem t/kept t/lost]
}
