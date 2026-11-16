// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package openapicases_test

import (
	"crypto/sha256"
	"encoding/json/jsontext"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dexpace/morphic/engine"
	"github.com/dexpace/morphic/internal/harness"
	"github.com/dexpace/morphic/internal/harness/openapicases"
	"github.com/dexpace/morphic/ir"
)

// shards returns the coordinates of every case of c pick admits, by the
// case's version and target.
func shards(t *testing.T, c harness.Cases, pick func([]int) bool) map[[2]int][][]int {
	t.Helper()
	n, err := harness.CaseCount(c)
	require.NoError(t, err)
	out := map[[2]int][][]int{}
	for i := range n {
		at, err := harness.Coordinates(c, i)
		require.NoError(t, err)
		if pick(at) {
			out[[2]int{at[0], at[1]}] = append(out[[2]int{at[0], at[1]}], at)
		}
	}
	return out
}

// TestNew_EveryDocumentCompilesClean compiles each core case as the harness's
// first three oracles hold a spec: verified, so a broken invariant or a panic
// fails the run, and with no error diagnostic. Each case is a valid document
// of its version, so a case the compiler refuses is the writer's defect.
func TestNew_EveryDocumentCompilesClean(t *testing.T) {
	t.Parallel()
	c := openapicases.New()
	axes := c.Axes()
	for key, cases := range shards(t, c, harness.Core(c)) {
		name := axes[0].Values[key[0]].Name + "/" + axes[1].Values[key[1]].Name
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			eng, err := engine.New(engine.WithCompilers(engine.Compilers()...), engine.WithPasses(engine.Passes()...))
			require.NoError(t, err)
			for _, at := range cases {
				doc, ok := c.Write(at)
				if !ok {
					continue
				}
				res, err := eng.RunSources(t.Context(), doc.Sources,
					engine.RunOptions{Verify: true, CompilerOptions: doc.Options})
				if !assert.NoError(t, err, "%v", at) {
					continue
				}
				if first, failed := ir.FirstError(res.Diagnostics); failed {
					t.Errorf("%v: %s: %s\n%s", at, first.Code, first.Message, doc.Sources[0].Data)
				}
			}
		})
	}
}

// TestNew_EachCaseWritesADocumentOfItsOwn writes every case of the full
// product: each document is one JSON source, written alike twice, and no two
// cases write the same one, as a value Write misreads would make them; and
// each value of each axis is written by some case. Each case's name is its
// own because CaseCount accepts the axes.
func TestNew_EachCaseWritesADocumentOfItsOwn(t *testing.T) {
	t.Parallel()
	c := openapicases.New()
	axes := c.Axes()
	n, err := harness.CaseCount(c)
	require.NoError(t, err)

	writers := map[[sha256.Size]byte][]int{}
	written := make([][]bool, len(axes))
	for k, a := range axes {
		written[k] = make([]bool, len(a.Values))
	}
	for i := range n {
		at, err := harness.Coordinates(c, i)
		require.NoError(t, err)
		doc, ok := c.Write(at)
		if !ok {
			continue
		}
		require.Len(t, doc.Sources, 1, "%v", at)
		data := doc.Sources[0].Data
		require.True(t, jsontext.Value(data).IsValid(), "%v", at)
		again, _ := c.Write(at)
		require.Equal(t, string(data), string(again.Sources[0].Data), "%v is written alike twice", at)
		sum := sha256.Sum256(data)
		first, dup := writers[sum]
		require.False(t, dup, "%v and %v write one document", first, at)
		writers[sum] = at
		for k, v := range at {
			written[k][v] = true
		}
	}
	for k, a := range axes {
		for v, ok := range written[k] {
			assert.True(t, ok, "no case writes %s %s", a.Name, a.Values[v].Name)
		}
	}
}

// TestNew_TheAxesAreTheProducts pins the axes' order and the values a later
// version or construct is recorded under.
func TestNew_TheAxesAreTheProducts(t *testing.T) {
	axes := openapicases.New().Axes()
	names := make([]string, len(axes))
	for i, a := range axes {
		names[i] = a.Name
	}
	assert.Equal(t, []string{"version", "target", "construct", "second", "position", "spelling"}, names)
	assert.Equal(t, []harness.Value{{Name: "3.0.3"}, {Name: "3.1.0"}, {Name: "3.2.0", Like: "3.1.0"}}, axes[0].Values)
	assert.Equal(t, harness.Value{Name: "none"}, axes[2].Values[0])
	assert.Equal(t, []harness.Value{{Name: "$ref"}, {Name: "$ref~"}}, axes[2].Values[1:3])
	assert.Len(t, axes[1].Values, 8)
	assert.Len(t, axes[4].Values, 9)
}

func TestNew_WritesNothingOutsideTheProduct(t *testing.T) {
	c := openapicases.New()
	for _, at := range [][]int{nil, {0, 0, 0, 0, 0}, {0, 0, 0, 0, 0, 1}, {0, -1, 0, 0, 0, 0}} {
		_, ok := c.Write(at)
		assert.False(t, ok, "%v", at)
	}
	_, ok := c.Write([]int{0, 0, 0, 0, 0, 0})
	assert.True(t, ok, "the first case writes the object target at a component")
}

// TestRecord_ReadsEachCaseOraclesRecord holds each record to its file and
// every line's reason to Reasons.
func TestRecord_ReadsEachCaseOraclesRecord(t *testing.T) {
	for _, o := range harness.CaseOracles() {
		r, err := openapicases.Record(o)
		require.NoError(t, err, o)
		_, _, _, unreasoned := r.Judge(nil, openapicases.Reasons())
		assert.Empty(t, unreasoned, o)
	}
	_, err := openapicases.Record(harness.OracleOrder)
	require.Error(t, err)
	assert.Contains(t, err.Error(), `oracle "order-invariance" keeps no record`)
}
