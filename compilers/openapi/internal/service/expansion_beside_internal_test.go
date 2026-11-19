// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package service

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	yaml "go.yaml.in/yaml/v3"

	"github.com/dexpace/morphic/compilers/openapi/internal/bind"
	"github.com/dexpace/morphic/compilers/openapi/internal/budget"
	"github.com/dexpace/morphic/compilers/openapi/internal/openapitest"
	"github.com/dexpace/morphic/compilers/openapi/internal/ynode"
)

// TestExpansion_AHopWeighsWhatItWritesBesideItsRef pins the weight of a hop of a
// chain, of which a use copies the fields beside its $ref and not the $ref: a
// mapping, and each key and value written beside the $ref, wherever in the
// mapping the $ref is written. The end of the chain is weighed whole.
func TestExpansion_AHopWeighsWhatItWritesBesideItsRef(t *testing.T) {
	t.Parallel()
	at := bind.Reading{}
	ref := func() []*yaml.Node { return []*yaml.Node{ynode.Scalar("$ref"), ynode.Scalar("x")} }
	summary := func() []*yaml.Node { return []*yaml.Node{ynode.Scalar("summary"), ynode.Scalar("abc")} }

	for name, tc := range map[string]struct {
		hop  *yaml.Node
		want int
	}{
		"a bare $ref":                     {ynode.Map(ref()...), 1},
		"a $ref and a summary":            {ynode.Map(append(ref(), summary()...)...), 13},
		"a summary and then the $ref":     {ynode.Map(append(summary(), ref()...)...), 13},
		"a summary alone":                 {ynode.Map(summary()...), 1 + 8 + 4},
		"a longer target changes nothing": {ynode.Map(ynode.Scalar("$ref"), ynode.Scalar("#/components/parameters/Q")), 1},
	} {
		assert.Equal(t, tc.want, weightBeside(tc.hop, at, budget.Unbounded()), name)
	}

	end := ynode.Map(ref()...)
	assert.Equal(t, 1+5+2, referenceExpansionSize(end, at, budget.Unbounded()), "the end is weighed whole, its $ref among it")
}

// TestExpansion_AHopNoMappingHoldsIsWeighedWhole pins weightBeside's fallback:
// what is no mapping, or is read as a schema, writes nothing beside a $ref, so
// it is weighed as referenceExpansionSize weighs it.
func TestExpansion_AHopNoMappingHoldsIsWeighedWhole(t *testing.T) {
	t.Parallel()
	scalar := ynode.Scalar("abc")
	assert.Equal(t, referenceExpansionSize(scalar, bind.Reading{}, budget.Unbounded()), weightBeside(scalar, bind.Reading{}, budget.Unbounded()))

	table := bind.Bind(t.Context(), openapitest.YAMLDocument(t, "openapi: 3.1.0\n"), bind.Config{Self: "spec.yaml"})
	schema := table.ReadingAt(bind.DocumentPointer{Pointer: "/components/schemas/S"})
	require.True(t, schema.Schema())
	hop := ynode.Map(ynode.Scalar("$ref"), ynode.Scalar("x"))
	assert.Equal(t, 1, weightBeside(hop, schema, budget.Unbounded()), "a schema weighs one, as weightOf weighs it")
}

// TestBeside_ReadsNothingOffNoMapping holds the readers of what a reference
// writes beside its $ref to nothing where there is no mapping to read: no
// node, a scalar, and a core holding no node.
func TestBeside_ReadsNothingOffNoMapping(t *testing.T) {
	t.Parallel()
	assert.False(t, writesBeside(nil))
	assert.False(t, writesBeside(ynode.Scalar("x")))
	assert.Nil(t, writtenRoot(struct{}{}))
	assert.Empty(t, appendRoot(nil, struct{}{}))
}
