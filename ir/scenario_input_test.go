// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package ir_test

import (
	"encoding/json/v2"
	"slices"
	"testing"

	"github.com/google/go-cmp/cmp"
	"github.com/google/go-cmp/cmp/cmpopts"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dexpace/morphic/ir"
)

// scenarioStr is the string value a scenario's parameters and body hold.
var scenarioStr = ir.Value{Kind: ir.ValueString, Str: "v"}

// TestExample_ScenarioInput_Encoding pins how an example writes its input: a
// populated one under "input" with its parameters keyed by ID and its body
// beside them, an empty one as an empty object, and a nil one not at all.
func TestExample_ScenarioInput_Encoding(t *testing.T) {
	t.Parallel()
	body := scenarioStr
	for _, tc := range []struct {
		name string
		ex   ir.Example
		want string
	}{
		{
			name: "params and body",
			ex: ir.Example{Name: "ex", Input: &ir.ScenarioInput{
				Params: map[ir.ParamID]ir.Value{"param/x/o/q": scenarioStr},
				Body:   &body,
			}},
			want: `{"name":"ex","input":{"params":{"param/x/o/q":{"kind":"string","str":"v"}},` +
				`"body":{"kind":"string","str":"v"}}}`,
		},
		{
			name: "an empty scenario sends nothing but is not absent",
			ex:   ir.Example{Name: "ex", Input: &ir.ScenarioInput{}},
			want: `{"name":"ex","input":{}}`,
		},
		{
			name: "a nil input is no member",
			ex:   ir.Example{Name: "ex"},
			want: `{"name":"ex"}`,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			raw, err := json.Marshal(tc.ex, json.Deterministic(true))
			require.NoError(t, err)
			assert.JSONEq(t, tc.want, string(raw))
			assert.Equal(t, tc.want, string(raw), "the encoding's member order is part of the contract")
		})
	}
}

// TestExample_ScenarioInput_RoundTrips holds each distinction an input draws to
// the codec: an absent input and an empty one, a nil Body and one holding null,
// a nil parameter map and an empty one, and a key that names no valid ParamID,
// which the codec carries and irverify reports. It runs through an Example
// alone and through a whole Document, each compared with cmp.Diff.
func TestExample_ScenarioInput_RoundTrips(t *testing.T) {
	t.Parallel()
	null := ir.Value{Kind: ir.ValueNull}
	body := scenarioStr
	for _, tc := range []struct {
		name  string
		input *ir.ScenarioInput
	}{
		{name: "a nil input", input: nil},
		{name: "an empty input", input: &ir.ScenarioInput{}},
		{name: "a nil parameter map", input: &ir.ScenarioInput{Body: &body}},
		{name: "a nil body", input: &ir.ScenarioInput{Params: map[ir.ParamID]ir.Value{"param/x/o/q": scenarioStr}}},
		{name: "a body holding a null value", input: &ir.ScenarioInput{Body: &null}},
		{name: "a key that is no valid ParamID", input: &ir.ScenarioInput{
			Params: map[ir.ParamID]ir.Value{"": scenarioStr, "not an id": null, "t/x/M": scenarioStr},
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			ex := ir.Example{Name: "ex", Input: tc.input}
			assertRoundTrip(t, ex)

			doc := ir.Document{
				IRVersion: ir.IRVersion,
				Types: ir.TypeRegistry{"t/x/S": &ir.Scalar{
					ID: "t/x/S", Name: ir.Naming{Source: "s", Canonical: "s"}, Examples: []ir.Example{ex},
				}},
			}
			raw, err := json.Marshal(&doc, json.Deterministic(true))
			require.NoError(t, err)
			var got ir.Document
			require.NoError(t, json.Unmarshal(raw, &got))
			assert.Empty(t, cmp.Diff(doc, got), "a document must hand its example back as written")
		})
	}
}

// TestExample_ScenarioInput_EmptyParamMapIsTheNilOne pins the one distinction
// the codec does not draw: Params carries omitempty, so an empty map is written
// as no member and read back nil, and a Body beside it is untouched.
func TestExample_ScenarioInput_EmptyParamMapIsTheNilOne(t *testing.T) {
	t.Parallel()
	body := scenarioStr
	in := ir.Example{Name: "ex", Input: &ir.ScenarioInput{Params: map[ir.ParamID]ir.Value{}, Body: &body}}
	raw, err := json.Marshal(in, json.Deterministic(true))
	require.NoError(t, err)
	assert.NotContains(t, string(raw), `"params"`)

	var got ir.Example
	require.NoError(t, json.Unmarshal(raw, &got))
	require.NotNil(t, got.Input, "the input itself stays present")
	assert.Nil(t, got.Input.Params)
	assert.Empty(t, cmp.Diff(in, got, cmpopts.EquateEmpty()))
}

// TestExample_ScenarioInput_NullBodyIsNotAbsent pins that "body":{"kind":"null"}
// reads back as a non-nil Body holding null, the request content being JSON
// null, and is not folded into the absent one.
func TestExample_ScenarioInput_NullBodyIsNotAbsent(t *testing.T) {
	t.Parallel()
	var got ir.Example
	require.NoError(t, json.Unmarshal([]byte(`{"input":{"body":{"kind":"null"}}}`), &got))
	require.NotNil(t, got.Input)
	require.NotNil(t, got.Input.Body)
	assert.Equal(t, ir.ValueNull, got.Input.Body.Kind)

	var none ir.Example
	require.NoError(t, json.Unmarshal([]byte(`{"input":{}}`), &none))
	require.NotNil(t, none.Input)
	assert.Nil(t, none.Input.Body)
}

// TestExample_ScenarioInput_TypeEdgesReadItInTheWalksOrder pins the order
// TypeEdges yields the types an input's values name: each parameter's value in
// sorted key order, then the body, which is the order the reflection walk meets
// them. The keys are declared out of order, so a map range shows.
func TestExample_ScenarioInput_TypeEdgesReadItInTheWalksOrder(t *testing.T) {
	t.Parallel()
	ref := func(id ir.TypeID) ir.Value {
		return ir.Value{Kind: ir.ValueRefKind, Ref: &ir.ValueRef{Type: id, Member: "m"}}
	}
	body := ir.Value{Kind: ir.ValueCtor, Ctor: &ir.CtorValue{Scalar: "t/x/C", Name: "now"}}
	m := &ir.Model{ID: "t/x/M", Name: ir.Naming{Source: "m", Canonical: "m"}, Examples: []ir.Example{{
		Name: "ex",
		Input: &ir.ScenarioInput{
			Params: map[ir.ParamID]ir.Value{"param/b": ref("t/x/B"), "param/a": ref("t/x/A")},
			Body:   &body,
		},
	}}}

	got := slices.Collect(ir.TypeEdges(m))

	assert.Equal(t, []ir.TypeID{"t/x/A", "t/x/B", "t/x/C"}, got)
	assert.Empty(t, edgeDiff(m), "TypeEdges must agree with the reflection walk")
}
