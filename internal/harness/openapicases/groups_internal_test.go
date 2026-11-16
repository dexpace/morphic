// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package openapicases

import (
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestWithDialect_NamesTheDialectWhereTheSchemaDoesNot(t *testing.T) {
	for _, tc := range []struct{ name, schema, want string }{
		{"an object naming none", `{"type": "string", "minLength": 1}`,
			`{"$schema": "` + dialect + `", "type": "string", "minLength": 1}`},
		{"an empty object", `{}`, `{"$schema": "` + dialect + `"}`},
		{"an object naming its own", `{"type": "string", "$schema": "x"}`, `{"type": "string", "$schema": "x"}`},
		{"a boolean schema", `true`, `true`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got, err := withDialect([]byte(tc.schema))
			require.NoError(t, err)
			assert.JSONEq(t, tc.want, string(got))
		})
	}
	got, err := withDialect([]byte(`{"type": "string"}`))
	require.NoError(t, err)
	assert.Regexp(t, `^\{\s*"\$schema"`, string(got), "the dialect is named first")

	for _, bad := range []string{``, `{`, `{"a"`} {
		_, err := withDialect([]byte(bad))
		assert.Error(t, err, "%q", bad)
	}
}

// TestGroupsWrite_PanicsOnWhatItCannotWrite pins the panics CheckCase
// reports: a suite that did not read, a group whose instance or schema is no
// JSON.
func TestGroupsWrite_PanicsOnWhatItCannotWrite(t *testing.T) {
	broken := Groups().(groupCases)
	broken.suite = errors.New("openapicases: the test suite: unreadable")
	assert.PanicsWithValue(t, "openapicases: the test suite: unreadable", func() { broken.Write([]int{0, 0, 0, 0}) })

	for name, g := range map[string]Group{
		"an instance that is no JSON": {Name: "f/g", Schema: []byte(`{}`), Tests: []Test{{Data: []byte(`{`), Valid: true}}},
		"a schema that is no JSON":    {Name: "f/g", Schema: []byte(`{"a"`), Tests: []Test{{Data: []byte(`1`), Valid: true}}},
	} {
		c := Groups().(groupCases)
		c.groups = []Group{g}
		assert.Panics(t, func() { c.Write([]int{0, 0, 0, 0}) }, name)
	}
}
