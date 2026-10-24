package sourceindex

import (
	"testing"

	"github.com/stretchr/testify/require"
	yaml "gopkg.in/yaml.v3"
)

// Decode is the one place these tests parse source text, so a fixture written as
// YAML is indexed over exactly the tree a compile would index.
func Decode(t *testing.T, src string) *yaml.Node {
	t.Helper()
	var root yaml.Node
	require.NoError(t, yaml.Unmarshal([]byte(src), &root))
	return &root
}

// MaxTrackedDepth is maxTrackedDepth, for the external tests.
const MaxTrackedDepth = maxTrackedDepth
