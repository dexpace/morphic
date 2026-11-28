// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package options_test

import (
	"encoding/json/jsontext"
	"encoding/json/v2"
	"errors"
	"strconv"
	"testing"

	"github.com/google/go-cmp/cmp"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dexpace/morphic/emitters/options"
)

// nestedLayer is a member a layer holds by value and through a pointer.
type nestedLayer struct {
	Label string `json:"label,omitempty"`
}

// layer is a configuration layer holding each kind of member DecodeLayer
// reads over its defaults: a string, a number, a struct, a map and a pointer.
type layer struct {
	Name    string            `json:"name,omitempty"`
	Count   int               `json:"count,omitzero"`
	Nested  nestedLayer       `json:"nested,omitzero"`
	Rows    map[string]string `json:"rows,omitempty"`
	Pointer *nestedLayer      `json:"pointer,omitzero"`
}

// freshDefaults returns a new defaults value on each call, so no row shares a
// map or a pointer with another.
func freshDefaults() layer {
	return layer{
		Name:    "default",
		Count:   1,
		Nested:  nestedLayer{Label: "inner"},
		Rows:    map[string]string{"a": "1"},
		Pointer: &nestedLayer{Label: "pointed"},
	}
}

func TestDecodeLayer_ReadsOverACopyOfItsDefaults(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name string
		raw  string
		want func() layer
	}{
		{name: "an empty object keeps every default", raw: `{}`, want: freshDefaults},
		{name: "a number replaces its default", raw: `{"count":2}`, want: func() layer {
			layerValue := freshDefaults()
			layerValue.Count = 2
			return layerValue
		}},
		{name: "a map merges key by key", raw: `{"rows":{"b":"2"}}`, want: func() layer {
			layerValue := freshDefaults()
			layerValue.Rows = map[string]string{"a": "1", "b": "2"}
			return layerValue
		}},
		{name: "a map entry replaces its default", raw: `{"rows":{"a":"9"}}`, want: func() layer {
			layerValue := freshDefaults()
			layerValue.Rows = map[string]string{"a": "9"}
			return layerValue
		}},
		{name: "a struct member replaces its default", raw: `{"nested":{"label":"x"}}`, want: func() layer {
			layerValue := freshDefaults()
			layerValue.Nested.Label = "x"
			return layerValue
		}},
		{name: "a pointer's member replaces its default", raw: `{"pointer":{"label":"p"}}`, want: func() layer {
			layerValue := freshDefaults()
			layerValue.Pointer = &nestedLayer{Label: "p"}
			return layerValue
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got, err := options.DecodeLayer(freshDefaults(), []byte(tc.raw))
			require.NoError(t, err)
			assert.Empty(t, cmp.Diff(tc.want(), got))
		})
	}
}

func TestDecodeLayer_WritesNotItsDefaults(t *testing.T) {
	t.Parallel()
	defaults := freshDefaults()

	got, err := options.DecodeLayer(defaults, []byte(`{"rows":{"b":"2"}}`))
	require.NoError(t, err)
	assert.Empty(t, cmp.Diff(freshDefaults(), defaults), "decoding wrote the defaults")

	got.Rows["c"] = "3"
	got.Pointer.Label = "written"
	assert.Empty(t, cmp.Diff(freshDefaults(), defaults), "the result shares a map or a pointer with the defaults")
}

func TestDecodeLayer_RefusesAMemberWrittenNull(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		raw     string
		pointer string
	}{
		{raw: `null`, pointer: ""},
		{raw: `{"name":null}`, pointer: "/name"},
		{raw: `{"count":null}`, pointer: "/count"},
		{raw: `{"rows":null}`, pointer: "/rows"},
		{raw: `{"rows":{"a":null}}`, pointer: "/rows/a"},
		{raw: `{"nested":null}`, pointer: "/nested"},
		{raw: `{"nested":{"label":null}}`, pointer: "/nested/label"},
		{raw: `{"pointer":null}`, pointer: "/pointer"},
	} {
		t.Run(tc.raw, func(t *testing.T) {
			t.Parallel()
			got, err := options.DecodeLayer(freshDefaults(), []byte(tc.raw))
			require.ErrorIs(t, err, options.ErrNullMember)
			assert.EqualError(t, err, strconv.Quote(tc.pointer)+": member written null")
			assert.NotContains(t, err.Error(), "interface {}")
			assert.Empty(t, cmp.Diff(layer{}, got))
		})
	}
}

func TestDecodeLayer_RefusesWhatIsNotAStrictObject(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name string
		raw  string
		want error
	}{
		{name: "an unknown member", raw: `{"bogus":1}`, want: json.ErrUnknownName},
		{name: "an unknown nested member", raw: `{"nested":{"bogus":1}}`, want: json.ErrUnknownName},
		{name: "a name in another case", raw: `{"Name":"x"}`, want: json.ErrUnknownName},
		{name: "a repeated member", raw: `{"name":"a","name":"b"}`, want: jsontext.ErrDuplicateName},
		{name: "a repeated map key", raw: `{"rows":{"a":"1","a":"2"}}`, want: jsontext.ErrDuplicateName},
		{name: "empty input", raw: ``},
		{name: "an array", raw: `[]`},
		{name: "a string", raw: `"x"`},
		{name: "trailing data", raw: `{} {}`},
		{name: "a member of the wrong type", raw: `{"name":1}`},
		{name: "invalid UTF-8", raw: "{\"name\":\"\xff\"}"},
		{name: "a fault after a member that decoded", raw: `{"name":"set","rows":null}`, want: options.ErrNullMember},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got, err := options.DecodeLayer(freshDefaults(), []byte(tc.raw))
			require.Error(t, err)
			if tc.want != nil {
				assert.ErrorIs(t, err, tc.want)
			} else {
				assert.NotErrorIs(t, err, options.ErrNullMember)
			}
			assert.Empty(t, cmp.Diff(layer{}, got), "a refusal returns the zero value")
		})
	}
}

// unencodable holds a channel, which encodes only while it is nil and omitted.
type unencodable struct {
	Channel chan int `json:"channel,omitzero"`
}

// undecodable writes a JSON string its own decoder refuses.
type undecodable struct{}

// errUndecodable is what undecodable's decoder refuses its own encoding with.
var errUndecodable = errors.New("undecodable refuses every value")

// MarshalJSON writes a value UnmarshalJSON refuses.
func (undecodable) MarshalJSON() ([]byte, error) { return []byte(`"x"`), nil }

// UnmarshalJSON refuses every value, the one MarshalJSON writes among them.
func (*undecodable) UnmarshalJSON([]byte) error { return errUndecodable }

func TestDecodeLayer_RefusesDefaultsItCannotCopy(t *testing.T) {
	t.Parallel()

	nilChannel, err := options.DecodeLayer(unencodable{}, []byte(`{}`))
	require.NoError(t, err, "a nil channel is omitted and copies")
	assert.Nil(t, nilChannel.Channel)

	openChannel, err := options.DecodeLayer(unencodable{Channel: make(chan int)}, []byte(`{}`))
	require.Error(t, err)
	assert.ErrorContains(t, err, "copy defaults")
	assert.Nil(t, openChannel.Channel)

	_, err = options.DecodeLayer(undecodable{}, []byte(`{}`))
	require.ErrorIs(t, err, errUndecodable)
	assert.ErrorContains(t, err, "copy defaults")
}
