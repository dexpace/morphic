// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package irverify_test

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dexpace/morphic/ir"
	"github.com/dexpace/morphic/ir/irverify"
)

// emptyRefViolations returns doc's ir/empty-<noun>-ref violations, filtering
// out whatever else Verify reports — naming, provenance, a dangling reference a
// minimal fixture leaves open — the way typeRefViolations does for
// checkTypeRefs, so each case below only has to build the one shape it names.
func emptyRefViolations(doc *ir.Document) []irverify.Violation {
	var out []irverify.Violation
	for _, violation := range verifyDeclared(doc) {
		if strings.HasPrefix(violation.Code, "ir/empty-") && strings.HasSuffix(violation.Code, "-ref") {
			out = append(out, violation)
		}
	}
	return out
}

// opDoc wraps op in one service and group, the shape every operation-scoped
// case below needs.
func opDoc(op ir.Operation) *ir.Document {
	return &ir.Document{Services: []ir.Service{{
		ID:     "service/x/S",
		Name:   named("s"),
		Groups: []ir.OperationGroup{{ID: "operation-group/x/S/g", Operations: []ir.Operation{op}}},
	}}}
}

// TestVerify_EmptyRequiredReferenceIsAViolation drives checkEmptyRefs across
// every shape idPositions holds to idRequired: a scalar, a non-nil pointer
// naming nothing, a slice element beside a non-empty sibling, an empty map key,
// and an empty map value at two different ID classes.
func TestVerify_EmptyRequiredReferenceIsAViolation(t *testing.T) {
	t.Parallel()
	overload := ir.OperationID("")
	for _, tc := range []struct {
		name string
		doc  *ir.Document
		code string
		path string
	}{
		{
			name: "SchemeUse.Scheme",
			doc: &ir.Document{Services: []ir.Service{{
				ID:   "service/x/S",
				Name: named("s"),
				Auth: []ir.AuthRequirement{{Schemes: []ir.SchemeUse{{Scheme: ""}}}},
			}}},
			code: "ir/empty-auth-ref",
			path: "document.Services[0].Auth[0].Schemes[0].Scheme",
		},
		{
			name: "MessageBinding.Channel",
			doc: opDoc(ir.Operation{
				ID:       "operation/x/S/op",
				Name:     named("op"),
				Bindings: ir.OpBindings{Message: &ir.MessageBinding{Channel: ""}},
			}),
			code: "ir/empty-channel-ref",
			path: "document.Services[0].Groups[0].Operations[0].Bindings.Message.Channel",
		},
		{
			name: "OTPBinding.Process",
			doc: opDoc(ir.Operation{
				ID:       "operation/x/S/op",
				Name:     named("op"),
				Bindings: ir.OpBindings{OTP: &ir.OTPBinding{Process: ""}},
			}),
			code: "ir/empty-channel-ref",
			path: "document.Services[0].Groups[0].Operations[0].Bindings.OTP.Process",
		},
		{
			name: "ValueRef.Type",
			doc: closedDoc(&ir.Model{
				ID:   "type/x/M",
				Name: named("m"),
				Properties: []ir.Property{{
					ID:      "property/x/M/f",
					Name:    named("f"),
					Type:    ir.TypeRef{Target: "type/x/Leaf"},
					Default: &ir.Value{Kind: ir.ValueRefKind, Ref: &ir.ValueRef{Type: "", Member: "X"}},
				}},
			}),
			code: "ir/empty-type-ref",
			path: "document.Types[type/x/M].Properties[0].Default.Ref.Type",
		},
		{
			name: "ConstructorValue.Scalar",
			doc: closedDoc(&ir.Model{
				ID:   "type/x/M",
				Name: named("m"),
				Properties: []ir.Property{{
					ID:      "property/x/M/f",
					Name:    named("f"),
					Type:    ir.TypeRef{Target: "type/x/Leaf"},
					Default: &ir.Value{Kind: ir.ValueConstructor, Constructor: &ir.ConstructorValue{Scalar: "", Name: "now"}},
				}},
			}),
			code: "ir/empty-type-ref",
			path: "document.Types[type/x/M].Properties[0].Default.Constructor.Scalar",
		},
		{
			name: "Operation.OverloadOf, a non-nil pointer naming nothing",
			doc: opDoc(ir.Operation{
				ID:         "operation/x/S/op",
				Name:       named("op"),
				OverloadOf: &overload,
			}),
			code: "ir/empty-operation-ref",
			path: "document.Services[0].Groups[0].Operations[0].OverloadOf",
		},
		{
			name: "Callback.Operations, one empty entry beside a non-empty sibling",
			doc: opDoc(ir.Operation{
				ID:   "operation/x/S/op",
				Name: named("op"),
				Bindings: ir.OpBindings{HTTP: []ir.HTTPBinding{{
					Method: "GET",
					Callbacks: []ir.Callback{{
						Expression: "$request.body#/url",
						Operations: []ir.OperationID{"operation/x/S/other", ""},
					}},
				}}},
			}),
			code: "ir/empty-operation-ref",
			path: "document.Services[0].Groups[0].Operations[0].Bindings.HTTP[0].Callbacks[0].Operations[1]",
		},
		{
			name: "Service.Renames, an empty key",
			doc: &ir.Document{Services: []ir.Service{{
				ID:      "service/x/S",
				Name:    named("s"),
				Renames: map[ir.TypeID]ir.Naming{"": named("x")},
			}}},
			code: "ir/empty-type-ref",
			path: "document.Services[0].Renames[].key",
		},
		{
			name: "Discriminator.Mapping, an empty value",
			doc: closedDoc(&ir.Model{
				ID:   "type/x/Base",
				Name: named("base"),
				Discriminator: &ir.Discriminator{
					Property: "property/x/Base/kind",
					Mapping:  map[string]ir.TypeID{"x": ""},
				},
			}),
			code: "ir/empty-type-ref",
			path: "document.Types[type/x/Base].Discriminator.Mapping[x]",
		},
		{
			name: "ResourceInfo.Lifecycle, an empty value",
			doc: &ir.Document{Services: []ir.Service{{
				ID:   "service/x/S",
				Name: named("s"),
				Groups: []ir.OperationGroup{{
					ID:       "operation-group/x/S/g",
					Resource: &ir.ResourceInfo{Lifecycle: map[string]ir.OperationID{"read": ""}},
				}},
			}}},
			code: "ir/empty-operation-ref",
			path: "document.Services[0].Groups[0].Resource.Lifecycle[read]",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got := emptyRefViolations(tc.doc)
			require.Len(t, got, 1)
			assert.Equal(t, tc.code, got[0].Code)
			assert.Equal(t, tc.path, got[0].Path)
		})
	}
}

// TestVerify_DiscriminatorWithNoLocatorIsAViolation is the empty union
// variant's sibling defect (GitHub #397 fixed the target; this is the tag): a
// discriminator naming no property, no wire name and no tuple index locates
// nothing, so it gets its own message rather than the generic empty-reference
// one, naming the two fields that could have located the tag instead.
func TestVerify_DiscriminatorWithNoLocatorIsAViolation(t *testing.T) {
	t.Parallel()
	doc := closedDoc(&ir.Model{
		ID:            "type/x/Base",
		Name:          named("base"),
		Discriminator: &ir.Discriminator{},
	})

	got := emptyRefViolations(doc)
	require.Len(t, got, 1)
	assert.Equal(t, "ir/empty-property-ref", got[0].Code)
	assert.Equal(t, "document.Types[type/x/Base].Discriminator.Property", got[0].Path)
	assert.Contains(t, got[0].Message, "PropertyName")
	assert.Contains(t, got[0].Message, "TupleElementIndex")
}

// TestVerify_AllowedEmptyReferencesAreClean pins the positive half at every
// documented exception, so the check cannot pass by rejecting every empty
// reference outright: Discriminator.Property empty when PropertyName or Index
// locates the tag instead (Default stays at its own zero throughout), a
// pointer left nil, and an empty or nil slice/map, which have no entry to be
// empty.
func TestVerify_AllowedEmptyReferencesAreClean(t *testing.T) {
	t.Parallel()
	idx := 0
	for _, tc := range []struct {
		name string
		doc  *ir.Document
	}{
		{
			name: "Discriminator.Property empty, PropertyName locates the tag",
			doc: closedDoc(&ir.Model{
				ID:   "type/x/ByName",
				Name: named("byname"),
				Discriminator: &ir.Discriminator{
					PropertyName: "kind",
					Mapping:      map[string]ir.TypeID{"leaf": "type/x/Leaf"},
				},
			}),
		},
		{
			name: "Discriminator.Property empty, Index locates the tag",
			doc: closedDoc(&ir.Model{
				ID:            "type/x/ByIndex",
				Name:          named("byindex"),
				Discriminator: &ir.Discriminator{TupleElementIndex: &idx},
			}),
		},
		{
			name: "nil pointers: OverloadOf, LongRunning's two operations, Reply.Channel",
			doc: opDoc(ir.Operation{
				ID:          "operation/x/S/op",
				Name:        named("op"),
				OverloadOf:  nil,
				LongRunning: &ir.LongRunning{},
				Bindings: ir.OpBindings{Message: &ir.MessageBinding{
					Channel: "channel/a",
					Reply:   &ir.Reply{},
				}},
			}),
		},
		{
			name: "empty slices: Channel.Messages, Service.Extends",
			doc: &ir.Document{
				Channels: map[ir.ChannelID]ir.Channel{
					"channel/a": {ID: "channel/a", Name: named("a"), Messages: []ir.MessageID{}},
				},
				Services: []ir.Service{{ID: "service/x/S", Name: named("s"), Extends: []ir.ServiceID{}}},
			},
		},
		{
			name: "empty maps: Service.Renames, Discriminator.Mapping, ResourceInfo.Lifecycle",
			doc: &ir.Document{
				Types: ir.TypeRegistry{"type/x/M": &ir.Model{
					ID:            "type/x/M",
					Name:          named("m"),
					Discriminator: &ir.Discriminator{PropertyName: "kind", Mapping: map[string]ir.TypeID{}},
				}},
				Services: []ir.Service{{
					ID:      "service/x/S",
					Name:    named("s"),
					Renames: map[ir.TypeID]ir.Naming{},
					Groups: []ir.OperationGroup{{
						ID:       "operation-group/x/S/g",
						Resource: &ir.ResourceInfo{Lifecycle: map[string]ir.OperationID{}},
					}},
				}},
			},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			assert.Empty(t, emptyRefViolations(tc.doc))
		})
	}
}

// TestVerify_EmptyIDsAnotherCheckOwnsAreNotReportedTwice pins idElsewhere: an
// entity's own ID, a registry key and TypeRef.Target are each held by the
// check that owns them, so an empty one draws that check's code and no
// ir/empty-<noun>-ref beside it. Each case asserts the owner's code too, so a
// fixture that stopped reaching the empty position could not pass here by
// reporting nothing at all.
//
// Every class that declares its own ID has a case, because the classification
// is per field: reading one class right says nothing about the next, and the
// other cases here would not notice a wrong entry for it.
func TestVerify_EmptyIDsAnotherCheckOwnsAreNotReportedTwice(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name  string
		doc   *ir.Document
		owner string
	}{
		{
			name: "TypeRef.Target, held by checkTypeRefs",
			doc: closedDoc(&ir.Model{
				ID:         "type/x/M",
				Name:       named("m"),
				Properties: []ir.Property{{ID: "property/x/M/f", Name: named("f"), Type: ir.TypeRef{}}},
			}),
			owner: "ir/type-ref-no-target",
		},
		{
			name:  "Operation.ID, held by checkDeclaredIDs",
			doc:   opDoc(ir.Operation{Name: named("op")}),
			owner: "ir/empty-operation-id",
		},
		{
			name: "OperationGroup.ID, held by checkDeclaredIDs",
			doc: &ir.Document{Services: []ir.Service{{
				ID: "service/x/S", Name: named("s"), Groups: []ir.OperationGroup{{Name: named("g")}},
			}}},
			owner: "ir/empty-group-id",
		},
		{
			name:  "Service.ID, held by checkDeclaredIDs",
			doc:   &ir.Document{Services: []ir.Service{{Name: named("s")}}},
			owner: "ir/empty-service-id",
		},
		{
			name: "Property.ID, held by checkDeclaredIDs",
			doc: closedDoc(&ir.Model{
				ID:         "type/x/M",
				Name:       named("m"),
				Properties: []ir.Property{{Name: named("f"), Type: ir.TypeRef{Target: "type/x/M"}}},
			}),
			owner: "ir/empty-property-id",
		},
		{
			name:  "a Document.Types key, held by checkRegistryKeys",
			doc:   &ir.Document{Types: ir.TypeRegistry{"": &ir.Model{Name: named("m")}}},
			owner: "ir/empty-type-id",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			vs := verifyDeclared(tc.doc)
			codes := make([]string, 0, len(vs))
			for _, violation := range vs {
				codes = append(codes, violation.Code)
			}
			require.Contains(t, codes, tc.owner, "the owning check must report the empty position")
			assert.Empty(t, emptyRefViolations(tc.doc), "and it must be the only one that does")
		})
	}
}

// TestVerify_ParamIDClassIsReached drives checkEmptyRefs across every position
// that holds a ParameterID: both references that must name a parameter, and the
// idempotency token, which may be empty when the operation is not token-keyed.
// The two halves are asserted together so a position that stopped being reached
// cannot pass by reporting nothing.
func TestVerify_ParamIDClassIsReached(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name string
		op   ir.Operation
		want []string
	}{
		{
			name: "an empty binding reference",
			op: ir.Operation{Bindings: ir.OpBindings{HTTP: []ir.HTTPBinding{{
				ParamBindings: []ir.HTTPParamBinding{{Param: ""}},
			}}}},
			want: []string{"document.Services[0].Groups[0].Operations[0].Bindings.HTTP[0].ParamBindings[0].Param"},
		},
		{
			name: "an empty pagination input",
			op:   ir.Operation{Pagination: &ir.Pagination{InputCursor: &ir.InputPath{Param: new(ir.ParameterID(""))}}},
			want: []string{"document.Services[0].Groups[0].Operations[0].Pagination.InputCursor.Param"},
		},
		{
			name: "an empty token with no token kind",
			op:   ir.Operation{Idempotency: ir.Idempotency{Kind: ir.IdempotencySafe}},
			want: []string{},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			tc.op.ID = "operation/x/S/op"
			paths := make([]string, 0, 1)
			for _, violation := range emptyRefViolations(opDoc(tc.op)) {
				assert.Equal(t, "ir/empty-parameter-ref", violation.Code)
				paths = append(paths, violation.Path)
			}
			assert.Equal(t, tc.want, paths)
		})
	}
}

// TestVerify_DocumentedEmptyPositionsStayLegal pins the positions whose
// comments spell "none" as an empty ID: a discriminator's default, an
// idempotency token parameter an operation not keyed by a token leaves empty,
// and a discriminator's tag property when PropertyName locates the tag. No
// empty-reference report may be drawn at any of them, whatever else the
// fixture draws.
func TestVerify_DocumentedEmptyPositionsStayLegal(t *testing.T) {
	t.Parallel()
	doc := scenarioBase()
	doc.Types["type/x/Base"] = &ir.Model{
		ID: "type/x/Base", Name: named("base"),
		Discriminator: &ir.Discriminator{PropertyName: "k", Property: "", Default: "", Mapping: map[string]ir.TypeID{"a": "type/x/M"}},
	}
	scenarioOperation(doc).Idempotency = ir.Idempotency{Kind: ir.IdempotencySafe, TokenParam: ""}

	assert.Empty(t, emptyRefViolations(doc))
}

// TestVerify_EmptyChannelOfAMessageBindingIsReportedOnce pins the empty channel
// of a message binding as one ir/empty-channel-ref at the channel, and the same
// binding naming a declared channel as clean.
func TestVerify_EmptyChannelOfAMessageBindingIsReportedOnce(t *testing.T) {
	t.Parallel()
	doc := scenarioBase()
	scenarioOperation(doc).Bindings.Message = &ir.MessageBinding{Channel: "", Direction: ir.MessageDirectionSend}

	got := emptyRefViolations(doc)

	require.Len(t, got, 1)
	assert.Equal(t, "ir/empty-channel-ref", got[0].Code)
	assert.Equal(t, scenarioOp+".Bindings.Message.Channel", got[0].Path)

	valid := scenarioBase()
	valid.Channels = map[ir.ChannelID]ir.Channel{"channel/x/c": {ID: "channel/x/c", Name: named("c")}}
	valid.IDSpaces["channel"] = []string{"x"}
	scenarioOperation(valid).Bindings.Message = &ir.MessageBinding{Channel: "channel/x/c", Direction: ir.MessageDirectionSend}
	assert.Empty(t, emptyRefViolations(valid))
}
