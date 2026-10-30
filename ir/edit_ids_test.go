package ir_test

import (
	"reflect"
	"slices"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dexpace/morphic/ir"
	"github.com/dexpace/morphic/ir/irtest"
)

// idsDoc returns a document of a model with properties p/a and p/b, an enum
// with member e/a, an operation, a group with a resource property and a
// service with a common error header. Each call builds new nodes.
func idsDoc() *ir.Document {
	return &ir.Document{
		IRVersion: ir.IRVersion,
		Types: ir.TypeRegistry{
			"t/m": &ir.Model{ID: "t/m", Properties: []ir.Property{{ID: "p/a"}, {ID: "p/b"}}},
			"t/e": &ir.Enum{ID: "t/e", Members: []ir.EnumMember{{ID: "e/a"}}},
		},
		Services: []ir.Service{{ID: "s/a", CommonErrors: []ir.ErrorCase{{Headers: []ir.Header{{ID: "p/h"}}}},
			Groups: []ir.OperationGroup{{ID: "g/a", Resource: &ir.ResourceInfo{Properties: []ir.Property{{ID: "p/r"}}},
				Operations: []ir.Operation{{ID: "o/a", Params: []ir.Parameter{{ID: "q/a"}}}}}}}},
	}
}

// model returns the model td is, failing the test when it is none.
func model(t *testing.T, td ir.TypeDef) *ir.Model {
	t.Helper()
	m, ok := td.(*ir.Model)
	require.True(t, ok, "%T is no model", td)
	return m
}

// TestEditor_UpdateRefusesAChangeAddingAnID pins that a change may declare no
// ID its node does not, nested ones included, nor one more often than the node
// does, whatever the node's kind: an edit adds no ID. A change removing or
// keeping a declaration, or adding a node with an empty ID, which declares
// nothing, is taken.
func TestEditor_UpdateRefusesAChangeAddingAnID(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name  string
		write func(t *testing.T, e *ir.Editor)
		want  string
	}{
		{"a property added", func(t *testing.T, e *ir.Editor) {
			e.UpdateType("t/m", func(td ir.TypeDef) {
				m := model(t, td)
				m.Properties = append(m.Properties, ir.Property{ID: "p/new"})
			})
		}, `the change of "t/m" declares the ir.PropID "p/new", which the node does not`},
		{"a property's ID changed", func(t *testing.T, e *ir.Editor) {
			e.UpdateType("t/m", func(td ir.TypeDef) { model(t, td).Properties[1].ID = "p/c" })
		}, `the change of "t/m" declares the ir.PropID "p/c", which the node does not`},
		{"a property repeated", func(t *testing.T, e *ir.Editor) {
			e.UpdateType("t/m", func(td ir.TypeDef) { model(t, td).Properties[1].ID = "p/a" })
		}, `the change of "t/m" declares the ir.PropID "p/a" 2 times, where the node does 1 times`},
		{"an enum member added", func(t *testing.T, e *ir.Editor) {
			e.UpdateType("t/e", func(td ir.TypeDef) {
				enum, _ := td.(*ir.Enum)
				enum.Members = append(enum.Members, ir.EnumMember{ID: "e/b"})
			})
		}, `the change of "t/e" declares the ir.EnumMemberID "e/b", which the node does not`},
		{"the least of several added", func(t *testing.T, e *ir.Editor) {
			e.UpdateType("t/m", func(td ir.TypeDef) {
				m := model(t, td)
				m.Properties = append(m.Properties, ir.Property{ID: "p/z", Args: []ir.Parameter{{ID: "q/z"}}},
					ir.Property{ID: "p/y"})
			})
		}, `the change of "t/m" declares the ir.ParamID "q/z", which the node does not`},
		{"the least of several of one class added", func(t *testing.T, e *ir.Editor) {
			e.UpdateType("t/m", func(td ir.TypeDef) {
				m := model(t, td)
				m.Properties = append(m.Properties, ir.Property{ID: "p/z"}, ir.Property{ID: "p/y"})
			})
		}, `the change of "t/m" declares the ir.PropID "p/y", which the node does not`},
		{"a parameter added", func(t *testing.T, e *ir.Editor) {
			e.UpdateOperation("o/a", func(op *ir.Operation) { op.Params = append(op.Params, ir.Parameter{ID: "q/b"}) })
		}, `the change of "o/a" declares the ir.ParamID "q/b", which the node does not`},
		{"a resource property added", func(t *testing.T, e *ir.Editor) {
			e.UpdateGroup("g/a", func(g *ir.OperationGroup) {
				g.Resource.Identifiers = []ir.Property{{ID: "p/r2"}}
			})
		}, `the change of "g/a" declares the ir.PropID "p/r2", which the node does not`},
		{"a service's error header renamed", func(t *testing.T, e *ir.Editor) {
			e.UpdateService("s/a", func(s *ir.Service) { s.CommonErrors[0].Headers[0].ID = "p/h2" })
		}, `the change of "s/a" declares the ir.PropID "p/h2", which the node does not`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			doc := idsDoc()
			e := ir.NewEditor(doc)
			tt.write(t, e)
			out, diags := e.Finish()

			assert.Equal(t, []string{"internal: edit: update rejected: " + tt.want + "; an edit adds no ID"},
				refusals(t, diags))
			assert.Equal(t, irtest.Encoded(t, idsDoc()), irtest.Encoded(t, out), "the refused change was applied")
		})
	}
}

// TestEditor_UpdateTakesAChangeAddingNoID pins the changes the ID check lets
// through: removing a declaration, reordering, and adding a node whose ID is
// empty, which irverify reports and nothing can reference.
func TestEditor_UpdateTakesAChangeAddingNoID(t *testing.T) {
	t.Parallel()
	e := ir.NewEditor(idsDoc())
	e.UpdateType("t/m", func(td ir.TypeDef) {
		m := model(t, td)
		m.Properties = []ir.Property{m.Properties[1], {}}
	})
	e.UpdateOperation("o/a", func(op *ir.Operation) { op.Params = nil })
	out, diags := e.Finish()

	require.Empty(t, diags)
	assert.Equal(t, []ir.Property{{ID: "p/b"}, {}}, model(t, out.Types["t/m"]).Properties)
}

// TestEditor_ReplaceRefusesANodeAddingAnID pins that a replacement may declare
// no ID the node it replaces does not, beside its own, which keys it: one of
// another kind declaring fewer is taken, and so is one replacing a nil node or
// one off its key, whose own IDs are not the key's, or one at the empty ID,
// which declares nothing.
func TestEditor_ReplaceRefusesANodeAddingAnID(t *testing.T) {
	t.Parallel()
	doc := idsDoc()
	doc.Types["t/nil"] = nil
	doc.Types["t/off"] = &ir.Model{ID: "t/other", Properties: []ir.Property{{ID: "p/o"}}}
	doc.Types[""] = &ir.Model{}

	e := ir.NewEditor(doc)
	e.ReplaceType(&ir.Any{})
	e.ReplaceType(&ir.Model{ID: "t/m", Properties: []ir.Property{{ID: "p/b"}, {ID: "p/new"}}})
	e.ReplaceType(&ir.Model{ID: "t/e", Properties: []ir.Property{{ID: "e/a"}}})
	e.ReplaceType(&ir.Model{ID: "t/m", Properties: []ir.Property{{ID: "p/b"}, {ID: "p/a"}}})
	e.ReplaceType(&ir.Primitive{ID: "t/e", Prim: ir.PrimString})
	e.ReplaceType(&ir.Any{ID: "t/nil"})
	e.ReplaceType(&ir.Model{ID: "t/off", Properties: []ir.Property{{ID: "p/o"}}})
	out, diags := e.Finish()

	assert.Equal(t, []string{
		`internal: edit: replace rejected: the node for "t/m" declares the ir.PropID "p/new", ` +
			`which the node it replaces does not; an edit adds no ID`,
		`internal: edit: replace rejected: the node for "t/e" declares the ir.PropID "e/a", ` +
			`which the node it replaces does not; an edit adds no ID`,
	}, refusals(t, diags))
	assert.Equal(t, []ir.Property{{ID: "p/b"}, {ID: "p/a"}}, model(t, out.Types["t/m"]).Properties)
	assert.Equal(t, ir.KindPrimitive, out.Types["t/e"].Kind())
	assert.Equal(t, ir.KindAny, out.Types["t/nil"].Kind())
	assert.Equal(t, ir.TypeID("t/off"), out.Types["t/off"].Common().ID)
}

// TestEditor_HoldsEachNodeToItsOwnIDs pins that the ID check starts afresh at
// each call: an ID the last node updated or replaced declares is one the next
// node, which does not, still may not add.
func TestEditor_HoldsEachNodeToItsOwnIDs(t *testing.T) {
	t.Parallel()
	e := ir.NewEditor(idsDoc())
	e.UpdateType("t/m", func(ir.TypeDef) {})
	e.UpdateGroup("g/a", func(g *ir.OperationGroup) { g.Resource.Identifiers = []ir.Property{{ID: "p/a"}} })
	e.UpdateType("t/m", func(ir.TypeDef) {})
	e.ReplaceType(&ir.Model{ID: "t/e", Properties: []ir.Property{{ID: "p/a"}}})
	out, diags := e.Finish()

	assert.Equal(t, []string{
		`internal: edit: update rejected: the change of "g/a" declares the ir.PropID "p/a", ` +
			`which the node does not; an edit adds no ID`,
		`internal: edit: replace rejected: the node for "t/e" declares the ir.PropID "p/a", ` +
			`which the node it replaces does not; an edit adds no ID`,
	}, refusals(t, diags))
	assert.Equal(t, irtest.Encoded(t, idsDoc()), irtest.Encoded(t, out))
}

// TestEditor_ChecksIDsWhereverTheyNestAndOnlyThere pins where the ID check
// reads: an ID minted or repeated deep inside an operation's request, behind a
// pointer, a slice or a map, is found, while
// a default nested past ir.MaxWalkDepth, which can hold no declaration, is not
// read at all, so neither a change nor a replacement nesting one is refused.
func TestEditor_ChecksIDsWhereverTheyNestAndOnlyThere(t *testing.T) {
	t.Parallel()
	deep := []ir.Property{{ID: "p/a", Default: nestedValue(ir.MaxWalkDepth)}}
	doc := idsDoc()
	op := &doc.Services[0].Groups[0].Operations[0]
	op.Request = &ir.Payload{Contents: []ir.Content{{
		ItemEncoding: &ir.PartEncoding{Headers: []ir.Header{{ID: "p/i", Args: []ir.Parameter{{ID: "q/i"}}}}},
		Encoding:     map[ir.PropID]ir.PartEncoding{"p/part": {Headers: []ir.Header{{ID: "p/e"}}}},
	}}}

	e := ir.NewEditor(doc)
	e.UpdateOperation("o/a", func(op *ir.Operation) {
		header := &op.Request.Contents[0].ItemEncoding.Headers[0]
		header.Args = append(header.Args, ir.Parameter{ID: "q/minted"})
	})
	e.UpdateOperation("o/a", func(op *ir.Operation) {
		part := op.Request.Contents[0].Encoding["p/part"]
		part.Headers = append(part.Headers, ir.Header{ID: "p/minted"})
		op.Request.Contents[0].Encoding["p/part"] = part
	})
	e.UpdateOperation("o/a", func(op *ir.Operation) {
		part := op.Request.Contents[0].Encoding["p/part"]
		op.Request.Contents[0].Encoding["p/other"] = part
	})
	e.UpdateType("t/m", func(td ir.TypeDef) { model(t, td).Properties = append(model(t, td).Properties[:0], deep...) })
	e.ReplaceType(&ir.Model{ID: "t/m", Properties: deep})
	out, diags := e.Finish()

	assert.Equal(t, []string{
		`internal: edit: update rejected: the change of "o/a" declares the ir.ParamID "q/minted", ` +
			`which the node does not; an edit adds no ID`,
		`internal: edit: update rejected: the change of "o/a" declares the ir.PropID "p/minted", ` +
			`which the node does not; an edit adds no ID`,
		`internal: edit: update rejected: the change of "o/a" declares the ir.PropID "p/e" 2 times, ` +
			`where the node does 1 times; an edit adds no ID`,
	}, refusals(t, diags))
	assert.Equal(t, deep, model(t, out.Types["t/m"]).Properties)
}

// TestEditor_IDCheckEntersOnlyWhatCanDeclare holds the ID check's cost to
// what in a node can declare an ID: an update of a model whose property holds
// a default nesting a thousand levels, constraints and examples enters no more
// values than one whose property holds none of them.
func TestEditor_IDCheckEntersOnlyWhatCanDeclare(t *testing.T) {
	t.Parallel()
	scanned := func(p ir.Property) int {
		e := ir.NewEditor(&ir.Document{Types: ir.TypeRegistry{"t/m": &ir.Model{ID: "t/m", Properties: []ir.Property{p}}}})
		e.UpdateType("t/m", func(ir.TypeDef) {})
		_, diags := e.Finish()
		require.Empty(t, diags)
		return ir.IDsScanned(e)
	}

	bare := scanned(ir.Property{ID: "p/a"})
	full := scanned(ir.Property{ID: "p/a", Default: nestedValue(1000), Constraints: &ir.Constraints{Pattern: "^a$"},
		Examples: []ir.Example{{Value: nestedValue(10)}}})

	assert.Positive(t, bare)
	assert.Equal(t, bare, full, "the check entered what can declare no ID")
}

// TestEditor_IDCheckSortsOnlyWhatChangedOrder holds the ID check to sorting
// the IDs a node declares only when a change reorders them: a change leaving
// them be costs no sort, and one reordering them is checked by sorting.
func TestEditor_IDCheckSortsOnlyWhatChangedOrder(t *testing.T) {
	t.Parallel()
	e := ir.NewEditor(idsDoc())
	e.UpdateType("t/m", func(td ir.TypeDef) { td.Common().Docs.Summary = "kept in order" })
	untouched := ir.IDListsSorted(e)
	e.UpdateType("t/m", func(td ir.TypeDef) { slices.Reverse(model(t, td).Properties) })
	_, diags := e.Finish()

	require.Empty(t, diags)
	assert.Zero(t, untouched, "a change keeping the order was sorted")
	assert.Positive(t, ir.IDListsSorted(e), "a reordering change was not checked by sorting")
}

// TestEditor_IDCheckReadsABoundedPartOfANode holds the IR's types to what
// ends the ID check's scan, which has no depth cap: entering only what can
// declare an ID, from any node an update or a replacement hands it, it meets
// no type twice on one path, so no value holds itself there. The one type that
// can, an operation group through its groups, is held apart by every update.
func TestEditor_IDCheckReadsABoundedPartOfANode(t *testing.T) {
	t.Parallel()
	var ids ir.IDReader
	apart := map[reflect.Type][]string{
		reflect.TypeFor[ir.Document]():       keyedFields,
		reflect.TypeFor[ir.Service]():        {"Groups"},
		reflect.TypeFor[ir.OperationGroup](): {"Groups", "Operations"},
	}
	held := func(typ reflect.Type) []reflect.Type {
		switch typ.Kind() {
		case reflect.Pointer, reflect.Slice, reflect.Array:
			return []reflect.Type{typ.Elem()}
		case reflect.Map:
			return []reflect.Type{typ.Key(), typ.Elem()}
		case reflect.Struct:
			var out []reflect.Type
			for f := range typ.Fields() {
				if !slices.Contains(apart[typ], f.Name) {
					out = append(out, f.Type)
				}
			}
			return out
		default:
			return nil
		}
	}
	canHold := map[reflect.Type]bool{}
	var holds func(reflect.Type, map[reflect.Type]bool) bool
	holds = func(typ reflect.Type, seen map[reflect.Type]bool) bool {
		if _, _, declares := ids.DeclaredIDOf(reflect.New(typ).Elem()); declares {
			return true
		}
		seen[typ] = true
		return slices.ContainsFunc(held(typ), func(h reflect.Type) bool { return !seen[h] && holds(h, seen) })
	}
	var path []reflect.Type
	var walk func(reflect.Type)
	walk = func(typ reflect.Type) {
		if _, known := canHold[typ]; !known {
			canHold[typ] = holds(typ, map[reflect.Type]bool{})
		}
		if !canHold[typ] {
			return
		}
		require.NotContains(t, path, typ, "a node can hold %s inside itself, through %v", typ, path)
		path = append(path, typ)
		for _, h := range held(typ) {
			walk(h)
		}
		path = path[:len(path)-1]
	}
	roots := make([]reflect.Type, 0, 8+len(allKinds))
	roots = append(roots, reflect.TypeFor[ir.Document](), reflect.TypeFor[ir.Service](),
		reflect.TypeFor[ir.OperationGroup](), reflect.TypeFor[ir.Operation](), reflect.TypeFor[ir.Server](),
		reflect.TypeFor[ir.Channel](), reflect.TypeFor[ir.Message](), reflect.TypeFor[ir.AuthScheme]())
	for _, k := range allKinds {
		td, ok := ir.NewTypeDef(k)
		require.True(t, ok)
		roots = append(roots, reflect.TypeOf(td))
	}
	for _, root := range roots {
		walk(root)
	}
	assert.True(t, canHold[reflect.TypeFor[ir.Parameter]()], "the check must reach past a node's own ID")
}
