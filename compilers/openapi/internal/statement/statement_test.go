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
		table, c := tableOver(t, wide(n), rulesAt(t, "3.1.0", ""))
		st := table.At(component(c, "W"))
		require.Len(t, st.Keys, n)
		last := fmt.Sprintf("x-%d", n-1)
		k, read := st.Key(last)
		require.True(t, read)
		assert.Equal(t, last, k.Name)
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
	table, c := tableOver(t, document("3.1.0",
		"A: {maxLength: null, format: ~, $id: null, const: null, default: ~, example: null, title: t}"),
		rulesAt(t, "3.1.0", ""))
	a := table.At(component(c, "A"))
	for _, keyword := range []string{"maxLength", "format", "$id"} {
		k, read := a.Key(keyword)
		assert.False(t, read, keyword)
		assert.True(t, k.Null, "%s is written", keyword)
	}
	for _, keyword := range []string{"const", "default", "example"} {
		k, read := a.Key(keyword)
		require.True(t, read, keyword)
		assert.Equal(t, "!!null", k.Node.ShortTag(), keyword)
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
	table, c := tableOver(t, `openapi: 3.1.0
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
		k, read := table.At(component(c, name)).Key("maximum")
		require.True(t, read, "%s writes maximum as the parser reads it", name)
		assert.Equal(t, yaml.ScalarNode, k.Node.Kind, "%s: the value's alias is followed", name)
		assert.Equal(t, want, k.Node.Value, name)
	}
}
