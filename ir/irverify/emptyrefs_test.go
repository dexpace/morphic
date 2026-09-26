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
	for _, v := range irverify.Verify(doc) {
		if strings.HasPrefix(v.Code, "ir/empty-") && strings.HasSuffix(v.Code, "-ref") {
			out = append(out, v)
		}
	}
	return out
}

// opDoc wraps op in one service and group, the shape every operation-scoped
// case below needs.
func opDoc(op ir.Operation) *ir.Document {
	return &ir.Document{Services: []ir.Service{{
		ID:     "s/x/S",
		Name:   named("s"),
		Groups: []ir.OperationGroup{{Operations: []ir.Operation{op}}},
	}}}
}

// TestVerify_EmptyRequiredReferenceIsAViolation drives checkEmptyRefs across
// every shape idPositions holds to idRequired: a scalar, a non-nil pointer
// naming nothing, a slice element beside a non-empty sibling, an empty map key,
// and an empty map value at two different ID classes.
func TestVerify_EmptyRequiredReferenceIsAViolation(t *testing.T) {
	t.Parallel()
	overload := ir.OpID("")
	for _, tc := range []struct {
		name string
		doc  *ir.Document
		code string
		path string
	}{
		{
			name: "SchemeUse.Scheme",
			doc: &ir.Document{Services: []ir.Service{{
				ID:   "s/x/S",
				Name: named("s"),
				Auth: []ir.AuthRequirement{{Schemes: []ir.SchemeUse{{Scheme: ""}}}},
			}}},
			code: "ir/empty-auth-ref",
			path: "doc.Services[0].Auth[0].Schemes[0].Scheme",
		},
		{
			name: "MessageBinding.Channel",
			doc: opDoc(ir.Operation{
				ID:       "op/x/S/op",
				Name:     named("op"),
				Bindings: ir.OpBindings{Message: &ir.MessageBinding{Channel: ""}},
			}),
			code: "ir/empty-channel-ref",
			path: "doc.Services[0].Groups[0].Operations[0].Bindings.Message.Channel",
		},
		{
			name: "OTPBinding.Process",
			doc: opDoc(ir.Operation{
				ID:       "op/x/S/op",
				Name:     named("op"),
				Bindings: ir.OpBindings{OTP: &ir.OTPBinding{Process: ""}},
			}),
			code: "ir/empty-channel-ref",
			path: "doc.Services[0].Groups[0].Operations[0].Bindings.OTP.Process",
		},
		{
			name: "ValueRef.Type",
			doc: closedDoc(&ir.Model{
				ID:   "t/x/M",
				Name: named("m"),
				Properties: []ir.Property{{
					ID:      "p/x/M/f",
					Name:    named("f"),
					Type:    ir.TypeRef{Target: "t/x/Leaf"},
					Default: &ir.Value{Kind: ir.ValueRefKind, Ref: &ir.ValueRef{Type: "", Member: "X"}},
				}},
			}),
			code: "ir/empty-type-ref",
			path: "doc.Types[t/x/M].Properties[0].Default.Ref.Type",
		},
		{
			name: "CtorValue.Scalar",
			doc: closedDoc(&ir.Model{
				ID:   "t/x/M",
				Name: named("m"),
				Properties: []ir.Property{{
					ID:      "p/x/M/f",
					Name:    named("f"),
					Type:    ir.TypeRef{Target: "t/x/Leaf"},
					Default: &ir.Value{Kind: ir.ValueCtor, Ctor: &ir.CtorValue{Scalar: "", Name: "now"}},
				}},
			}),
			code: "ir/empty-type-ref",
			path: "doc.Types[t/x/M].Properties[0].Default.Ctor.Scalar",
		},
		{
			name: "Operation.OverloadOf, a non-nil pointer naming nothing",
			doc: opDoc(ir.Operation{
				ID:         "op/x/S/op",
				Name:       named("op"),
				OverloadOf: &overload,
			}),
			code: "ir/empty-op-ref",
			path: "doc.Services[0].Groups[0].Operations[0].OverloadOf",
		},
		{
			name: "Callback.Operations, one empty entry beside a non-empty sibling",
			doc: opDoc(ir.Operation{
				ID:   "op/x/S/op",
				Name: named("op"),
				Bindings: ir.OpBindings{HTTP: []ir.HTTPBinding{{
					Method: "GET",
					Callbacks: []ir.Callback{{
						Expression: "$request.body#/url",
						Operations: []ir.OpID{"op/x/S/other", ""},
					}},
				}}},
			}),
			code: "ir/empty-op-ref",
			path: "doc.Services[0].Groups[0].Operations[0].Bindings.HTTP[0].Callbacks[0].Operations[1]",
		},
		{
			name: "Service.Renames, an empty key",
			doc: &ir.Document{Services: []ir.Service{{
				ID:      "s/x/S",
				Name:    named("s"),
				Renames: map[ir.TypeID]ir.Naming{"": named("x")},
			}}},
			code: "ir/empty-type-ref",
			path: "doc.Services[0].Renames[].key",
		},
		{
			name: "Discriminator.Mapping, an empty value",
			doc: closedDoc(&ir.Model{
				ID:   "t/x/Base",
				Name: named("base"),
				Discriminator: &ir.Discriminator{
					Property: "p/x/Base/kind",
					Mapping:  map[string]ir.TypeID{"x": ""},
				},
			}),
			code: "ir/empty-type-ref",
			path: "doc.Types[t/x/Base].Discriminator.Mapping[x]",
		},
		{
			name: "ResourceInfo.Lifecycle, an empty value",
			doc: &ir.Document{Services: []ir.Service{{
				ID:   "s/x/S",
				Name: named("s"),
				Groups: []ir.OperationGroup{{
					Resource: &ir.ResourceInfo{Lifecycle: map[string]ir.OpID{"read": ""}},
				}},
			}}},
			code: "ir/empty-op-ref",
			path: "doc.Services[0].Groups[0].Resource.Lifecycle[read]",
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
		ID:            "t/x/Base",
		Name:          named("base"),
		Discriminator: &ir.Discriminator{},
	})

	got := emptyRefViolations(doc)
	require.Len(t, got, 1)
	assert.Equal(t, "ir/empty-prop-ref", got[0].Code)
	assert.Equal(t, "doc.Types[t/x/Base].Discriminator.Property", got[0].Path)
	assert.Contains(t, got[0].Message, "PropertyName")
	assert.Contains(t, got[0].Message, "Index")
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
				ID:   "t/x/ByName",
				Name: named("byname"),
				Discriminator: &ir.Discriminator{
					PropertyName: "kind",
					Mapping:      map[string]ir.TypeID{"leaf": "t/x/Leaf"},
				},
			}),
		},
		{
			name: "Discriminator.Property empty, Index locates the tag",
			doc: closedDoc(&ir.Model{
				ID:            "t/x/ByIndex",
				Name:          named("byindex"),
				Discriminator: &ir.Discriminator{Index: &idx},
			}),
		},
		{
			name: "nil pointers: OverloadOf, LongRunning's two operations, Reply.Channel",
			doc: opDoc(ir.Operation{
				ID:          "op/x/S/op",
				Name:        named("op"),
				OverloadOf:  nil,
				LongRunning: &ir.LongRunning{},
				Bindings: ir.OpBindings{Message: &ir.MessageBinding{
					Channel: "chan/a",
					Reply:   &ir.Reply{},
				}},
			}),
		},
		{
			name: "empty slices: Channel.Messages, Service.Extends",
			doc: &ir.Document{
				Channels: map[ir.ChannelID]ir.Channel{
					"chan/a": {ID: "chan/a", Name: named("a"), Messages: []ir.MessageID{}},
				},
				Services: []ir.Service{{ID: "s/x/S", Name: named("s"), Extends: []ir.ServiceID{}}},
			},
		},
		{
			name: "empty maps: Service.Renames, Discriminator.Mapping, ResourceInfo.Lifecycle",
			doc: &ir.Document{
				Types: ir.TypeRegistry{"t/x/M": &ir.Model{
					ID:            "t/x/M",
					Name:          named("m"),
					Discriminator: &ir.Discriminator{PropertyName: "kind", Mapping: map[string]ir.TypeID{}},
				}},
				Services: []ir.Service{{
					ID:      "s/x/S",
					Name:    named("s"),
					Renames: map[ir.TypeID]ir.Naming{},
					Groups: []ir.OperationGroup{{
						Resource: &ir.ResourceInfo{Lifecycle: map[string]ir.OpID{}},
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
