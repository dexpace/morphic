// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package overlay_test

import (
	"testing"

	"github.com/google/go-cmp/cmp"
	"github.com/stretchr/testify/require"
	yaml "go.yaml.in/yaml/v3"

	"github.com/dexpace/morphic/compilers/openapi/internal/budget"
	"github.com/dexpace/morphic/compilers/openapi/internal/doctree"
	"github.com/dexpace/morphic/compilers/openapi/internal/overlay"
	"github.com/dexpace/morphic/ir"
)

// overlayIndex is the index the tests hand Apply as the overlay's place in
// Document.Sources. It is non-zero so that WriterAt's answer for a position the
// overlay wrote cannot be the 0 it gives for one the overlay left alone.
const overlayIndex = 1

// v10 and v11 open an overlay document of each version the compiler applies,
// before its actions.
const (
	v10 = "overlay: 1.0.0\ninfo: {title: O, version: \"1\"}\nactions:\n"
	v11 = "overlay: 1.1.0\ninfo: {title: O, version: \"1\"}\nactions:\n"
)

// decoded parses src into a fresh node tree. Every case gets its own, because
// Apply mutates the tree it is handed.
func decoded(t *testing.T, src string) *yaml.Node {
	t.Helper()
	var root yaml.Node
	require.NoError(t, yaml.Unmarshal([]byte(src), &root))
	return &root
}

// applied overlays ov onto a fresh tree of src, returning the attribution, the
// tree the overlay left and what it reported.
func applied(t *testing.T, src, ov string, lax bool) (overlay.Origin, *yaml.Node, []ir.Diagnostic) {
	t.Helper()
	root := decoded(t, src)
	origin, diags := overlay.Apply(t.Context(), overlayIndex, root, overlayDoc(t, ov), overlay.Options{Path: "o.yaml", Data: []byte(ov), Lax: lax})
	return origin, root, diags
}

// valueOf decodes the tree under root, so a test compares what the overlay
// left as data rather than as YAML text.
func valueOf(t *testing.T, root *yaml.Node) any {
	t.Helper()
	var v any
	require.NoError(t, root.Decode(&v))
	return v
}

// sameValue requires the tree under root to hold the value want writes as
// YAML.
func sameValue(t *testing.T, want string, root *yaml.Node) {
	t.Helper()
	if d := cmp.Diff(valueOf(t, decoded(t, want)), valueOf(t, root)); d != "" {
		t.Errorf("the overlay left another tree (-want +got):\n%s", d)
	}
}

// codes returns each diagnostic's code and severity, in order, nil for none.
func codes(diags []ir.Diagnostic) []string {
	if len(diags) == 0 {
		return nil
	}
	out := make([]string, 0, len(diags))
	for _, d := range diags {
		out = append(out, string(d.Severity)+" "+d.Code)
	}
	return out
}

// sameCodes requires diags to carry want's codes and severities, in order.
func sameCodes(t *testing.T, want []string, diags []ir.Diagnostic) {
	t.Helper()
	if d := cmp.Diff(want, codes(diags)); d != "" {
		t.Errorf("another report (-want +got):\n%s\n%+v", d, diags)
	}
}

// member returns the key and value nodes of a mapping member, reached from the
// document root along a path of mapping keys.
func member(t *testing.T, root *yaml.Node, keys ...string) (key, value *yaml.Node) {
	t.Helper()
	n := root.Content[0]
	for _, want := range keys {
		found := false
		for i := 0; i+1 < len(n.Content) && !found; i += 2 {
			if n.Content[i].Value == want {
				key, value, found = n.Content[i], n.Content[i+1], true
			}
		}
		require.True(t, found, "no %q under the path %v", want, keys)
		n = value
	}
	return key, value
}

// overlayDoc reads ov as the compile reads an overlay document, into the tree
// overlay.Apply is handed.
func overlayDoc(t testing.TB, ov string) *yaml.Node {
	t.Helper()
	doc, err := doctree.Read(t.Context(), []byte(ov), budget.Unbounded())
	require.NoError(t, err)
	return doc.Root
}
