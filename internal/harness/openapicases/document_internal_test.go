// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package openapicases

import (
	"bytes"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestEncode_WritesEachObjectInItsOrder(t *testing.T) {
	got, err := encode(object{{"z", "a"}, {"a", []any{raw(`1`), object{}}}, {"m", object(nil)}})
	require.NoError(t, err)
	assert.JSONEq(t, `{"z": "a", "a": [1, {}], "m": {}}`, string(got))
	assert.Less(t, bytes.Index(got, []byte(`"z"`)), bytes.Index(got, []byte(`"a"`)), "members are written in their order")
}

func TestEncode_RefusesWhatNoDocumentHolds(t *testing.T) {
	deep := object{{"x", "leaf"}}
	for range maxDepth {
		deep = object{{"x", deep}}
	}
	for _, tc := range []struct {
		name string
		v    any
		want string
	}{
		{"a value of no JSON type", object{{"n", 1}}, "no JSON for a int"},
		{"a name written twice", object{{"a", "x"}, {"a", "y"}}, "duplicate object member name"},
		{"a raw value that is no JSON", []any{raw(`{`)}, "unexpected EOF"},
		{"a document past the depth bound", deep, "deeper than 32"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, err := encode(tc.v)
			require.Error(t, err)
			assert.Contains(t, err.Error(), tc.want)
		})
	}

	_, err := encode(deep[0].value)
	require.NoError(t, err, "a document at the depth bound is written")
}

func TestObjectWith_SetsEachMemberInItsPlace(t *testing.T) {
	body := object{{"type", "array"}, {"items", "a"}}
	got := body.with(object{{"items", "b"}, {"minItems", "1"}})
	assert.Equal(t, object{{"type", "array"}, {"items", "b"}, {"minItems", "1"}}, got)
	assert.Equal(t, object{{"type", "array"}, {"items", "a"}}, body, "the body is left as it was")
}

func TestWrite_ATableTheWriterCannotEncodePanics(t *testing.T) {
	casesValue := New().(cases)
	casesValue.targets = []target{{name: "bad", body: object{{"type", 1}}}}
	assert.PanicsWithValue(t, "openapicases: case [0 0 0 0 0 0]: openapicases: no JSON for a int", func() {
		casesValue.Write([]int{0, 0, 0, 0, 0, 0})
	})
}
