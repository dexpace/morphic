// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package pass_test

import (
	"slices"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dexpace/morphic/ir"
	"github.com/dexpace/morphic/pass"
)

// twoFinding is a row of twoFindings: a document drawing two findings of code
// from one family, at the nodes want lists in the order they must come out.
type twoFinding struct {
	family string
	doc    func() *ir.Document
	code   string
	want   []string
	// once, when set, says why the family can report one finding of its code a
	// document, which is then all the row holds it to.
	once string
}

// operationsOf is validDoc holding an operation of each ID, in that order,
// built by mk: the way a family that reads operations draws two findings.
func operationsOf(mk func(id ir.OperationID) ir.Operation, ids ...ir.OperationID) *ir.Document {
	ops := make([]ir.Operation, 0, len(ids))
	for _, id := range ids {
		ops = append(ops, mk(id))
	}
	return docWithOperations(ops...)
}

// typesOf is validDoc holding the type typeOf makes of each of ids.
func typesOf(typeOf func(id ir.TypeID) ir.TypeDef, ids ...ir.TypeID) *ir.Document {
	doc := validDoc()
	for _, id := range ids {
		doc.Types[id] = typeOf(id)
	}
	return doc
}

// registryRow is the row of the family registryCases names, over the types
// t/z and t/b, declared in that order: the family ranges the registry, so its
// findings come out in ID order, t/b first.
func registryRow(name string) twoFinding {
	for _, tc := range registryCases() {
		if tc.name == name {
			return twoFinding{
				family: name, code: tc.code, want: []string{tc.at("type/b"), tc.at("type/z")},
				doc: func() *ir.Document { return typesOf(tc.typeOf, "type/z", "type/b") },
			}
		}
	}
	panic("no registry case " + name)
}

// twoFindings is one row per rule family Validate runs, in the order it runs
// them. A family added without a row fails the length check below, so the new
// family is held to one order the day it exists.
var twoFindings = []twoFinding{
	{family: "checkNilTypes", code: "ir/nil-type", want: []string{"types[type/b]", "types[type/z]"},
		doc: func() *ir.Document {
			return typesOf(func(ir.TypeID) ir.TypeDef { return (*ir.Model)(nil) }, "type/z", "type/b")
		}},
	{family: "checkDanglingRefs", code: "ir/dangling-type-ref",
		want: []string{"document.Types[type/b].Properties[0].Type.Target", "document.Types[type/z].Properties[0].Type.Target"},
		doc: func() *ir.Document {
			return typesOf(func(id ir.TypeID) ir.TypeDef {
				return &ir.Model{ID: id, Properties: []ir.Property{{ID: ir.PropertyID("property/" + id), WireName: "a", Type: ir.TypeRef{Target: "type/none"}}}}
			}, "type/z", "type/b")
		}},
	{family: "checkEmptyRefs", code: "ir/empty-channel-ref",
		want: []string{scenarioOpPath + ".Bindings.Message.Channel", "document.Services[0].Groups[0].Operations[1].Bindings.Message.Channel"},
		doc: func() *ir.Document {
			return operationsOf(func(id ir.OperationID) ir.Operation {
				op := ir.Operation{ID: id}
				op.Bindings.Message = &ir.MessageBinding{Direction: ir.MessageDirectionSend}
				return op
			}, "operation/z", "operation/b")
		}},
	{family: "checkGroupWalkTruncated", code: "ir/walk-truncated", want: []string{"document"},
		once: "a stopped walk of the groups is one finding, whatever its depth past the bound",
		doc: func() *ir.Document {
			return nestGroups(ir.MaxGroupDepth+1, ir.Operation{ID: "deep"})
		}},
	{family: "checkResponseIndices", code: "ir/response-index-out-of-range",
		want: []string{"op/bindings/http/0/successStatus/1", "op/bindings/http/0/successStatus/3"},
		doc:  func() *ir.Document { return opWithSuccessStatus(map[int]int{3: 203, 1: 201}) }},
	{family: "checkEncodingKeys", code: "ir/encoding-key-unknown-property",
		want: []string{"op/request/contents/0/encoding/property/m/b", "op/request/contents/0/encoding/property/m/z"},
		doc: func() *ir.Document {
			doc := validDoc()
			requestContent(doc).Encoding = map[ir.PropertyID]ir.PartEncoding{"property/m/z": {}, "property/m/b": {}}
			return doc
		}},
	{family: "checkCatchAllEncodingKeys", code: "ir/encoding-key-unknown-catch-all",
		want: []string{"op/request/contents/0/catchAllEncoding/type/b", "op/request/contents/0/catchAllEncoding/type/z"},
		doc: func() *ir.Document {
			doc := validDoc()
			value := &ir.PartEncoding{HasFilename: true}
			// Two declared models holding no catch-all: a key naming no type is the
			// dangling check's.
			doc.Types["type/z"], doc.Types["type/b"] = &ir.Model{ID: "type/z"}, &ir.Model{ID: "type/b"}
			requestContent(doc).CatchAllEncoding = map[ir.TypeID]ir.CatchAllParts{"type/z": {Value: value}, "type/b": {Value: value}}
			return doc
		}},
	{family: "checkPayloadRequired", code: "ir/payload-required-outside-request",
		want: []string{"operation/z/responses/0", "operation/b/responses/0"},
		doc: func() *ir.Document {
			return operationsOf(func(id ir.OperationID) ir.Operation {
				payload := multipartPayload(nil)
				payload.Required = new(false)
				return ir.Operation{ID: id, Responses: []ir.Response{{Payload: &payload}}}
			}, "operation/z", "operation/b")
		}},
	{family: "checkPropIDRefs", code: "ir/dangling-property-ref",
		want: []string{
			"document.Services[0].Groups[0].Operations[0].Bindings.HTTP[0].ResponseBodyPath.Segments[0]",
			"document.Services[0].Groups[0].Operations[1].Bindings.HTTP[0].ResponseBodyPath.Segments[0]",
		},
		doc: func() *ir.Document {
			return operationsOf(func(id ir.OperationID) ir.Operation {
				op := propPathOperation([]ir.PropertyID{"property/m/ghost"})
				op.ID = id
				return op
			}, "operation/z", "operation/b")
		}},
	registryRow("checkDiscriminators"),
	registryRow("checkRepeatedVariants"),
	registryRow("checkDuplicateWireNames"),
	registryRow("checkAmbiguousAliases"),
	registryRow("checkDuplicateMemberIDs"),
	{family: "checkParams", code: "pass/param-binding-mismatch",
		want: []string{"operation/z/bindings/http/0", "operation/b/bindings/http/0"},
		doc: func() *ir.Document {
			return operationsOf(func(id ir.OperationID) ir.Operation {
				op := paramBindingOp(id, ir.Parameter{ID: ir.ParameterID("parameter/" + id), Name: ir.Naming{Source: "q"}})
				op.Bindings.HTTP[0].ParamBindings = append(op.Bindings.HTTP[0].ParamBindings, op.Bindings.HTTP[0].ParamBindings[0])
				return op
			}, "operation/z", "operation/b")
		}},
	{family: "checkIdempotency", code: "pass/idempotency-token-mismatch",
		want: []string{"operation/z/idempotency", "operation/b/idempotency"},
		doc: func() *ir.Document {
			return operationsOf(func(id ir.OperationID) ir.Operation {
				return ir.Operation{ID: id, Idempotency: ir.Idempotency{Kind: ir.IdempotencyToken}}
			}, "operation/z", "operation/b")
		}},
	{family: "checkMessageBindings", code: "pass/message-not-in-channel",
		want: []string{"operation/z/bindings/message/messages/0", "operation/b/bindings/message/messages/0"},
		doc: func() *ir.Document {
			doc := messageBindingDoc("channel/a", []ir.MessageID{"message/a"}, nil)
			doc.Services[0].Groups[0].Operations = nil
			for _, id := range []ir.OperationID{"operation/z", "operation/b"} {
				op := ir.Operation{ID: id}
				op.Bindings.Message = &ir.MessageBinding{Channel: "channel/a", Messages: []ir.MessageID{"message/b"}}
				doc.Services[0].Groups[0].Operations = append(doc.Services[0].Groups[0].Operations, op)
			}
			return doc
		}},
	{family: "checkOneWay", code: "pass/oneway-with-responses",
		want: []string{"operation/z", "operation/b"},
		doc: func() *ir.Document {
			return operationsOf(func(id ir.OperationID) ir.Operation {
				return ir.Operation{ID: id, OneWay: true, Responses: []ir.Response{{}}}
			}, "operation/z", "operation/b")
		}},
	{family: "checkDefaultResponses", code: "ir/default-response-conditions",
		want: []string{"operation/z/default", "operation/b/default"},
		doc: func() *ir.Document {
			return operationsOf(func(id ir.OperationID) ir.Operation {
				return ir.Operation{ID: id, Default: &ir.Response{}}
			}, "operation/z", "operation/b")
		}},
	registryRow("checkArgsOutsideGraphQL"),
	// The families that read what a source says, in the order Validate runs
	// them after the rest.
	{family: "checkBounds", code: boundsCode, want: []string{"type/b", "type/z"},
		doc: func() *ir.Document {
			return typesOf(func(id ir.TypeID) ir.TypeDef {
				return &ir.List{ID: id, ElementType: strT, Constraints: &ir.Constraints{MinItems: new(int64(2)), MaxItems: new(int64(1))}}
			}, "type/z", "type/b")
		}},
	{family: "checkValues", code: exampleCode, want: []string{"type/b", "type/z"},
		doc: func() *ir.Document {
			return typesOf(func(id ir.TypeID) ir.TypeDef {
				return &ir.List{ID: id, ElementType: strT, Examples: []ir.Example{{Value: text("not a list")}}}
			}, "type/z", "type/b")
		}},
	{family: "checkCompositionCyclesAndInfiniteTypes", code: cycleCode, want: []string{"type/b", "type/z"},
		doc: func() *ir.Document {
			return typesOf(func(id ir.TypeID) ir.TypeDef { return composed(string(id), new(ref(string(id)))) }, "type/z", "type/b")
		}},
}

// TestValidate_EveryFamilyDrawsTwoFindingsInOneOrder holds each rule family to
// the order of its findings: a document drawing two of one code, at nodes
// declared in the reverse of their reported order, reports them as the row
// lists, and validated reads it determinismRuns times for one output. A range
// over a map in place of sorted keys fails here. A family without a row fails
// the length check; what the table cannot see is a range added inside a family
// whose tests never draw two findings from two keys of the ranged map.
func TestValidate_EveryFamilyDrawsTwoFindingsInOneOrder(t *testing.T) {
	t.Parallel()
	require.Len(t, twoFindings, pass.Families(), "each family takes a row, in the order Validate runs them")

	for i, row := range twoFindings {
		t.Run(row.family, func(t *testing.T) {
			t.Parallel()
			doc := row.doc()

			got := withCode(pass.FamilyFindings(t, doc, i), row.code)

			assert.Equal(t, row.want, pointers(got), "the findings, in the order they are reported")
			if row.once != "" {
				require.Len(t, got, 1, row.once)
			} else {
				require.GreaterOrEqual(t, len(got), 2, "a row holds a family to two findings")
				assert.Len(t, slices.Compact(pointers(got)), len(got), "no two findings at one node")
			}
			validated(t, doc)
		})
	}
}

// failures is a fataler recording what it is told, which stops the run as
// testing.T does.
type failures struct{ told []string }

func (f *failures) Helper() {}

func (f *failures) Fatalf(format string, _ ...any) {
	f.told = append(f.told, format)
	panic(f)
}

// TestReadRepeatedly_ReadsATwoFindingDocumentUntilItHasSaidSoEveryTime pins the
// helper every test reads Validate through: a read drawing one finding or none
// is made once, one drawing two is made determinismRuns times, and a reading
// that differs from the first fails the test.
func TestReadRepeatedly_ReadsATwoFindingDocumentUntilItHasSaidSoEveryTime(t *testing.T) {
	t.Parallel()
	finding := func(node string) ir.Diagnostic {
		return ir.NewDiagnostic(ir.SeverityWarning, "pass/x", "m", ir.Provenance{Node: node})
	}
	for _, tc := range []struct {
		name  string
		diags []ir.Diagnostic
		reads int
	}{
		{"no finding", nil, 1},
		{"one finding", []ir.Diagnostic{finding("a")}, 1},
		{"two findings", []ir.Diagnostic{finding("a"), finding("b")}, determinismRuns},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			reads := 0

			got := readRepeatedly(&failures{}, func() []ir.Diagnostic { reads++; return tc.diags })

			assert.Equal(t, tc.reads, reads)
			assert.Equal(t, tc.diags, got)
		})
	}

	t.Run("a reading that differs", func(t *testing.T) {
		t.Parallel()
		reads, f := 0, &failures{}
		flip := func() []ir.Diagnostic {
			reads++
			if reads == determinismRuns {
				return []ir.Diagnostic{finding("b"), finding("a")}
			}
			return []ir.Diagnostic{finding("a"), finding("b")}
		}

		assert.PanicsWithValue(t, f, func() { readRepeatedly(f, flip) })
		assert.Equal(t, determinismRuns, reads, "the last reading is the one that differs")
		assert.Len(t, f.told, 1)
	})
}
