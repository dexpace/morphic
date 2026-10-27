package load

import (
	"errors"
	"fmt"
	"sync"
	"sync/atomic"
	"testing"

	soa "github.com/speakeasy-api/openapi/openapi"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	yaml "go.yaml.in/yaml/v3"

	"github.com/dexpace/morphic/compilers/openapi/internal/budget"
	"github.com/dexpace/morphic/compilers/openapi/internal/diag"
	"github.com/dexpace/morphic/compilers/openapi/internal/overlay"
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
	at := pointerAt(0, overlay.Origin{})
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
	assert.Equal(t, at(""), got[0].Provenance)
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
			doc := &soa.OpenAPI{}
			doc.InitCache()
			for range 100 {
				r.recordTree(key, tree, nil)
				got, ok := r.preparedFor(key)
				assert.True(t, ok)
				assert.Same(t, tree, got)
				assert.True(t, r.prepared(tree))
				a := r.once(key, func() answer { return answer{body: []byte(key)} })
				assert.Equal(t, key, string(a.body))
				r.handOver(doc, nil)
			}
		})
	}
}
