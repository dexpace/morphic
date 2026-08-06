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
			// Both of the next two cases guard the byte-comparison
			// implementation this one is written not to be: such a guard sits
			// before the decode, and loosening it there swallows an unrelated
			// value and reports success, leaving the registry silently unset.
			// A length test takes this one. Neither can fire against the guard
			// as written, which sits after the decode, where a non-object has
			// already produced its own error.
			name:    "outer_is_a_non_null_scalar",
			data:    `true`,
			wantSub: "type registry:",
		},
		{
			// A pre-decode guard matching "null" as a substring rather than as
			// the whole document takes this one.
			name:    "outer_is_the_string_null",
			data:    `"null"`,
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
// convention issue #46 asks for: encoding/json calls UnmarshalJSON even for a
// literal null, and the implementation treats that as a no-op instead of
// allocating an empty map, the same way time.Time.UnmarshalJSON does.
//
// The padded case calls the exported method directly with whitespace around
// the literal — still a valid JSON encoding of null, and the spelling a byte
// comparison against "null" would miss.
//
// Where the no-op convention and plain-map semantics part ways is a null
// decoded over an already-populated destination: this registry keeps its
// entries, while the stdlib nils a plain map such as Document.Channels. #46
// chose the Unmarshaler convention. The difference cannot arise for a decode
// into a fresh Document — what internal/harness's round-trip oracle does — and
// there the two agree on nil.
func TestTypeRegistry_UnmarshalNullIsNoOp(t *testing.T) {
	t.Parallel()

	t.Run("zero_value_stays_nil", func(t *testing.T) {
		t.Parallel()
		var reg ir.TypeRegistry
		require.NoError(t, json.Unmarshal([]byte(`null`), &reg))
		assert.Nil(t, reg)
	})

	t.Run("padded_null_stays_nil", func(t *testing.T) {
		t.Parallel()
		var reg ir.TypeRegistry
		require.NoError(t, reg.UnmarshalJSON([]byte(" \n null \n ")))
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
// at. The null fix makes "types":null behave like the sibling registry fields
// already did: nil in, nil out. It deliberately does not make the empty object
// durable — "types":{} still decodes to a non-nil, empty registry that
// omitempty drops on marshal, so a second decode yields nil. The table pins
// that this is generic map/omitempty behavior rather than a TypeRegistry
// defect: Channels, Messages and Auth have no custom UnmarshalJSON at all and
// collapse identically.
//
// Leaving it follows the standard #47 already recorded for the IR's omitempty
// collections, set out on TypeRegistry.UnmarshalJSON: the collapse is a defect
// only where nil and empty denote different things, as on the
// []AuthRequirement fields (Operation.Auth, Server.Auth, Service.Auth) — not
// on the Document.Auth scheme registry exercised below, where an empty
// registry and an absent one both mean a document that declares none.
// Changing it for every optional map field would be a separate, repo-wide
// change.
func TestDocument_EmptyMapFieldsCollapseUniformly(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		data string
	}{
		{name: "types", data: `{"types":{}}`},
		{name: "channels", data: `{"channels":{}}`},
		{name: "messages", data: `{"messages":{}}`},
		{name: "auth", data: `{"auth":{}}`},
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
				"%s: empty-object collapse-on-remarshal is shared map/omitempty behavior, not a TypeRegistry-specific "+
					"defect — a field that stops collapsing has diverged from the registry fields next to it", tt.name)
		})
	}
}
