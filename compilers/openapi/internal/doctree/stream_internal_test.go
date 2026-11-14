// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package doctree

import (
	"context"
	"io"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	yaml "go.yaml.in/yaml/v3"

	"github.com/dexpace/morphic/internal/ctxtest"
)

// TestHoldsContent_ReadsOnlyAWellFormedDocument pins the predicate the tail
// count turns on. yaml.v3 wraps exactly one root in every document it parses,
// so the shapes that fail the guard are built rather than decoded — the same
// reason TestUnmarshal_RejectsADocumentNodeHoldingMoreThanOneRoot builds its
// node — and a document that holds nothing is the null its separator decodes
// to, whatever the spelling.
func TestHoldsContent_ReadsOnlyAWellFormedDocument(t *testing.T) {
	t.Parallel()
	wrap := func(root *yaml.Node) *yaml.Node {
		return &yaml.Node{Kind: yaml.DocumentNode, Content: []*yaml.Node{root}}
	}
	mapping := &yaml.Node{Kind: yaml.MappingNode, Tag: "!!map"}

	assert.True(t, holdsContent(wrap(mapping)))
	assert.True(t, holdsContent(wrap(&yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: "text"})))
	assert.False(t, holdsContent(wrap(&yaml.Node{Kind: yaml.ScalarNode, Tag: "!!null"})), "an explicit null")
	assert.False(t, holdsContent(wrap(&yaml.Node{Kind: yaml.ScalarNode})),
		"an untagged empty scalar resolves to null, which is what a bare separator decodes to")
	assert.False(t, holdsContent(mapping), "not a document node")
	assert.False(t, holdsContent(&yaml.Node{Kind: yaml.DocumentNode, Content: []*yaml.Node{mapping, mapping}}),
		"a document node yaml.v3 never produces")
}

// TestContextReader_FailsOnceTheContextIsDone pins the reader every decode goes
// through: it reads while its context is live, and once it is done fails with
// the context's error without reading.
func TestContextReader_FailsOnceTheContextIsDone(t *testing.T) {
	t.Parallel()
	under := strings.NewReader("abcdef")
	r := contextReader{ctx: &ctxtest.Countdown{Context: t.Context(), Left: 1}, r: under}
	buf := make([]byte, 3)

	n, err := r.Read(buf)
	require.NoError(t, err)
	assert.Equal(t, "abc", string(buf[:n]))

	n, err = r.Read(buf)
	require.ErrorIs(t, err, context.Canceled)
	assert.Zero(t, n)
	assert.Equal(t, 3, under.Len(), "nothing more was read")
}

// TestContextReader_ReadsAStreamWholeThroughALiveContext pins that reading through the
// context changes nothing a live one parses: the reader ends the stream as the
// bytes do.
func TestContextReader_ReadsAStreamWholeThroughALiveContext(t *testing.T) {
	t.Parallel()
	const text = "openapi: 3.1.0\ninfo: {title: t, version: \"1\"}\npaths: {}\n"
	r := contextReader{ctx: t.Context(), r: strings.NewReader(text)}

	got, err := io.ReadAll(r)

	require.NoError(t, err)
	assert.Equal(t, text, string(got))
}
