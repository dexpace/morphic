// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package load

import (
	"errors"
	"fmt"
	"net/http"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	yaml "go.yaml.in/yaml/v3"

	"github.com/dexpace/morphic/compilers/openapi/internal/budget"
	"github.com/dexpace/morphic/compilers/openapi/internal/diag"
	"github.com/dexpace/morphic/compilers/openapi/internal/provenance"
	"github.com/dexpace/morphic/ir"
)

// TestExternalReads_OnceReadsEachDocumentOnce pins the record's one read per
// key: the answer the first ask got is every later ask's, a failure included,
// each key keeps its own, and only a first ask is charged.
func TestExternalReads_OnceReadsEachDocumentOnce(t *testing.T) {
	t.Parallel()
	r := newExternalReads(sourceDocument{}, budget.Unbounded())
	reads := map[string]int{}
	readAs := func(key string, a answer) func() answer {
		return func() answer {
			reads[key]++
			return a
		}
	}
	gone := answer{err: errors.New("GET k: 404 Not Found")}
	found := answer{body: []byte("doc")}

	first := r.once("k", readAs("k", gone))
	again := r.once("k", readAs("k", found))
	other := r.once("other", readAs("other", found))

	assert.Equal(t, gone, first)
	assert.Equal(t, gone, again, "a later ask gets the first answer, not a read of its own")
	assert.Equal(t, found, other, "each key keeps its own answer")
	assert.Equal(t, map[string]int{"k": 1, "other": 1}, reads)
	assert.Equal(t, 2, r.documents.Spent(), "a document is charged once")
}

// TestExternalReads_OnceReadsOnceUnderConcurrentAsks pins the one read where
// asks race: every ask for one key, from goroutines at once, gets the answer of
// a single read, and the document is charged once.
func TestExternalReads_OnceReadsOnceUnderConcurrentAsks(t *testing.T) {
	t.Parallel()
	r := newExternalReads(sourceDocument{}, budget.Unbounded())
	var reads atomic.Int32
	var wg sync.WaitGroup
	got := make([]answer, 16)
	for i := range got {
		wg.Go(func() {
			got[i] = r.once("k", func() answer {
				return answer{body: fmt.Appendf(nil, "read %d", reads.Add(1))}
			})
		})
	}
	wg.Wait()

	assert.Equal(t, int32(1), reads.Load())
	for _, a := range got {
		assert.Equal(t, "read 1", string(a.body))
	}
	assert.Equal(t, 1, r.documents.Spent())
}

// TestExternalReads_OnceRefusesADocumentPastTheBudget pins the budget's edge:
// limit documents are read, the next is refused unread with the budget named,
// and a refused one stays refused, however often it is asked for.
func TestExternalReads_OnceRefusesADocumentPastTheBudget(t *testing.T) {
	t.Parallel()
	r := newExternalReads(sourceDocument{}, budget.AtMost(2))
	read := 0
	fetch := func() answer {
		read++
		return answer{}
	}
	for i := range 2 {
		assert.NoError(t, r.once(fmt.Sprintf("k%d", i), fetch).err)
	}

	past := r.once("k2", fetch)
	again := r.once("k2", fetch)

	require.Error(t, past.err)
	assert.Equal(t, "external document k2 refused: it is past the 2-document budget", past.err.Error())
	assert.Equal(t, past, again, "the refusal is the document's answer")
	assert.Equal(t, 2, read, "nothing past the budget is read")
	assert.Equal(t, 3, r.documents.Spent(), "the refused document is charged, so the budget reads as crossed")
}

// TestExternalReads_OnceLeavesAFailureWhereAReadPanics pins that a read
// stopped by a panic, which the resolution walk recovers from, leaves its
// document failed rather than answered as empty, and is not read again.
func TestExternalReads_OnceLeavesAFailureWhereAReadPanics(t *testing.T) {
	t.Parallel()
	r := newExternalReads(sourceDocument{}, budget.Unbounded())
	assert.Panics(t, func() {
		r.once("k", func() answer { panic("read") })
	})

	got := r.once("k", func() answer { return answer{} })

	require.Error(t, got.err)
	assert.Equal(t, "external document k refused: its read did not finish", got.err.Error())
}

// TestExternalReads_ExhaustedReportsOnceAtTheSource pins the report: nothing
// while every read was within the budget, and one error at the source once a
// document fell past it.
func TestExternalReads_ExhaustedReportsOnceAtTheSource(t *testing.T) {
	t.Parallel()
	at := provenance.Doc{}
	r := newExternalReads(sourceDocument{}, budget.AtMost(1))
	ok := func() answer { return answer{} }
	r.once("a", ok)
	assert.Empty(t, r.exhausted(at), "a budget spent exactly is not crossed")

	r.once("b", ok)
	r.once("c", ok)

	got := r.exhausted(at)
	require.Len(t, got, 1)
	assert.Equal(t, diag.BudgetExceeded, got[0].Code)
	assert.Equal(t, ir.SeverityError, got[0].Severity)
	assert.Equal(t, at.At(""), got[0].Provenance)
	assert.Equal(t, "external references name more documents than the 1-document budget; "+
		"the $refs into the rest are reported unresolved", got[0].Message)
	free := newExternalReads(sourceDocument{}, budget.Unbounded())
	for _, key := range []string{"a", "b", "c"} {
		free.once(key, ok)
	}
	assert.Empty(t, free.exhausted(at), "an unbounded budget is never crossed")
}

// TestExternalReads_IsSafeForConcurrentUse holds the record to what the readers
// sharing it promise: the resolver's interfaces do not rule out concurrent
// calls, and a write racing another is a fatal map fault that no recover can
// catch. Under -race, unsynchronized access fails here.
func TestExternalReads_IsSafeForConcurrentUse(t *testing.T) {
	t.Parallel()
	r := newExternalReads(sourceDocument{}, budget.Unbounded())
	for i := range 8 {
		key := fmt.Sprintf("http://host/doc%d.yaml", i)
		t.Run(fmt.Sprintf("doc%d", i), func(t *testing.T) {
			t.Parallel()
			tree := &yaml.Node{Kind: yaml.ScalarNode, Value: key}
			for range 100 {
				r.recordTree(key, tree, nil)
				got, ok := r.preparedFor(key)
				assert.True(t, ok)
				assert.Same(t, tree, got)
				a := r.once(key, func() answer { return answer{body: []byte(key)} })
				assert.Equal(t, key, string(a.body))
				r.adopt(key+"#again", key)
			}
		})
	}
}

// TestPreparedFor covers externalReads.preparedFor's four outcomes: a file key
// found directly, a URL key found by its request spelling, which is what
// external.document records a fetched tree under, a key http.NewRequest itself
// refuses, and a key nothing was ever recorded under.
func TestPreparedFor(t *testing.T) {
	t.Parallel()

	t.Run("a file key", func(t *testing.T) {
		t.Parallel()
		r := newExternalReads(sourceDocument{}, budget.Unbounded())
		tree := &yaml.Node{Kind: yaml.ScalarNode, Value: "x"}
		r.recordTree("/tmp/doc.yaml", tree, nil)

		got, ok := r.preparedFor("/tmp/doc.yaml")

		require.True(t, ok)
		assert.Same(t, tree, got)
	})

	t.Run("a file key http.NewRequest would respell", func(t *testing.T) {
		t.Parallel()
		r := newExternalReads(sourceDocument{}, budget.Unbounded())
		tree := &yaml.Node{Kind: yaml.ScalarNode, Value: "x"}
		r.recordTree("dir/with space/doc.yaml", tree, nil)

		got, ok := r.preparedFor("dir/with space/doc.yaml")

		require.True(t, ok, "the path is its own key; the request spelling %20 is not")
		assert.Same(t, tree, got)
	})

	t.Run("a URL key found by its request spelling", func(t *testing.T) {
		t.Parallel()
		r := newExternalReads(sourceDocument{}, budget.Unbounded())
		tree := &yaml.Node{Kind: yaml.ScalarNode, Value: "x"}
		// The spelling external.document records under: the request's own URL.
		req, err := http.NewRequest(http.MethodGet, "HTTP://host/doc.yaml", nil)
		require.NoError(t, err)
		r.recordTree(req.URL.String(), tree, nil)

		// The binder's name for it, spelled as the reference writes it.
		got, ok := r.preparedFor("HTTP://host/doc.yaml")

		require.True(t, ok)
		assert.Same(t, tree, got)
	})

	t.Run("a URL key spelled with an empty port", func(t *testing.T) {
		t.Parallel()
		r := newExternalReads(sourceDocument{}, budget.Unbounded())
		tree := &yaml.Node{Kind: yaml.ScalarNode, Value: "x"}
		// http.NewRequest drops the empty port, so external.document records it
		// without one.
		r.recordTree("http://host/doc.yaml", tree, nil)

		got, ok := r.preparedFor("http://host:/doc.yaml")

		require.True(t, ok)
		assert.Same(t, tree, got)
	})

	t.Run("a key http.NewRequest rejects", func(t *testing.T) {
		t.Parallel()
		r := newExternalReads(sourceDocument{}, budget.Unbounded())

		_, ok := r.preparedFor("http://host/\x00bad")

		assert.False(t, ok)
	})

	t.Run("a missing key", func(t *testing.T) {
		t.Parallel()
		r := newExternalReads(sourceDocument{}, budget.Unbounded())

		_, ok := r.preparedFor("/no/such/file.yaml")

		assert.False(t, ok)
	})
}
