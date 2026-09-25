package ir_test

import (
	"encoding/json/v2"
	"testing"

	"github.com/google/go-cmp/cmp"
	"github.com/google/go-cmp/cmp/cmpopts"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dexpace/morphic/ir"
)

func TestValue_JSONRoundTrip(t *testing.T) {
	t.Parallel()
	cases := map[string]ir.Value{
		"null":   {Kind: ir.ValueNull},
		"bool":   {Kind: ir.ValueBool, Bool: true},
		"string": {Kind: ir.ValueString, Str: "hello"},
		"symbol": {Kind: ir.ValueSymbol, Str: "ok"},
		"number": {Kind: ir.ValueNumber, Num: ir.BigVal("3.14")},
		"bytes":  {Kind: ir.ValueBytes, Bytes: []byte{0x01, 0x02}},
		"list": {Kind: ir.ValueList, List: []ir.Value{
			{Kind: ir.ValueNumber, Num: ir.BigVal("1")},
			{Kind: ir.ValueNumber, Num: ir.BigVal("2")},
		}},
		"empty list":   {Kind: ir.ValueList, List: []ir.Value{}},
		"empty object": {Kind: ir.ValueObject, Object: []ir.Field{}},
		"empty bytes":  {Kind: ir.ValueBytes, Bytes: []byte{}},
		"object": {Kind: ir.ValueObject, Object: []ir.Field{
			{Name: "b", Value: ir.Value{Kind: ir.ValueBool, Bool: false}},
			{Name: "a", Value: ir.Value{Kind: ir.ValueString, Str: "x"}},
		}},
		"ref": {Kind: ir.ValueRefKind, Ref: &ir.ValueRef{Type: ir.TypeID("t/x"), Member: "M"}},
		"ctor": {Kind: ir.ValueCtor, Ctor: &ir.CtorValue{
			Scalar: ir.TypeID("t/s"),
			Name:   "fromISO",
			Args:   []ir.Value{{Kind: ir.ValueString, Str: "2024-05-06"}},
		}},
	}
	for name, v := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			raw, err := json.Marshal(v)
			require.NoError(t, err)
			var got ir.Value
			require.NoError(t, json.Unmarshal(raw, &got))
			// Bytes/List/Object carry omitempty (the Value doc: an empty payload and
			// a nil one are the same value), so the "empty …" cases above marshal
			// indistinguishably from nil and decode back nil; EquateEmpty makes
			// that the pass condition instead of exact struct reproduction, which
			// would fail an empty-but-non-nil payload against the nil it decoded to.
			assert.Empty(t, cmp.Diff(v, got, cmpopts.EquateEmpty()), "JSON round-trip must reproduce the original value")
		})
	}
}

// TestValue_EmptyPayloadHasOneSpelling pins the Value doc comment on the wire:
// Kind already says which payload a value carries, so a nil payload and an
// empty one are the same value and write the same bytes. A round-trip compare
// cannot see this, since both decode to a nil payload either way.
func TestValue_EmptyPayloadHasOneSpelling(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name   string
		empty  ir.Value
		absent ir.Value
		want   string
	}{
		{"list", ir.Value{Kind: ir.ValueList, List: []ir.Value{}}, ir.Value{Kind: ir.ValueList}, `{"kind":"list"}`},
		{"object", ir.Value{Kind: ir.ValueObject, Object: []ir.Field{}}, ir.Value{Kind: ir.ValueObject}, `{"kind":"object"}`},
		{"bytes", ir.Value{Kind: ir.ValueBytes, Bytes: []byte{}}, ir.Value{Kind: ir.ValueBytes}, `{"kind":"bytes"}`},
		{
			"ctor args",
			ir.Value{Kind: ir.ValueCtor, Ctor: &ir.CtorValue{Args: []ir.Value{}}},
			ir.Value{Kind: ir.ValueCtor, Ctor: &ir.CtorValue{}},
			`{"kind":"ctor","ctor":{}}`,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			for _, v := range []ir.Value{tt.empty, tt.absent} {
				raw, err := json.Marshal(v)
				require.NoError(t, err)
				assert.Equal(t, tt.want, string(raw))
			}
		})
	}
}
