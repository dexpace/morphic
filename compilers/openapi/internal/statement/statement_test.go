// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package statement_test

import (
	"fmt"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	yaml "go.yaml.in/yaml/v3"

	"github.com/dexpace/morphic/compilers/openapi/internal/statement"
)

// wide returns a component W writing n extension keys, x-0 to x-(n-1).
func wide(n int) string {
	keys := make([]string, n)
	for i := range n {
		keys[i] = fmt.Sprintf("x-%d: %d", i, i)
	}
	return document("3.1.0", "W: {"+strings.Join(keys, ", ")+"}")
}

// TestSchema_KeyOnAWideObjectScansNothing pins that an object writing many
// keys answers Key from its index: a lookup allocates nothing, and the keys it
// compares do not grow from 2,000 keys to 20,000. It runs serially, as
// AllocsPerRun counts the whole process.
func TestSchema_KeyOnAWideObjectScansNothing(t *testing.T) {
	compared := map[int]int{}
	for _, n := range []int{2000, 20000} {
		table, loweringContext := tableOver(t, wide(n), rulesAt(t, "3.1.0", ""))
		st := table.At(component(loweringContext, "W"))
		require.Len(t, st.Keys, n)
		last := fmt.Sprintf("x-%d", n-1)
		key, read := st.Key(last)
		require.True(t, read)
		assert.Equal(t, last, key.Name)
		_, read = st.Key("absent")
		assert.False(t, read)
		assert.Zero(t, testing.AllocsPerRun(100, func() { st.Key(last) }), "a lookup allocates nothing")
		compared[n] = statement.Compared(st.Schema, last)
	}
	assert.Equal(t, compared[2000], compared[20000], "the keys compared do not grow with the object")
}

// TestSchema_KeyReadsANullKeywordAsLeftOut pins the one reading a null keyword
// has, which every keeper goes through, and the three keywords whose value is
// an instance, where a null is that value
// (docs/compilers/openapi.md#null-keywords).
func TestSchema_KeyReadsANullKeywordAsLeftOut(t *testing.T) {
	t.Parallel()
	table, loweringContext := tableOver(t, document("3.1.0",
		"A: {maxLength: null, format: ~, $id: null, const: null, default: ~, example: null, title: t}"),
		rulesAt(t, "3.1.0", ""))
	a := table.At(component(loweringContext, "A"))
	for _, keyword := range []string{"maxLength", "format", "$id"} {
		key, read := a.Key(keyword)
		assert.False(t, read, keyword)
		assert.True(t, key.Null, "%s is written", keyword)
	}
	for _, keyword := range []string{"const", "default", "example"} {
		key, read := a.Key(keyword)
		require.True(t, read, keyword)
		assert.Equal(t, "!!null", key.Node.ShortTag(), keyword)
	}
	_, read := a.Key("title")
	assert.True(t, read)
}

// TestSchema_KeyReadsAKeywordAsTheParserDoes pins the keys every verbatim
// keyword and every bound is read from beside the model: a keyword merged in
// through `<<` and one written through an alias key are read (GitHub #395,
// #384), a keyword written twice at its last copy, a value written as an alias
// as what it names, and a schema whose whole body is an alias through it.
func TestSchema_KeyReadsAKeywordAsTheParserDoes(t *testing.T) {
	t.Parallel()
	table, loweringContext := tableOver(t, `openapi: 3.1.0
info: {title: t, version: "1"}
paths: {}
x-keys: [&max maximum]
x-base: &base {maximum: 9}
components:
  schemas:
    Merged: {type: integer, <<: *base}
    AliasKey: {type: integer, *max : 7}
    Repeated: {type: integer, maximum: 1, maximum: 2}
    Anchored: &anchored {type: integer, maximum: &n 5}
    Aliased: *anchored
    AliasValue: {type: integer, maximum: *n}
`, rulesAt(t, "3.1.0", ""))
	for name, want := range map[string]string{
		"Merged": "9", "AliasKey": "7", "Repeated": "2", "AliasValue": "5", "Aliased": "5",
	} {
		key, read := table.At(component(loweringContext, name)).Key("maximum")
		require.True(t, read, "%s writes maximum as the parser reads it", name)
		assert.Equal(t, yaml.ScalarNode, key.Node.Kind, "%s: the value's alias is followed", name)
		assert.Equal(t, want, key.Node.Value, name)
	}
}

// TestKey_DefinedTellsAKeywordTheRowsDefine pins that a key is defined where
// the rows the schema is read with place it: `dependencies` by the drafts and
// not by 2020-12, an extension by neither.
func TestKey_DefinedTellsAKeywordTheRowsDefine(t *testing.T) {
	t.Parallel()
	const draft07 = "http://json-schema.org/draft-07/schema#"
	for _, tc := range []struct {
		named string
		want  map[string]bool
	}{
		{"", map[string]bool{"type": true, "dependencies": false, "x-a": false}},
		{draft07, map[string]bool{"type": true, "dependencies": true, "x-a": false}},
	} {
		src := document("3.1.0", "A: {type: object, dependencies: {a: [b]}, x-a: 1}")
		if tc.named != "" {
			src = "jsonSchemaDialect: '" + tc.named + "'\n" + src
		}
		table, loweringContext := tableOver(t, src, rulesAt(t, "3.1.0", tc.named))
		a := table.At(component(loweringContext, "A"))
		for key, want := range tc.want {
			keyValue, read := a.Key(key)
			require.True(t, read, key)
			assert.Equal(t, want, keyValue.Defined(), "%q reading %q", key, tc.named)
		}
	}
}
