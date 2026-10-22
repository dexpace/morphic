package operation

import (
	"testing"

	soa "github.com/speakeasy-api/openapi/openapi"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestFirstPathSegment_Empty(t *testing.T) {
	t.Parallel()
	assert.Empty(t, firstPathSegment("/"))
	assert.Equal(t, "users", firstPathSegment("/users/{id}"))
}

func TestLowerTagDefs_NilEntrySkipped(t *testing.T) {
	t.Parallel()
	l := newRawLowerer(&soa.OpenAPI{Tags: []*soa.Tag{nil, {}}})
	assert.Len(t, lowerTagDefs(l.ctx), 1, "nil tag entry skipped")
}

// TestIndexTags_SkipsANilEntry pins the guard on the tag list: a nil entry
// indexes no name, so it can neither be read for a kind or a parent by the
// grouping walk nor shadow a tag the document does declare. The parser produces
// no such entry, which is why the guard is exercised at the list it guards.
func TestIndexTags_SkipsANilEntry(t *testing.T) {
	t.Parallel()
	kept := &soa.Tag{Name: "kept"}

	got := indexTags([]*soa.Tag{nil, kept})

	require.Len(t, got, 1)
	assert.Same(t, kept, got["kept"])
}

// TestTagIndex_ChainAgreesWithEveryAncestor pins what makes a group's parent
// independent of which operation creates it: the chain of every tag a chain
// passes through is that chain's own prefix. The tags hold a cycle (a and b), a
// tag beneath it (c), one beneath that (d), and one whose parent is a badge (e).
func TestTagIndex_ChainAgreesWithEveryAncestor(t *testing.T) {
	t.Parallel()
	tag := func(name, parent, kind string) *soa.Tag {
		return &soa.Tag{Name: name, Parent: &parent, Kind: &kind}
	}
	x := indexTags([]*soa.Tag{
		tag("a", "b", "nav"), tag("b", "a", "nav"), tag("c", "a", "nav"),
		tag("d", "c", ""), tag("badge", "", "badge"), tag("e", "badge", "nav"),
	})
	want := map[string][]string{
		"a": {"a"}, "b": {"b"}, "c": {"a", "c"}, "d": {"a", "c", "d"},
		"e": {"e"}, "undeclared": {"undeclared"},
	}
	for name, chain := range want {
		got := x.chain(name)
		assert.Equal(t, chain, got, "chain of %s", name)
		for i := range got {
			assert.Equal(t, got[:i+1], x.chain(got[i]), "%s's ancestor %s agrees", name, got[i])
		}
	}
}
