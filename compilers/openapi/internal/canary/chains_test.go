// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package canary_test

import (
	"bytes"
	"context"
	"fmt"
	"runtime"
	"strconv"
	"strings"
	"testing"

	soa "github.com/speakeasy-api/openapi/openapi"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// chainOf returns a document whose component parameters P0 to P(n-1) each name
// the next, P(n) being a parameter.
func chainOf(n int) string {
	entries := make([]string, 0, n+1)
	for i := range n {
		entries = append(entries, refTo("parameters", fmt.Sprintf("P%d", i), fmt.Sprintf("P%d", i+1)))
	}
	return section("parameters", append(entries, fmt.Sprintf("P%d: {name: q, in: query}", n))...)
}

// chainAsks is how many times chainRootAllocations asks a chain's head for its
// root node, so what the asks allocate stands clear of what reading the
// statistics does.
const chainAsks = 100

// chainRootAllocations resolves input, a document chainOf writes, and reports
// the bytes asking P0 for its root node chainAsks times allocates, or why it
// could not ask.
func chainRootAllocations(ctx context.Context, input []byte) string {
	doc, _, err := soa.Unmarshal(ctx, bytes.NewReader(input))
	if err != nil {
		return "decode: " + err.Error()
	}
	opts := soa.ResolveAllOptions{OpenAPILocation: location, DisableExternalRefs: true}
	if _, err := doc.ResolveAllReferences(ctx, opts); err != nil {
		return err.Error()
	}
	head := doc.Components.Parameters.GetOrZero("P0")
	var stats runtime.MemStats
	runtime.ReadMemStats(&stats)
	start := stats.TotalAlloc
	for range chainAsks {
		if head.GetRootNode() == nil {
			return "the head names no node"
		}
	}
	runtime.ReadMemStats(&stats)
	return strconv.FormatUint(stats.TotalAlloc-start, 10)
}

// chainGrowth is how many times what asking the head of a 64-hop chain
// allocates asking the head of a 256-hop one must exceed: once, were the end
// remembered, and about four times, as a walk keeps each hop it passes.
const chainGrowth = 3

// referenceChainWalk pins reference-chain-walk: a reference's object and root
// node are its chain's end, though its record names the next hop, and asking
// for them walks every hop, so asking the head of a chain four times as long
// allocates more than chainGrowth times as much.
func referenceChainWalk(t *testing.T) {
	t.Run("the answer is the chain's end", func(t *testing.T) {
		t.Parallel()
		doc, _, err := soa.Unmarshal(t.Context(), strings.NewReader(chainOf(3)))
		require.NoError(t, err)
		_, err = doc.ResolveAllReferences(t.Context(), soa.ResolveAllOptions{OpenAPILocation: location, DisableExternalRefs: true})
		require.NoError(t, err, revisit(t, result{}))
		params := doc.Components.Parameters
		head, next, end := params.GetOrZero("P0"), params.GetOrZero("P1"), params.GetOrZero("P3")
		require.NotNil(t, head.GetReferenceResolutionInfo(), revisit(t, result{}))
		assert.Same(t, next, head.GetReferenceResolutionInfo().Object, "its record names the next hop: %s", revisit(t, result{}))
		assert.Same(t, end.GetObject(), head.GetObject(), revisit(t, result{}))
		assert.Same(t, end.GetRootNode(), head.GetRootNode(), revisit(t, result{}))
	})
	t.Run("asking walks the chain", func(t *testing.T) {
		t.Parallel()
		cost := func(n int) uint64 {
			r := probe(t, "chain-root", chainOf(n))
			require.Equal(t, returned, r.outcome, revisit(t, r))
			allocated, err := strconv.ParseUint(r.report, 10, 64)
			require.NoError(t, err, revisit(t, r))
			return allocated
		}
		short, long := cost(64), cost(4*64)
		assert.Greater(t, long, chainGrowth*short, "%d and %d bytes: %s", short, long, revisit(t, result{}))
	})
}
