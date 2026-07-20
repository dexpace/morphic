package ir_test

import (
	"encoding/json"
	"testing"

	"github.com/google/go-cmp/cmp"
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
			var back ir.Value
			require.NoError(t, json.Unmarshal(raw, &back))
			if diff := cmp.Diff(v, back); diff != "" {
				t.Errorf("round-trip mismatch (-want +got):\n%s", diff)
			}
		})
	}
}
