package ir_test

import (
	"encoding/json"
	"testing"

	"github.com/google/go-cmp/cmp"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dexpace/morphic/ir"
)

// sampleDocument builds one document that touches every TypeDef kind.
func sampleDocument(t *testing.T) ir.Document {
	t.Helper()
	mk := func(id string, td ir.TypeDef) (ir.TypeID, ir.TypeDef) {
		typeID := ir.TypeID(id)
		td.Common().ID = typeID
		return typeID, td
	}
	types := ir.TypeRegistry{}
	for _, entry := range []ir.TypeDef{
		&ir.Primitive{Prim: "string"},
		&ir.Scalar{Base: &ir.TypeRef{Target: "t/p/string"}},
		&ir.Model{Additional: "closed"},
		&ir.Union{Exclusive: true},
		&ir.Enum{ValueType: "string", Closed: true},
		&ir.List{Elem: ir.TypeRef{Target: "t/p/string"}},
		&ir.MapT{Key: ir.TypeRef{Target: "t/p/string"}, Value: ir.TypeRef{Target: "t/p/string"}},
		&ir.Tuple{Elems: []ir.TypeRef{{Target: "t/p/string"}}},
		&ir.Literal{Value: ir.Value{Kind: ir.ValueString, Str: "fixed"}},
		&ir.External{Identity: "erlang:pid"},
		&ir.Any{},
	} {
		id, td := mk("t/k/"+string(entry.Kind()), entry)
		types[id] = td
	}
	return ir.Document{IRVersion: "0.1.0", Name: "kinds", Version: "1", Types: types}
}

func TestDocument_JSONRoundTripAllKinds(t *testing.T) {
	t.Parallel()
	doc := sampleDocument(t)

	raw, err := json.Marshal(doc)
	require.NoError(t, err)

	var back ir.Document
	require.NoError(t, json.Unmarshal(raw, &back))
	if diff := cmp.Diff(doc, back); diff != "" {
		t.Errorf("round-trip mismatch (-want +got):\n%s", diff)
	}
}

func TestOperation_AuthEmptyNonNilRoundTrips(t *testing.T) {
	t.Parallel()
	// An empty non-nil Auth ("explicitly public") must survive the JSON round
	// trip distinct from nil ("inherit the service default").
	op := ir.Operation{ID: "op/x", Auth: []ir.AuthRequirement{}}
	raw, err := json.Marshal(op)
	require.NoError(t, err)
	assert.Contains(t, string(raw), `"auth":[]`, "empty non-nil Auth serializes as []")

	var back ir.Operation
	require.NoError(t, json.Unmarshal(raw, &back))
	require.NotNil(t, back.Auth, "empty Auth must not deserialize to nil")
	assert.Empty(t, back.Auth)

	var nilOp ir.Operation
	rawNil, err := json.Marshal(nilOp)
	require.NoError(t, err)
	assert.Contains(t, string(rawNil), `"auth":null`, "nil Auth serializes as null")
	var backNil ir.Operation
	require.NoError(t, json.Unmarshal(rawNil, &backNil))
	assert.Nil(t, backNil.Auth, "nil Auth stays nil")
}

func TestDocument_MarshalIsDeterministic(t *testing.T) {
	t.Parallel()
	doc := sampleDocument(t)
	a, err := json.Marshal(doc)
	require.NoError(t, err)
	b, err := json.Marshal(doc)
	require.NoError(t, err)
	assert.Equal(t, string(a), string(b))
}

func TestTypeRegistry_KindTagIsAdjacent(t *testing.T) {
	t.Parallel()
	raw, err := json.Marshal(&ir.Model{TypeCommon: ir.TypeCommon{ID: "t/x"}})
	require.NoError(t, err)
	var probe struct {
		Kind ir.TypeKind `json:"kind"`
		ID   ir.TypeID   `json:"id"`
	}
	require.NoError(t, json.Unmarshal(raw, &probe))
	assert.Equal(t, ir.KindModel, probe.Kind)
	assert.Equal(t, ir.TypeID("t/x"), probe.ID)
}

func TestTypeRegistry_UnmarshalRejectsUnknownKind(t *testing.T) {
	t.Parallel()
	var reg ir.TypeRegistry
	err := json.Unmarshal([]byte(`{"t/x":{"kind":"bogus"}}`), &reg)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "bogus")
}

func TestTypeRegistry_UnmarshalErrors(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name    string
		data    string
		wantSub string
	}{
		{
			name:    "outer_not_a_map",
			data:    `["not", "a", "map"]`,
			wantSub: "type registry:",
		},
		{
			name:    "entry_not_an_object",
			data:    `{"t/x":123}`,
			wantSub: "reading kind tag:",
		},
		{
			name:    "unknown_kind",
			data:    `{"t/x":{"kind":"nope"}}`,
			wantSub: `unknown kind "nope"`,
		},
		{
			name:    "concrete_body_type_mismatch",
			data:    `{"t/x":{"kind":"primitive","prim":123}}`,
			wantSub: "t/x (primitive):",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			var reg ir.TypeRegistry
			err := json.Unmarshal([]byte(tt.data), &reg)
			require.Error(t, err)
			assert.Contains(t, err.Error(), tt.wantSub)
		})
	}
}
