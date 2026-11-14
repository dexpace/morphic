// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package irverify

import (
	"io/fs"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dexpace/morphic/ir"
	"github.com/dexpace/morphic/ir/irtest"
)

// cutVerifyPoints is how many parts the sweep cuts each document's walk into:
// it cuts at each boundary between two.
const cutVerifyPoints = 16

// cuttingVisitor shows inner the values a walk reaches until left runs out, and
// then none, recording in cut that one went unshown; it tells inner of the
// cycles the walk meets while inner is shown values.
type cuttingVisitor struct {
	inner ir.Visitor
	left  int
	cut   *bool
}

var _ ir.CycleVisitor = (*cuttingVisitor)(nil)

// Visit shows v to the inner visitor while the limit lasts.
func (c *cuttingVisitor) Visit(v reflect.Value, at ir.WalkPath) bool {
	if c.left == 0 {
		*c.cut = true
		return false
	}
	c.left--
	return c.inner.Visit(v, at)
}

// Cycle tells the inner visitor of a cycle, if it asks to be told.
func (c *cuttingVisitor) Cycle(at, entered ir.WalkPath, ref reflect.Type) {
	if cycles, asks := c.inner.(ir.CycleVisitor); asks {
		cycles.Cycle(at, entered, ref)
	}
}

// cutAt returns a walk that shows each visitor at most n values, as a walk cut
// short by the limit does, and says it stopped once one went unshown.
func cutAt(n int) walkFunc {
	return func(root any, path string, visitors ...ir.Visitor) bool {
		cut := false
		held := make([]ir.Visitor, len(visitors))
		for i, v := range visitors {
			held[i] = &cuttingVisitor{inner: v, left: n, cut: &cut}
		}
		return ir.WalkValues(root, path, held...) || cut
	}
}

// violationKeys counts vs by code and path, leaving out ir/walk-truncated.
func violationKeys(vs []Violation) map[string]int {
	keys := map[string]int{}
	for _, v := range vs {
		if v.Code != "ir/walk-truncated" {
			keys[v.Code+" at "+v.Path]++
		}
	}
	return keys
}

// TestVerify_ACutWalkClaimsNothingTheWholeRunLacks cuts the walk of every IR
// golden under testdata at points spread over what it shows a visitor, and
// holds each cut run to the violations the whole run has, beside the one
// ir/walk-truncated: a check settling against what a stopped walk collected
// would claim an absence the document does not have.
func TestVerify_ACutWalkClaimsNothingTheWholeRunLacks(t *testing.T) {
	t.Parallel()
	var docs int
	err := filepath.WalkDir("../../testdata", func(path string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() || !strings.HasSuffix(path, ".golden.json") {
			return err
		}
		doc, err := irtest.ReadGolden(path)
		if err != nil {
			return err
		}
		docs++
		have, shown := violationKeys(Verify(doc)), shownAll(doc)
		for k := 1; k < cutVerifyPoints; k++ {
			n := shown * k / cutVerifyPoints
			for key, times := range violationKeys(verify(doc, cutAt(n))) {
				assert.LessOrEqual(t, times, have[key], "%s cut at %d of %d values: %s", path, n, shown, key)
			}
		}
		return nil
	})
	require.NoError(t, err)
	require.Positive(t, docs, "the sweep reads the IR goldens under testdata")
}

// shownAll returns how many values a walk of root shows a visitor that prunes
// nothing, the most any visitor of it is shown.
func shownAll(root any) int {
	var n int
	ir.WalkValues(root, ir.DocumentPath, ir.VisitFunc(func(reflect.Value, ir.WalkPath) bool { n++; return true }))
	return n
}

// TestVerify_ACutWalkClaimsNoCallbackUnlisted holds the callback check to
// claiming nothing after a stop: the operation marked a callback comes before
// the one listing it, so a walk cut between the two has read the mark and not
// the listing. Every cut point is tried, beside the whole walk's agreement.
func TestVerify_ACutWalkClaimsNoCallbackUnlisted(t *testing.T) {
	t.Parallel()
	marked := ir.Operation{ID: "op/x/S/cb", Bindings: ir.OpBindings{HTTP: []ir.HTTPBinding{{
		Inbound: ir.HTTPInboundCallback, URITemplate: "{$url}",
	}}}}
	listing := ir.Operation{ID: "op/x/S/parent", Bindings: ir.OpBindings{HTTP: []ir.HTTPBinding{{
		URITemplate: "/sub", Callbacks: []ir.Callback{{Name: "on", Expression: "{$url}", Operations: []ir.OpID{marked.ID}}},
	}}}}
	doc := &ir.Document{IRVersion: ir.IRVersion, Services: []ir.Service{{ID: "s/x/S", Groups: []ir.OperationGroup{{
		ID: "g/x/S/g", Operations: []ir.Operation{marked, listing},
	}}}}}
	require.NotContains(t, violationKeys(Verify(doc)), "ir/callback-unlisted at doc.Services[0].Groups[0].Operations[0].Bindings.HTTP",
		"the control: the whole walk reads both halves")

	shown := shownAll(doc)
	for n := range shown {
		for key := range violationKeys(verify(doc, cutAt(n))) {
			assert.False(t, strings.HasPrefix(key, "ir/callback-"), "cut at %d of %d values: %s", n, shown, key)
		}
	}
}
