// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package runtimeexpr_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dexpace/morphic/compilers/openapi/internal/runtimeexpr"
	"github.com/dexpace/morphic/ir"
)

// TestLookups_WalkNamesEachMemberFromTheRoot pins a walk to the declaration of
// each token's member, through the type of the member before it, and to
// nothing where a step names no member or reaches no model.
func TestLookups_WalkNamesEachMemberFromTheRoot(t *testing.T) {
	t.Parallel()
	for name, tc := range map[string]struct {
		root   ir.TypeID
		tokens []string
		want   []ir.PropID
		ok     bool
	}{
		"no token":                     {root: "t/page", want: []ir.PropID{}, ok: true},
		"one member":                   {root: "t/page", tokens: []string{"items"}, want: []ir.PropID{"p/items"}, ok: true},
		"a chain":                      {root: "t/page", tokens: []string{"info", "next"}, want: []ir.PropID{"p/info", "p/next"}, ok: true},
		"a member the model lacks":     {root: "t/page", tokens: []string{"gone"}},
		"a step through a scalar":      {root: "t/page", tokens: []string{"items", "next"}},
		"a root that is no model":      {root: "t/text", tokens: []string{"next"}},
		"a root the registry lacks":    {root: "t/gone", tokens: []string{"next"}},
		"a member of the wrong model":  {root: "t/info", tokens: []string{"items"}},
		"a member named by its own ID": {root: "t/page", tokens: []string{"p/items"}},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			got, ok := runtimeexpr.NewLookups(registry()).Walk(tc.root, tc.tokens)
			assert.Equal(t, tc.ok, ok)
			assert.Equal(t, tc.want, got)
		})
	}
}

// TestLookups_HeaderMatchesTheNameWithoutCase pins a header to its wire name as
// HTTP compares one, and to every header a response declares under it.
func TestLookups_HeaderMatchesTheNameWithoutCase(t *testing.T) {
	t.Parallel()
	headers := []ir.Header{
		{Property: member("h/rate", "X-Rate", "t/text")},
		{Property: member("h/next", "Link", "t/text")},
		{Property: member("h/again", "x-rate", "t/text")},
	}
	lk := runtimeexpr.NewLookups(registry())
	at := runtimeexpr.ResponseKey{Op: "op/x"}
	assert.Equal(t, []ir.PropID{"h/rate", "h/again"}, lk.Header(at, headers, "X-RATE"))
	assert.Equal(t, []ir.PropID{"h/next"}, lk.Header(at, headers, "link"))
	assert.Empty(t, lk.Header(at, headers, "ETag"))
}

// TestLookups_ParamIndexBindsByWhereTheOperationPlacesThem pins the index to the
// operation's HTTP bindings, which is where an expression names a parameter.
func TestLookups_ParamIndexBindsByWhereTheOperationPlacesThem(t *testing.T) {
	t.Parallel()
	op := &ir.Operation{
		ID:     "op/x",
		Params: []ir.Parameter{{ID: "param/q", Name: ir.Naming{Source: "q"}}},
		Bindings: ir.OpBindings{HTTP: []ir.HTTPBinding{{
			ParamBindings: []ir.HTTPParamBinding{{Param: "param/q", Location: ir.HTTPLocationQuery}},
		}}},
	}
	ix := runtimeexpr.NewLookups(registry()).ParamIndex(op)
	assert.Equal(t, []ir.ParamID{"param/q"}, ix.Bound(ir.HTTPLocationQuery, "q"))
	assert.Empty(t, ix.Bound(ir.HTTPLocationHeader, "q"))
}

// TestLookups_IndexEachListOnceWhenAValueFirstReadsIt pins what makes resolving
// grow with the expressions and what they read rather than with the product of
// the two: a list is indexed by the first read of it, and no later read
// allocates. A response is indexed by its key, so two that differ in the list
// they sit in, or in where, are two lists. It does not run in parallel, since
// the count is process-wide.
func TestLookups_IndexEachListOnceWhenAValueFirstReadsIt(t *testing.T) {
	lk := runtimeexpr.NewLookups(registry())
	op := &ir.Operation{ID: "op/x"}
	headers := []ir.Header{{Property: member("h/rate", "X-Rate", "t/text")}}
	// The header is read by a lower-case name, which folding returns as it is.
	reads := map[string]func(){
		"a model's members":  func() { lk.Member("t/page", "info") },
		"a parameter index":  func() { lk.ParamIndex(op) },
		"a response headers": func() { lk.Header(runtimeexpr.ResponseKey{Op: "op/x"}, headers, "x-rate") },
	}
	models, operations, responses := lk.Indexed()
	require.Zero(t, models+operations+responses, "nothing is indexed before a value reads it")

	for name, read := range reads {
		read()
		assert.Zero(t, testing.AllocsPerRun(10, read), "%s: a second read builds no index", name)
	}
	models, operations, responses = lk.Indexed()
	assert.Equal(t, [3]int{1, 1, 1}, [3]int{models, operations, responses}, "one list of each kind is indexed")

	lk.Header(runtimeexpr.ResponseKey{Op: "op/x", List: 1}, headers, "X-Rate")
	lk.Header(runtimeexpr.ResponseKey{Op: "op/x", Index: 1}, headers, "X-Rate")
	lk.Header(runtimeexpr.ResponseKey{Op: "op/y"}, headers, "X-Rate")
	_, _, responses = lk.Indexed()
	assert.Equal(t, 4, responses, "a response differing in operation, list or index is another list")
}
