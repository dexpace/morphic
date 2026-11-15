// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package engine_test

import (
	"context"
	"fmt"
	"testing"

	"github.com/google/go-cmp/cmp"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dexpace/morphic/compilers"
	"github.com/dexpace/morphic/engine"
)

// TestWithCache_AMovedSpaceIsNoHit pins that a replay holds each read to the
// space it stated as well as to its bytes and bound: a document whose space
// moves from private to public, its bytes and bound as they were, bounds its
// references differently, so the run is compiled again and its reference to a
// private document refused.
func TestWithCache_AMovedSpaceIsNoHit(t *testing.T) {
	t.Parallel()
	const (
		fetched = "https://pub.example/a.yaml"
		inner   = "http://10.0.0.5/b.yaml"
	)
	docs := &boundReader{docs: map[string]boundDoc{
		fetched: {data: "X: {$ref: '" + inner + "#/D'}\n", space: compilers.SpacePrivate, bound: compilers.SpacePrivate},
		inner:   {data: "D: {type: string}\n", space: compilers.SpacePrivate, bound: compilers.SpacePrivate},
	}}
	spec := writeSpec(t, "openapi: 3.1.0\ninfo: {title: t, version: '1'}\npaths: {}\ncomponents:\n  schemas:\n"+
		"    A: {$ref: '"+fetched+"#/X'}\n")
	opts := engine.RunOptions{CompilerOptions: map[string]string{"allow-external-refs": "true"}}
	eng, counter := cachedEngine(t, newMemoryCache(), "b1")
	engine.ReadUnder(eng, docs)
	run := func() *engine.Result {
		t.Helper()
		res, err := eng.Run(t.Context(), spec, opts)
		require.NoError(t, err)
		return res
	}

	cold := run()
	require.False(t, cold.Failed, "%v", cold.Diagnostics)
	assertSameRun(t, cold, run())
	require.EqualValues(t, 1, counter.compiles.Load(), "answered from the entry")

	docs.set(fetched, boundDoc{data: "X: {$ref: '" + inner + "#/D'}\n", space: compilers.SpacePublic, bound: compilers.SpacePrivate}, true)
	moved := run()

	assert.EqualValues(t, 2, counter.compiles.Load(), "a moved space is no hit")
	require.True(t, moved.Failed, "the reference to the private document is refused")
	assert.Contains(t, fmt.Sprint(moved.Diagnostics), "it is at a private address")
}

// inTurnReader answers each read from the next of its answers.
type inTurnReader struct{ answers []compilers.DocumentRead }

func (r *inTurnReader) ReadDocument(context.Context, compilers.DocumentRequest) (compilers.DocumentRead, error) {
	answer := r.answers[0]
	r.answers = r.answers[1:]
	return answer, nil
}

// TestRecorder_ARepeatStatingAnotherSpaceOrBoundIsUnsound pins that a request
// answered twice in one compile with the same bytes but another space or bound
// leaves a record no replay can vouch for.
func TestRecorder_ARepeatStatingAnotherSpaceOrBoundIsUnsound(t *testing.T) {
	t.Parallel()
	req := compilers.DocumentRequest{Location: "a.yaml"}
	read := func(space, bound compilers.AddressSpace) compilers.DocumentRead {
		return compilers.DocumentRead{Data: []byte("a"), Space: space, Bound: bound}
	}
	tests := []struct {
		name        string
		first, next compilers.DocumentRead
		sound       bool
	}{
		{"the same space and bound", read(compilers.SpacePublic, compilers.SpacePublic),
			read(compilers.SpacePublic, compilers.SpacePublic), true},
		{"another space", read(compilers.SpacePublic, compilers.SpacePublic),
			read(compilers.SpacePrivate, compilers.SpacePublic), false},
		{"another bound", read(compilers.SpacePublic, compilers.SpacePublic),
			read(compilers.SpacePublic, compilers.SpacePrivate), false},
	}
	for _, tc := range tests {
		rec := engine.NewRecorder(&inTurnReader{answers: []compilers.DocumentRead{tc.first, tc.next}})
		for range 2 {
			_, err := rec.ReadDocument(t.Context(), req)
			require.NoError(t, err, tc.name)
		}
		_, sound := engine.SettledLocations(rec)
		assert.Equal(t, tc.sound, sound, tc.name)
	}
}

// TestRecorder_ReadsDifferingOnlyInReachAreListedInOneOrder pins that an
// entry's list of reads is in one order whatever the order the recorder held
// them in: two requests for one document that differ only in reach are listed
// by reach, in every run, so the bytes of the entry do not follow a map.
func TestRecorder_ReadsDifferingOnlyInReachAreListedInOneOrder(t *testing.T) {
	t.Parallel()
	docs := &repeatingReader{read: compilers.DocumentRead{Data: []byte("a")}}
	want := []compilers.DocumentRequest{
		{Location: "a.yaml", Reach: compilers.SpaceUnstated},
		{Location: "a.yaml", Reach: compilers.SpacePublic},
		{Location: "a.yaml", Reach: compilers.SpacePrivate},
		{Location: "a.yaml", Reach: compilers.SpaceLocal},
	}
	for run := range 20 {
		rec := engine.NewRecorder(docs)
		// A different order each run, so one that happens to sort is not the answer.
		for i := range want {
			req := want[(i+run)%len(want)]
			_, err := rec.ReadDocument(t.Context(), req)
			require.NoError(t, err)
		}
		if diff := cmp.Diff(want, engine.SettledRequests(rec)); diff != "" {
			t.Fatalf("run %d: the reads are not listed by reach (-want +got):\n%s", run, diff)
		}
	}
}

// repeatingReader answers every read with one answer.
type repeatingReader struct{ read compilers.DocumentRead }

func (r *repeatingReader) ReadDocument(context.Context, compilers.DocumentRequest) (compilers.DocumentRead, error) {
	return r.read, nil
}
