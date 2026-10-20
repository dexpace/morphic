package bind

import (
	"encoding/json/jsontext"
	"math"
	"runtime"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	yaml "gopkg.in/yaml.v3"
)

// TestDocs_NodeIsNilWhereTheDocumentHoldsNone pins node's answer for a
// position its document does not hold: nil, never the last node a walk
// reached on the way.
func TestDocs_NodeIsNilWhereTheDocumentHoldsNone(t *testing.T) {
	t.Parallel()
	var root yaml.Node
	require.NoError(t, yaml.Unmarshal([]byte("a: {b: 1}\n"), &root))
	d := newDocs(&root, Config{Self: "spec.yaml"}, &budget{limit: DefaultLimit})

	assert.Nil(t, d.node(Position{Pointer: "/a/c"}))
	require.NotNil(t, d.node(Position{Pointer: "/a/b"}))
	assert.Equal(t, "1", d.node(Position{Pointer: "/a/b"}).Value)
}

// TestDocs_ReadCostsItsPointersLength pins that a read spells the position it
// reaches once: a pointer twice as deep allocates about twice the bytes, where
// extending the position a token at a time copied it at each token and cost
// four times. It is not parallel, as the count is the process's, and takes the
// least of three counts, since anything else allocating only adds to one.
func TestDocs_ReadCostsItsPointersLength(t *testing.T) {
	spine := func(levels int) (*docs, jsontext.Pointer) {
		var root yaml.Node
		src := "a: " + strings.Repeat("{p: ", levels) + "{}" + strings.Repeat("}", levels) + "\n"
		require.NoError(t, yaml.Unmarshal([]byte(src), &root))
		return newDocs(&root, Config{Self: "spec.yaml"}, &budget{limit: DefaultLimit}),
			jsontext.Pointer("/a" + strings.Repeat("/p", levels))
	}
	cost := func(levels int) uint64 {
		d, pointer := spine(levels)
		r, reason := d.read(Source, pointer)
		require.Equal(t, NoReason, reason)
		require.Equal(t, pointer, r.at.Pointer)
		var stats runtime.MemStats
		least := uint64(math.MaxUint64)
		for range 3 {
			runtime.ReadMemStats(&stats)
			before := stats.TotalAlloc
			for range 10 {
				_, _ = d.read(Source, pointer)
			}
			runtime.ReadMemStats(&stats)
			least = min(least, stats.TotalAlloc-before)
		}
		return least
	}
	assert.Less(t, cost(800), 3*cost(400), "twice the depth, about twice the bytes")
}

// TestPrefix_SlicesItsPointer pins prefix: the pointer to the first n tokens,
// cut from the pointer itself, so it allocates nothing however long it is.
func TestPrefix_SlicesItsPointer(t *testing.T) {
	pointer := jsontext.Pointer("/a~1b/c/~0d/e")
	for n, want := range []string{"", "/a~1b", "/a~1b/c", "/a~1b/c/~0d", "/a~1b/c/~0d/e", "/a~1b/c/~0d/e"} {
		assert.Equal(t, want, prefix(pointer, n), n)
	}
	long := jsontext.Pointer(strings.Repeat("/p", 1000))
	assert.Zero(t, testing.AllocsPerRun(10, func() { _ = prefix(long, 500) }))
}
