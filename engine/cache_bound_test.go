// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package engine_test

import (
	"context"
	"fmt"
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dexpace/morphic/compilers"
	"github.com/dexpace/morphic/engine"
)

// boundDoc is a document a boundReader serves: its bytes, the space it states
// and its bound.
type boundDoc struct {
	data         string
	space, bound compilers.AddressSpace
}

// boundReader serves documents by location, refusing a bounded read whose
// bound lies past its reach unless lax, as a reader ignoring the reach would.
type boundReader struct {
	mu   sync.Mutex
	docs map[string]boundDoc
	lax  bool
}

// ReadDocument implements compilers.DocumentReader.
func (r *boundReader) ReadDocument(_ context.Context, req compilers.DocumentRequest) (compilers.DocumentRead, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	d, ok := r.docs[req.Location]
	switch {
	case !ok:
		return compilers.DocumentRead{}, fmt.Errorf("no document %s", req.Location)
	case !r.lax && req.Reach != compilers.SpaceUnstated && d.bound > req.Reach:
		return compilers.DocumentRead{}, &compilers.AddressSpaceError{Address: req.Location, Space: d.bound}
	}
	return compilers.DocumentRead{Data: []byte(d.data), Space: d.space, Bound: d.bound}, nil
}

// set replaces the document at location and whether reaches are ignored.
func (r *boundReader) set(location string, d boundDoc, lax bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.docs[location], r.lax = d, lax
}

// TestWithCache_AReadIsReplayedWithItsSpaceAndBound pins that a run answered
// from the cache is the run a compile would give: each read is replayed and
// must state the space and the bound it stated, so a document read through a
// proxy whose bound moved past a public document's reach is compiled again and
// refused, and a refusal, kept or not, is answered as the compile answers it.
func TestWithCache_AReadIsReplayedWithItsSpaceAndBound(t *testing.T) {
	t.Parallel()
	const proxied = "http://name.example/d.yaml"
	docs := &boundReader{docs: map[string]boundDoc{
		"https://pub.example/a.yaml": {data: "X: {$ref: '" + proxied + "#/D'}\n",
			space: compilers.SpacePublic, bound: compilers.SpacePublic},
		proxied: {data: "D: {type: string}\n", bound: compilers.SpacePublic},
	}}
	spec := writeSpec(t, "openapi: 3.1.0\ninfo: {title: t, version: '1'}\npaths: {}\ncomponents:\n  schemas:\n"+
		"    A: {$ref: 'https://pub.example/a.yaml#/X'}\n")
	opts := engine.RunOptions{CompilerOptions: map[string]string{"allow-external-refs": "true"}}
	store := newMemoryCache()
	eng, counter := cachedEngine(t, store, "b1")
	engine.ReadUnder(eng, docs)
	run := func() *engine.Result {
		t.Helper()
		res, err := eng.Run(t.Context(), spec, opts)
		require.NoError(t, err)
		return res
	}

	cold := run()
	require.False(t, cold.Failed, "%v", cold.Diagnostics)
	_, entry := store.only(t)
	assert.Contains(t, string(entry), `"bound":1`, "the record keeps each read's bound")
	assertSameRun(t, cold, run())
	assert.EqualValues(t, 1, counter.compiles.Load(), "answered from the entry")

	// The name now resolves to a private address, and a reader ignoring the
	// reach still serves it: the bound it states is no longer the recorded one.
	docs.set(proxied, boundDoc{data: "D: {type: string}\n", bound: compilers.SpacePrivate}, true)
	moved := run()
	assert.EqualValues(t, 2, counter.compiles.Load(), "a moved bound is no hit")
	require.True(t, moved.Failed)
	assert.Contains(t, fmt.Sprint(moved.Diagnostics), "it is at a private address")
	assertSameRun(t, moved, run())
	assert.EqualValues(t, 2, counter.compiles.Load(), "the refusal is answered from the entry")

	// A reader refusing the read leaves a record no replay vouches for.
	docs.set(proxied, boundDoc{data: "D: {type: string}\n", bound: compilers.SpacePrivate}, false)
	refused := run()
	again := run()
	assert.EqualValues(t, 4, counter.compiles.Load(), "a refused read is compiled each time")
	assertSameRun(t, refused, again)
	assert.Contains(t, fmt.Sprint(again.Diagnostics), "it is at a private address")
}
