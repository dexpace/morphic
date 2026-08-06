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
	assertRoundTrip(t, sampleDocument(t))
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
	assertDeterministicMarshal(t, sampleDocument(t))
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

// TestTypeRegistry_UnmarshalNullIsNoOp locks in the json.Unmarshaler
// convention that encoding/json calls UnmarshalJSON even for a literal null,
// and the implementation must treat that as a no-op (the same convention
// time.Time.UnmarshalJSON follows in the standard library): a null decode
// must leave an already-populated registry untouched, not clear it, and a
// zero-value registry must decode to nil rather than a non-nil empty map
// (issue #46).
func TestTypeRegistry_UnmarshalNullIsNoOp(t *testing.T) {
	t.Parallel()

	t.Run("zero_value_stays_nil", func(t *testing.T) {
		t.Parallel()
		var reg ir.TypeRegistry
		require.NoError(t, json.Unmarshal([]byte(`null`), &reg))
		assert.Nil(t, reg)
	})

	t.Run("populated_registry_is_untouched", func(t *testing.T) {
		t.Parallel()
		reg := ir.TypeRegistry{"t/x": &ir.Any{}}
		require.NoError(t, json.Unmarshal([]byte(`null`), &reg))
		assert.Len(t, reg, 1, "a null decode must not clear an existing registry")
	})
}

// TestDocument_TypesNullRoundTrips reproduces issue #46: a document whose
// "types" key is spelled as JSON null, rather than omitted, must decode to
// the same nil TypeRegistry that omitting the key produces — the fixed
// point invariant 7 promises for golden diffing and the planned caching
// layer. Before the fix, the first decode left Types as a non-nil empty map;
// since Document.Types carries omitempty, that map vanished on marshal and
// came back nil on the next decode, so the same document read differently
// depending on how many round trips it had already been through.
func TestDocument_TypesNullRoundTrips(t *testing.T) {
	t.Parallel()

	var doc ir.Document
	require.NoError(t, json.Unmarshal([]byte(`{"types":null}`), &doc))
	require.Nil(t, doc.Types, `a null "types" key must decode to nil, not an empty map`)

	raw, err := json.Marshal(doc)
	require.NoError(t, err)
	assert.NotContains(t, string(raw), `"types"`, "omitempty must drop a nil Types field")

	var doc2 ir.Document
	require.NoError(t, json.Unmarshal(raw, &doc2))
	assert.Empty(t, cmp.Diff(doc, doc2), "decoding must reach a fixed point after one round trip")
}

// TestDocument_EmptyMapFieldsCollapseUniformly draws the line issue #46 stops
// at. TypeRegistry's null guard makes "types":null behave like every sibling
// registry field already did (Channels/Messages/Auth are plain maps with no
// custom UnmarshalJSON): nil in, nil out. It does not make an explicit empty
// object durable — "types":{} still decodes to a non-nil, empty TypeRegistry
// that Document.Types' omitempty tag drops on marshal, so a second decode
// yields nil. That collapse is not particular to TypeRegistry or to its
// custom UnmarshalJSON: the stdlib's default map decoder does the same thing
// to Document.Channels, which has no custom UnmarshalJSON at all. Fixing it
// only for TypeRegistry would make TypeRegistry diverge from
// Channels/Messages/Auth instead of matching them, so it is left alone here;
// a fix for every optional map field's {} handling would be a separate,
// repo-wide change this issue does not ask for.
func TestDocument_EmptyMapFieldsCollapseUniformly(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		data string
	}{
		{name: "types", data: `{"types":{}}`},
		{name: "channels", data: `{"channels":{}}`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			var doc ir.Document
			require.NoError(t, json.Unmarshal([]byte(tt.data), &doc))

			raw, err := json.Marshal(doc)
			require.NoError(t, err)

			var doc2 ir.Document
			require.NoError(t, json.Unmarshal(raw, &doc2))
			assert.NotEmpty(t, cmp.Diff(doc, doc2),
				"empty-object collapse-on-remarshal is shared map/omitempty behavior, not a TypeRegistry-specific defect — "+
					"if this now passes, TypeRegistry has diverged from its sibling registries again")
		})
	}
}
