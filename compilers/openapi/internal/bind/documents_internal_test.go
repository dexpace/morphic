// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package bind

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	yaml "go.yaml.in/yaml/v3"

	"github.com/dexpace/morphic/compilers/openapi/internal/budget"
)

// TestDocs_NodeIsNilWhereTheDocumentHoldsNone pins node's answer for a
// position its document does not hold: nil, never the last node a walk
// reached on the way.
func TestDocs_NodeIsNilWhereTheDocumentHoldsNone(t *testing.T) {
	t.Parallel()
	var root yaml.Node
	require.NoError(t, yaml.Unmarshal([]byte("a: {b: 1}\n"), &root))
	d := newDocs(&root, Config{Self: "spec.yaml"}, &work{ctx: t.Context(), steps: budget.New(budget.AtMost(MaxSteps))})

	assert.Nil(t, d.node(DocumentPointer{Pointer: "/a/c"}))
	require.NotNil(t, d.node(DocumentPointer{Pointer: "/a/b"}))
	assert.Equal(t, "1", d.node(DocumentPointer{Pointer: "/a/b"}).Value)
}

// TestReads_ARefusalToOneAskerIsNoAnswerOfTheDocument pins that a read
// refused to its asker alone is kept for that asker and never as the
// document's answer, so another asker that may read it still does, in the
// order the binding's own drains never produce: the refused asker first.
func TestReads_ARefusalToOneAskerIsNoAnswerOfTheDocument(t *testing.T) {
	t.Parallel()
	var root, other yaml.Node
	require.NoError(t, yaml.Unmarshal([]byte("a: 1\n"), &root))
	require.NoError(t, yaml.Unmarshal([]byte("b: 2\n"), &other))
	opens := 0
	cfg := Config{Self: "spec.yaml", Open: func(_, from string) (*yaml.Node, error) {
		opens++
		if from == "https://pub.example/evil.yaml" {
			return nil, &BarredError{Why: "refused to it"}
		}
		return &other, nil
	}}
	d := newDocs(&root, cfg, &work{ctx: t.Context(), steps: budget.New(budget.AtMost(MaxSteps))})
	r := newReads()

	_, err := r.open(d, "http://10.0.0.5/s.yaml", "https://pub.example/evil.yaml")
	require.EqualError(t, err, "refused to it")
	_, err = r.open(d, "http://10.0.0.5/s.yaml", "https://pub.example/evil.yaml")
	require.EqualError(t, err, "refused to it", "kept for the asker")
	assert.False(t, r.unread(d, "http://10.0.0.5/s.yaml", "https://pub.example/evil.yaml"))
	assert.True(t, r.unread(d, "http://10.0.0.5/s.yaml", "spec.yaml"), "another asker has not read it")

	id, err := r.open(d, "http://10.0.0.5/s.yaml", "spec.yaml")
	require.NoError(t, err)
	assert.Equal(t, DocID(1), id)
	assert.Equal(t, 2, opens)
}

// TestDocs_JudgeWithNoOpenRefuses pins judge's precondition, which no binding
// reaches: with no Config.Open no document is read before, and asked anyway
// it refuses rather than admit or call nothing.
func TestDocs_JudgeWithNoOpenRefuses(t *testing.T) {
	t.Parallel()
	var src yaml.Node
	require.NoError(t, yaml.Unmarshal([]byte("openapi: 3.1.0\n"), &src))
	cfg := Config{Self: "https://x.test/spec.yaml"}
	d := newDocs(&src, cfg, &work{ctx: t.Context(), steps: budget.New(cfg.Limit)})
	assert.ErrorIs(t, d.judge("https://x.test/other.yaml", cfg.Self), errNotRead)
}
