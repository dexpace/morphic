package canary_test

import (
	"bytes"
	"context"
	"fmt"
	"strings"
	"testing"

	"github.com/speakeasy-api/openapi/overlay"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	yaml "go.yaml.in/yaml/v3"
)

// applied is what applyOverlay reports for an overlay it applies.
const applied = "applied"

// overlayOf returns an overlay document whose one action updates the target
// document's info object with update, written as one line of YAML.
func overlayOf(update string) string {
	return "overlay: 1.0.0\ninfo: {title: o, version: '1'}\nactions:\n" +
		"  - target: $.info\n    update: " + update + "\n"
}

// applyOverlay applies the overlay input to head, as the compiler applies one
// to a source's tree: applied, or what failed.
func applyOverlay(_ context.Context, input []byte) string {
	_, err := applyTo(input)
	if err != nil {
		return err.Error()
	}
	return applied
}

// applyTo parses the overlay input and applies it to head's tree, which it
// returns.
func applyTo(input []byte) (*yaml.Node, error) {
	o, err := overlay.ParseReader(bytes.NewReader(input))
	if err != nil {
		return nil, err
	}
	var root yaml.Node
	if err := yaml.Unmarshal([]byte(head), &root); err != nil {
		return nil, err
	}
	return &root, o.ApplyTo(&root)
}

// overlayAliasClone pins overlay-alias-clone: applying an update clones it
// through its aliases with no record of where it has been, so an anchor
// naming its own ancestor overflows the stack and an alias bomb runs memory
// out, and a cloned alias names a copy of its target outside the tree.
func overlayAliasClone(t *testing.T) {
	t.Run("an anchor naming its ancestor", func(t *testing.T) {
		t.Parallel()
		r := probe(t, "overlay", overlayOf("{x-a: &a [*a]}"))
		assert.Equal(t, overflowed, r.outcome, revisit(t, r))
	})
	t.Run("aliases multiplying", func(t *testing.T) {
		t.Parallel()
		levels := []string{"x-0: &l0 [a, a, a, a, a, a, a, a, a, a]"}
		for i := 1; i <= bombLevels; i++ {
			prev := fmt.Sprintf("*l%d", i-1)
			levels = append(levels, fmt.Sprintf("x-%d: &l%d [%s]", i, i, strings.Repeat(prev+", ", 9)+prev))
		}
		r := probe(t, "overlay", overlayOf("{"+strings.Join(levels, ", ")+"}"))
		assert.Equal(t, exhausted, r.outcome, revisit(t, r))
	})
	t.Run("an alias names a copy", func(t *testing.T) {
		t.Parallel()
		root, err := applyTo([]byte(overlayOf("{x-a: &n {k: v}, x-b: *n}")))
		require.NoError(t, err, revisit(t, result{}))
		info := valueOf(root.Content[0], "info")
		anchored, alias := valueOf(info, "x-a"), valueOf(info, "x-b")
		require.NotNil(t, anchored, revisit(t, result{}))
		require.NotNil(t, alias, revisit(t, result{}))
		require.Equal(t, yaml.AliasNode, alias.Kind, revisit(t, result{}))
		assert.NotSame(t, anchored, alias.Alias, "the alias names a copy: %s", revisit(t, result{}))
		assert.False(t, inTree(root, alias.Alias), "the copy is outside the tree: %s", revisit(t, result{}))
	})
}

// inTree reports whether n is a node of the tree under root, as its Content
// lists reach it.
func inTree(root, n *yaml.Node) bool {
	stack := []*yaml.Node{root}
	for len(stack) > 0 {
		top := stack[len(stack)-1]
		stack = stack[:len(stack)-1]
		if top == n {
			return true
		}
		stack = append(stack, top.Content...)
	}
	return false
}

// bombLevels is how many times the aliases of the alias bomb multiply its
// first sequence, tenfold each time.
const bombLevels = 8

// valueOf returns the value mapping m holds under key, or nil.
func valueOf(m *yaml.Node, key string) *yaml.Node {
	if m == nil {
		return nil
	}
	for i := 0; i+1 < len(m.Content); i += 2 {
		if m.Content[i].Value == key {
			return m.Content[i+1]
		}
	}
	return nil
}
