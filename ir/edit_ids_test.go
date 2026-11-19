// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package ir_test

import (
	"fmt"
	"reflect"
	"slices"
	"strings"
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
			"type/m": &ir.Model{ID: "type/m", Properties: []ir.Property{{ID: "property/a"}, {ID: "property/b"}}},
			"type/e": &ir.Enum{ID: "type/e", Members: []ir.EnumMember{{ID: "enum-member/a"}}},
		},
		Services: []ir.Service{{ID: "service/a", CommonErrors: []ir.ErrorCase{{Headers: []ir.Header{{ID: "property/h"}}}},
			Groups: []ir.OperationGroup{{ID: "operation-group/a", Resource: &ir.ResourceInfo{Properties: []ir.Property{{ID: "property/r"}}},
				Operations: []ir.Operation{{ID: "o/a", Params: []ir.Parameter{{ID: "q/a"}}}}}}}},
	}
}

// model returns the model td is, failing the test when it is none.
func model(t *testing.T, td ir.TypeDef) *ir.Model {
	t.Helper()
	modelValue, ok := td.(*ir.Model)
	require.True(t, ok, "%T is no model", td)
	return modelValue
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
		write func(t *testing.T, editor *ir.Editor)
		want  string
	}{
		{"a property added", func(t *testing.T, editor *ir.Editor) {
			editor.UpdateType("type/m", func(td ir.TypeDef) {
				modelValue := model(t, td)
				modelValue.Properties = append(modelValue.Properties, ir.Property{ID: "property/new"})
			})
		}, `the change of "type/m" declares the ir.PropertyID "property/new", which the node does not`},
		{"a property's ID changed", func(t *testing.T, editor *ir.Editor) {
			editor.UpdateType("type/m", func(td ir.TypeDef) { model(t, td).Properties[1].ID = "property/c" })
		}, `the change of "type/m" declares the ir.PropertyID "property/c", which the node does not`},
		{"a property repeated", func(t *testing.T, editor *ir.Editor) {
			editor.UpdateType("type/m", func(td ir.TypeDef) { model(t, td).Properties[1].ID = "property/a" })
		}, `the change of "type/m" declares the ir.PropertyID "property/a" 2 times, where the node does 1 times`},
		{"an enum member added", func(t *testing.T, editor *ir.Editor) {
			editor.UpdateType("type/e", func(td ir.TypeDef) {
				enum, _ := td.(*ir.Enum)
				enum.Members = append(enum.Members, ir.EnumMember{ID: "enum-member/b"})
			})
		}, `the change of "type/e" declares the ir.EnumMemberID "enum-member/b", which the node does not`},
		{"the least of several added", func(t *testing.T, editor *ir.Editor) {
			editor.UpdateType("type/m", func(td ir.TypeDef) {
				modelValue := model(t, td)
				modelValue.Properties = append(modelValue.Properties, ir.Property{ID: "property/z", Args: []ir.Parameter{{ID: "q/z"}}},
					ir.Property{ID: "property/y"})
			})
		}, `the change of "type/m" declares the ir.ParameterID "q/z", which the node does not`},
		{"the least of several of one class added", func(t *testing.T, editor *ir.Editor) {
			editor.UpdateType("type/m", func(td ir.TypeDef) {
				modelValue := model(t, td)
				modelValue.Properties = append(modelValue.Properties, ir.Property{ID: "property/z"}, ir.Property{ID: "property/y"})
			})
		}, `the change of "type/m" declares the ir.PropertyID "property/y", which the node does not`},
		{"a parameter added", func(t *testing.T, editor *ir.Editor) {
			editor.UpdateOperation("o/a", func(operation *ir.Operation) { operation.Params = append(operation.Params, ir.Parameter{ID: "q/b"}) })
		}, `the change of "o/a" declares the ir.ParameterID "q/b", which the node does not`},
		{"a resource property added", func(t *testing.T, editor *ir.Editor) {
			editor.UpdateGroup("operation-group/a", func(operationGroup *ir.OperationGroup) {
				operationGroup.Resource.Identifiers = []ir.Property{{ID: "property/r2"}}
			})
		}, `the change of "operation-group/a" declares the ir.PropertyID "property/r2", which the node does not`},
		{"a service's error header renamed", func(t *testing.T, editor *ir.Editor) {
			editor.UpdateService("service/a", func(service *ir.Service) { service.CommonErrors[0].Headers[0].ID = "property/h2" })
		}, `the change of "service/a" declares the ir.PropertyID "property/h2", which the node does not`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			doc := idsDoc()
			editor := ir.NewEditor(doc)
			tt.write(t, editor)
			out, diags := editor.Finish()

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
	editor := ir.NewEditor(idsDoc())
	editor.UpdateType("type/m", func(td ir.TypeDef) {
		modelValue := model(t, td)
		modelValue.Properties = []ir.Property{modelValue.Properties[1], {}}
	})
	editor.UpdateOperation("o/a", func(operation *ir.Operation) { operation.Params = nil })
	out, diags := editor.Finish()

	require.Empty(t, diags)
	assert.Equal(t, []ir.Property{{ID: "property/b"}, {}}, model(t, out.Types["type/m"]).Properties)
}

// TestEditor_ReplaceRefusesANodeAddingAnID pins that a replacement may declare
// no ID the node it replaces does not, beside its own, which keys it: one of
// another kind declaring fewer is taken, and so is one replacing a nil node or
// one off its key, whose own IDs are not the key's, or one at the empty ID,
// which declares nothing.
func TestEditor_ReplaceRefusesANodeAddingAnID(t *testing.T) {
	t.Parallel()
	doc := idsDoc()
	doc.Types["type/nil"] = nil
	doc.Types["type/off"] = &ir.Model{ID: "type/other", Properties: []ir.Property{{ID: "property/o"}}}
	doc.Types[""] = &ir.Model{}

	editor := ir.NewEditor(doc)
	editor.ReplaceType(&ir.Any{})
	editor.ReplaceType(&ir.Model{ID: "type/m", Properties: []ir.Property{{ID: "property/b"}, {ID: "property/new"}}})
	editor.ReplaceType(&ir.Model{ID: "type/e", Properties: []ir.Property{{ID: "enum-member/a"}}})
	editor.ReplaceType(&ir.Model{ID: "type/m", Properties: []ir.Property{{ID: "property/b"}, {ID: "property/a"}}})
	editor.ReplaceType(&ir.Primitive{ID: "type/e", PrimitiveKind: ir.PrimitiveString})
	editor.ReplaceType(&ir.Any{ID: "type/nil"})
	editor.ReplaceType(&ir.Model{ID: "type/off", Properties: []ir.Property{{ID: "property/o"}}})
	out, diags := editor.Finish()

	assert.Equal(t, []string{
		`internal: edit: replace rejected: the node for "type/m" declares the ir.PropertyID "property/new", ` +
			`which the node it replaces does not; an edit adds no ID`,
		`internal: edit: replace rejected: the node for "type/e" declares the ir.PropertyID "enum-member/a", ` +
			`which the node it replaces does not; an edit adds no ID`,
	}, refusals(t, diags))
	assert.Equal(t, []ir.Property{{ID: "property/b"}, {ID: "property/a"}}, model(t, out.Types["type/m"]).Properties)
	assert.Equal(t, ir.KindPrimitive, out.Types["type/e"].Kind())
	assert.Equal(t, ir.KindAny, out.Types["type/nil"].Kind())
	assert.Equal(t, ir.TypeID("type/off"), out.Types["type/off"].Common().ID)
}

// membersDoc returns a document declaring the namespace x for types and enum
// members, holding a union at t/x/u and an enum at t/x/e with the member a.
// Each call builds new nodes.
func membersDoc() *ir.Document {
	return &ir.Document{
		IRVersion: ir.IRVersion,
		IDSpaces:  map[string][]string{ir.IDKindType: {"x"}, ir.IDKindEnumMember: {"x"}},
		Types: ir.TypeRegistry{
			"type/x/u": &ir.Union{ID: "type/x/u"},
			"type/x/e": &ir.Enum{ID: "type/x/e", Members: []ir.EnumMember{member("enum-member/x/e/string:a", "a")}},
		},
	}
}

// member returns the enum member identified by id holding the string value.
func member(id ir.EnumMemberID, value string) ir.EnumMember {
	return ir.EnumMember{ID: id, Value: ir.Value{Kind: ir.ValueString, String: value}}
}

// TestEditor_TakesAnEnumMembersOwnID pins the one ID an edit may add: an enum
// member's own, the one its enum's ID and its value give it, a repeat's
// included. A union replaced by the enum of its values declares them, and so
// does an update adding a member.
func TestEditor_TakesAnEnumMembersOwnID(t *testing.T) {
	t.Parallel()
	enum := &ir.Enum{ID: "type/x/u", Members: []ir.EnumMember{
		member("enum-member/x/u/string:a", "a"), member("enum-member/x/u/string:a/2", "a"), member("enum-member/x/u/string:b", "b"),
	}}
	editor := ir.NewEditor(membersDoc())
	editor.ReplaceType(enum)
	editor.UpdateType("type/x/e", func(td ir.TypeDef) {
		added, _ := td.(*ir.Enum)
		added.Members = append(added.Members, member("enum-member/x/e/string:c", "c"))
	})
	out, diags := editor.Finish()

	require.Empty(t, diags)
	assert.Same(t, enum, out.Types["type/x/u"])
	updated, _ := out.Types["type/x/e"].(*ir.Enum)
	assert.Equal(t, []ir.EnumMember{member("enum-member/x/e/string:a", "a"), member("enum-member/x/e/string:c", "c")}, updated.Members)
}

// TestEditor_RefusesAMemberIDNotItsOwn pins what keeps the exemption to the
// member's own ID: one its value does not give it, alone or swapped with the
// member it belongs to, one beneath another enum, a repeat's ID worn by the
// first, a value with no key, and a namespace the document declares for types
// but not for members are each refused, by a replacement and by an update
// alike.
func TestEditor_RefusesAMemberIDNotItsOwn(t *testing.T) {
	t.Parallel()
	list := ir.Value{Kind: ir.ValueList}
	tests := []struct {
		name     string
		members  []ir.EnumMember
		memberNS []string
		want     string
	}{
		{"another value's ID", []ir.EnumMember{member("enum-member/x/u/string:a", "b")}, []string{"x"},
			`the ir.EnumMemberID "enum-member/x/u/string:a", which %s does not`},
		{"an ID beneath another enum", []ir.EnumMember{member("enum-member/x/e/string:a", "a")}, []string{"x"},
			`the ir.EnumMemberID "enum-member/x/e/string:a", which %s does not`},
		{"two values' IDs swapped", []ir.EnumMember{member("enum-member/x/u/string:b", "a"), member("enum-member/x/u/string:a", "b")},
			[]string{"x"}, `the ir.EnumMemberID "enum-member/x/u/string:a", which %s does not`},
		{"the first value's ID worn by its repeat", []ir.EnumMember{member("enum-member/x/u/string:a", "a"), member("enum-member/x/u/string:a", "a")},
			[]string{"x"}, `the ir.EnumMemberID "enum-member/x/u/string:a" 2 times, where %s does 1 times`},
		{"a value with no key, under the ID an empty key gives", []ir.EnumMember{{ID: "enum-member/x/u/", Value: list}},
			[]string{"x"}, `the ir.EnumMemberID "enum-member/x/u/", which %s does not`},
		{"a namespace not declared for members", []ir.EnumMember{member("enum-member/x/u/string:a", "a")}, []string{"y"},
			`the ir.EnumMemberID "enum-member/x/u/string:a", which %s does not`},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			doc := membersDoc()
			doc.IDSpaces[ir.IDKindEnumMember] = tc.memberNS
			doc.Types["type/x/v"] = &ir.Enum{ID: "type/x/v"}
			editor := ir.NewEditor(doc)
			editor.ReplaceType(&ir.Enum{ID: "type/x/u", Members: tc.members})
			editor.UpdateType("type/x/v", func(td ir.TypeDef) {
				enum, _ := td.(*ir.Enum)
				enum.Members = slices.Clone(tc.members)
				for i := range enum.Members {
					enum.Members[i].ID = ir.EnumMemberID(strings.Replace(string(enum.Members[i].ID), "/u/", "/v/", 1))
				}
			})
			out, diags := editor.Finish()

			replaced := fmt.Sprintf(tc.want, "the node it replaces")
			changed := strings.ReplaceAll(fmt.Sprintf(tc.want, "the node"), "/u/", "/v/")
			assert.Equal(t, []string{
				`internal: edit: replace rejected: the node for "type/x/u" declares ` + replaced + `; an edit adds no ID`,
				`internal: edit: update rejected: the change of "type/x/v" declares ` + changed + `; an edit adds no ID`,
			}, refusals(t, diags))
			assert.Equal(t, ir.KindUnion, out.Types["type/x/u"].Kind())
			assert.Empty(t, out.Types["type/x/v"].(*ir.Enum).Members)
		})
	}
}

// TestEditor_RefusesAMemberIDItsNodeDeclaresAlready pins that a member's own
// ID is one an edit may add only where its node does not declare it already: a
// change wearing it a second time, on a repeat of its value or on another
// value, is refused, by an update and by a replacement alike.
func TestEditor_RefusesAMemberIDItsNodeDeclaresAlready(t *testing.T) {
	t.Parallel()
	editor := ir.NewEditor(membersDoc())
	editor.UpdateType("type/x/e", func(td ir.TypeDef) {
		enum, _ := td.(*ir.Enum)
		enum.Members = append(enum.Members, member("enum-member/x/e/string:a", "a"))
	})
	editor.ReplaceType(&ir.Enum{ID: "type/x/e", Members: []ir.EnumMember{member("enum-member/x/e/string:a", "a"), member("enum-member/x/e/string:a", "b")}})
	out, diags := editor.Finish()

	assert.Equal(t, []string{
		`internal: edit: update rejected: the change of "type/x/e" declares the ir.EnumMemberID "enum-member/x/e/string:a" 2 times, ` +
			`where the node does 1 times; an edit adds no ID`,
		`internal: edit: replace rejected: the node for "type/x/e" declares the ir.EnumMemberID "enum-member/x/e/string:a" 2 times, ` +
			`where the node it replaces does 1 times; an edit adds no ID`,
	}, refusals(t, diags))
	assert.Equal(t, irtest.Encoded(t, membersDoc()), irtest.Encoded(t, out))
}

// TestEditor_HoldsEachNodeToItsOwnIDs pins that the ID check starts afresh at
// each call: an ID the last node updated or replaced declares is one the next
// node, which does not, still may not add.
func TestEditor_HoldsEachNodeToItsOwnIDs(t *testing.T) {
	t.Parallel()
	editor := ir.NewEditor(idsDoc())
	editor.UpdateType("type/m", func(ir.TypeDef) {})
	editor.UpdateGroup("operation-group/a", func(operationGroup *ir.OperationGroup) {
		operationGroup.Resource.Identifiers = []ir.Property{{ID: "property/a"}}
	})
	editor.UpdateType("type/m", func(ir.TypeDef) {})
	editor.ReplaceType(&ir.Model{ID: "type/e", Properties: []ir.Property{{ID: "property/a"}}})
	out, diags := editor.Finish()

	assert.Equal(t, []string{
		`internal: edit: update rejected: the change of "operation-group/a" declares the ir.PropertyID "property/a", ` +
			`which the node does not; an edit adds no ID`,
		`internal: edit: replace rejected: the node for "type/e" declares the ir.PropertyID "property/a", ` +
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
	deep := []ir.Property{{ID: "property/a", Default: nestedValue(ir.MaxWalkDepth)}}
	doc := idsDoc()
	operation := &doc.Services[0].Groups[0].Operations[0]
	operation.Request = &ir.Payload{Contents: []ir.Content{{
		ItemEncoding: &ir.PartEncoding{Headers: []ir.Header{{ID: "property/i", Args: []ir.Parameter{{ID: "q/i"}}}}},
		Encoding:     map[ir.PropertyID]ir.PartEncoding{"property/part": {Headers: []ir.Header{{ID: "property/e"}}}},
	}}}

	editor := ir.NewEditor(doc)
	editor.UpdateOperation("o/a", func(irOperation *ir.Operation) {
		header := &irOperation.Request.Contents[0].ItemEncoding.Headers[0]
		header.Args = append(header.Args, ir.Parameter{ID: "q/minted"})
	})
	editor.UpdateOperation("o/a", func(irOperation *ir.Operation) {
		part := irOperation.Request.Contents[0].Encoding["property/part"]
		part.Headers = append(part.Headers, ir.Header{ID: "property/minted"})
		irOperation.Request.Contents[0].Encoding["property/part"] = part
	})
	editor.UpdateOperation("o/a", func(irOperation *ir.Operation) {
		part := irOperation.Request.Contents[0].Encoding["property/part"]
		irOperation.Request.Contents[0].Encoding["property/other"] = part
	})
	editor.UpdateType("type/m", func(td ir.TypeDef) { model(t, td).Properties = append(model(t, td).Properties[:0], deep...) })
	editor.ReplaceType(&ir.Model{ID: "type/m", Properties: deep})
	out, diags := editor.Finish()

	assert.Equal(t, []string{
		`internal: edit: update rejected: the change of "o/a" declares the ir.ParameterID "q/minted", ` +
			`which the node does not; an edit adds no ID`,
		`internal: edit: update rejected: the change of "o/a" declares the ir.PropertyID "property/minted", ` +
			`which the node does not; an edit adds no ID`,
		`internal: edit: update rejected: the change of "o/a" declares the ir.PropertyID "property/e" 2 times, ` +
			`where the node does 1 times; an edit adds no ID`,
	}, refusals(t, diags))
	assert.Equal(t, deep, model(t, out.Types["type/m"]).Properties)
}

// TestEditor_IDCheckEntersOnlyWhatCanDeclare holds the ID check's cost to
// what in a node can declare an ID: an update of a model whose property holds
// a default nesting a thousand levels, constraints and examples enters no more
// values than one whose property holds none of them.
func TestEditor_IDCheckEntersOnlyWhatCanDeclare(t *testing.T) {
	t.Parallel()
	scanned := func(property ir.Property) int {
		editor := ir.NewEditor(&ir.Document{Types: ir.TypeRegistry{"type/m": &ir.Model{ID: "type/m", Properties: []ir.Property{property}}}})
		editor.UpdateType("type/m", func(ir.TypeDef) {})
		_, diags := editor.Finish()
		require.Empty(t, diags)
		return ir.IDsScanned(editor)
	}

	bare := scanned(ir.Property{ID: "property/a"})
	full := scanned(ir.Property{ID: "property/a", Default: nestedValue(1000), Constraints: &ir.Constraints{Pattern: "^a$"},
		Examples: []ir.Example{{Value: nestedValue(10)}}})

	assert.Positive(t, bare)
	assert.Equal(t, bare, full, "the check entered what can declare no ID")
}

// TestEditor_IDCheckSortsOnlyWhatChangedOrder holds the ID check to sorting
// the IDs a node declares only when a change reorders them: a change leaving
// them be costs no sort, and one reordering them is checked by sorting.
func TestEditor_IDCheckSortsOnlyWhatChangedOrder(t *testing.T) {
	t.Parallel()
	editor := ir.NewEditor(idsDoc())
	editor.UpdateType("type/m", func(td ir.TypeDef) { td.Common().Docs.Summary = "kept in order" })
	untouched := ir.IDListsSorted(editor)
	editor.UpdateType("type/m", func(td ir.TypeDef) { slices.Reverse(model(t, td).Properties) })
	_, diags := editor.Finish()

	require.Empty(t, diags)
	assert.Zero(t, untouched, "a change keeping the order was sorted")
	assert.Positive(t, ir.IDListsSorted(editor), "a reordering change was not checked by sorting")
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
	for _, typeKind := range allKinds {
		td, ok := ir.NewTypeDef(typeKind)
		require.True(t, ok)
		roots = append(roots, reflect.TypeOf(td))
	}
	for _, root := range roots {
		walk(root)
	}
	assert.True(t, canHold[reflect.TypeFor[ir.Parameter]()], "the check must reach past a node's own ID")
}
