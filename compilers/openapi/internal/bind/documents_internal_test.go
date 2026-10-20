package bind

import (
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
