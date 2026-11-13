// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package bind

import (
	"encoding/json/jsontext"
	"math"
	"runtime"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	yaml "go.yaml.in/yaml/v3"

	"github.com/dexpace/morphic/compilers/openapi/internal/budget"
)

// binderOver returns a pass over the source src, read under the default budget,
// which has read nothing yet.
func binderOver(t *testing.T, src string) *binder {
	t.Helper()
	var root yaml.Node
	require.NoError(t, yaml.Unmarshal([]byte(src), &root))
	cfg := Config{Self: "spec.yaml"}
	w := &work{ctx: t.Context(), steps: budget.New(budget.AtMost(MaxSteps))}
	d := newDocs(&root, cfg, w)
	return newBinder(cfg, d, w, newIdentifiers(resources{d}.document(Source)))
}

// TestBinder_ReadPointerCostsItsPointersLength pins that a read spells the position it
// reaches once: a pointer twice as deep allocates about twice the bytes, where
// extending the position a token at a time copied it at each token and cost
// four times. It is not parallel, as the count is the process's, and takes the
// least of three counts, since anything else allocating only adds to one.
func TestBinder_ReadPointerCostsItsPointersLength(t *testing.T) {
	spine := func(levels int) (*binder, jsontext.Pointer) {
		src := "a: " + strings.Repeat("{p: ", levels) + "{}" + strings.Repeat("}", levels) + "\n"
		return binderOver(t, src), jsontext.Pointer("/a" + strings.Repeat("/p", levels))
	}
	cost := func(levels int) uint64 {
		b, pointer := spine(levels)
		r, reason := b.readPointer(Source, pointer)
		require.Equal(t, NoReason, reason)
		require.Equal(t, pointer, r.at.Pointer)
		var stats runtime.MemStats
		least := uint64(math.MaxUint64)
		for range 3 {
			runtime.ReadMemStats(&stats)
			before := stats.TotalAlloc
			for range 10 {
				_, _ = b.readPointer(Source, pointer)
			}
			runtime.ReadMemStats(&stats)
			least = min(least, stats.TotalAlloc-before)
		}
		return least
	}
	assert.Less(t, cost(800), 3*cost(400), "twice the depth, about twice the bytes")
}

// TestBinder_ReadPointerReadsToItsTokenCap pins maxPointerTokens: a pointer of that
// many tokens is read whole, and one a token longer names nothing, however
// deep its document nests.
func TestBinder_ReadPointerReadsToItsTokenCap(t *testing.T) {
	t.Parallel()
	levels := maxPointerTokens + 1
	b := binderOver(t, strings.Repeat("{p: ", levels)+"{}"+strings.Repeat("}", levels)+"\n")

	_, reason := b.readPointer(Source, jsontext.Pointer(strings.Repeat("/p", maxPointerTokens)))
	assert.Equal(t, NoReason, reason)
	_, reason = b.readPointer(Source, jsontext.Pointer(strings.Repeat("/p", maxPointerTokens+1)))
	assert.Equal(t, NoTarget, reason)
}
