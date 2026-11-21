// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package e2e_test

import (
	"maps"
	"slices"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dexpace/morphic/compilers/openapi"
	"github.com/dexpace/morphic/ir"
)

// nullChainDocs is a chain of three hops across three documents: a 3.1 one's
// site reaches a 3.0 document's alias writing `nullable: true`, which reaches
// a 3.1 document's alias, which reaches a string.
var nullChainDocs = map[string]string{
	"mid.yaml": "openapi: 3.0.3\ninfo: {title: m, version: '1'}\npaths: {}\ncomponents:\n  schemas:\n" +
		"    Mid: {$ref: 'tail.yaml#/Tail', nullable: true}\n",
	"tail.yaml": "Tail: {$ref: '#/Leaf'}\nLeaf: {type: string}\n",
}

// nullChainSites are the three places the chain is read from: its first hop
// in the 3.1 source, its middle hop and its last.
var nullChainSites = []string{
	"        a: {$ref: 'mid.yaml#/components/schemas/Mid'}\n",
	"        b: {$ref: 'tail.yaml#/Tail'}\n",
	"        c: {$ref: 'tail.yaml#/Leaf'}\n",
}

// nullChainSpec is a source whose model M holds the named sites.
func nullChainSpec(sites ...string) string {
	return dynHead + "    M:\n      type: object\n      properties:\n" + strings.Join(sites, "")
}

// nullChainReadings compiles sites together and returns each property's
// TypeRef.Nullable by wire name and every diagnostic as text.
func nullChainReadings(t *testing.T, sites ...string) (map[string]bool, []string) {
	t.Helper()
	dir := t.TempDir()
	files := map[string]string{"main.yaml": nullChainSpec(sites...)}
	maps.Copy(files, nullChainDocs)
	layout(t, dir, files)
	doc, diags := compileDir(t, dir, openapi.Options{})
	require.NotNil(t, doc)
	model, ok := doc.Types[componentID("M")].(*ir.Model)
	require.True(t, ok)
	out := map[string]bool{}
	for _, property := range model.Properties {
		out[property.WireName] = property.Type.Nullable
	}
	return out, lines(diags)
}

// TestCompile_AChainAcrossDialectsIsReadTheSameFromEachOfItsSites pins that
// what a $ref chain says about null does not depend on which of its sites is
// read, in which order, or beside which others: the chain's alias writing
// `nullable` is read in its own document's dialect each time, whoever asks.
// Each site's reading together with the others is the reading it has alone.
func TestCompile_AChainAcrossDialectsIsReadTheSameFromEachOfItsSites(t *testing.T) {
	t.Parallel()
	alone := map[string]bool{}
	var wantDiags []string
	for _, site := range nullChainSites {
		got, diags := nullChainReadings(t, site)
		require.Len(t, got, 1)
		maps.Copy(alone, got)
		wantDiags = append(wantDiags, diags...)
	}

	slices.Sort(wantDiags)
	require.NotEmpty(t, wantDiags, "the alias writing nullable is read, and says so")
	assert.True(t, alone["a"], "a 3.0 alias writing nullable widens what it reaches")
	assert.False(t, alone["c"])

	for _, order := range [][]int{{0, 1, 2}, {2, 1, 0}, {1, 0, 2}, {1, 2, 0}} {
		sites := make([]string, 0, len(order))
		for _, i := range order {
			sites = append(sites, nullChainSites[i])
		}
		together, diags := nullChainReadings(t, sites...)
		assert.Equal(t, alone, together, "sites in order %v", order)
		slices.Sort(diags)
		assert.Equal(t, wantDiags, diags, "findings in order %v", order)
	}
}
