// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package openapicases_test

import (
	"crypto/sha256"
	"encoding/json/jsontext"
	"slices"
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
	out := map[[2]int][][]int{}
	each, err := harness.Each(c)
	require.NoError(t, err)
	for _, at := range each {
		if pick(at) {
			out[[2]int{at[0], at[1]}] = append(out[[2]int{at[0], at[1]}], at)
		}
	}
	return out
}

// TestNew_EachCoreCaseCompilesWithNoError holds the schema cases to
// eachCoreCaseCompilesWithNoError.
func TestNew_EachCoreCaseCompilesWithNoError(t *testing.T) {
	t.Parallel()
	eachCoreCaseCompilesWithNoError(t, openapicases.New())
}

// TestGroups_EachCoreCaseCompilesWithNoError holds the test suite's groups to
// eachCoreCaseCompilesWithNoError: a group whose schema the compiler refuses
// would leave its instances unjudged.
func TestGroups_EachCoreCaseCompilesWithNoError(t *testing.T) {
	t.Parallel()
	eachCoreCaseCompilesWithNoError(t, openapicases.Groups())
}

// TestNew_EachValueCompilesAtALaterVersion holds the schema cases to
// eachValueCompilesAtALaterVersion.
func TestNew_EachValueCompilesAtALaterVersion(t *testing.T) {
	t.Parallel()
	eachValueCompilesAtALaterVersion(t, openapicases.New())
}

// TestGroups_EachValueCompilesAtALaterVersion holds the test suite's groups to
// eachValueCompilesAtALaterVersion.
func TestGroups_EachValueCompilesAtALaterVersion(t *testing.T) {
	t.Parallel()
	eachValueCompilesAtALaterVersion(t, openapicases.Groups())
}

// eachValueCompilesAtALaterVersion compiles, at each version of c the core
// leaves out, the first case writing each value of each other axis, as
// eachCoreCaseCompilesWithNoError compiles the core. The gate checks no other
// document of such a version, so one the version alone makes the compiler
// refuse fails here and not first in the nightly run, whose oracles decline
// it; one refused only where two values meet is that run's to find.
func eachValueCompilesAtALaterVersion(t *testing.T, c harness.Cases) {
	t.Helper()
	axes := c.Axes()
	seen := map[[3]int]bool{}
	first := map[[2]int][][]int{}
	each, err := harness.Each(c)
	require.NoError(t, err)
	for _, at := range each {
		if axes[0].Values[at[0]].Like == "" {
			continue
		}
		fresh := false
		for k := 1; k < len(at); k++ {
			fresh = fresh || !seen[[3]int{at[0], k, at[k]}]
		}
		if !fresh {
			continue
		}
		if _, ok := c.Write(at); !ok {
			continue
		}
		for k := 1; k < len(at); k++ {
			seen[[3]int{at[0], k, at[k]}] = true
		}
		first[[2]int{at[0], at[1]}] = append(first[[2]int{at[0], at[1]}], at)
	}
	require.NotEmpty(t, first, "a version the core leaves out")
	compilesWithNoError(t, c, first)
}

// eachCoreCaseCompilesWithNoError compiles each core case of c
// (compilesWithNoError).
func eachCoreCaseCompilesWithNoError(t *testing.T, c harness.Cases) {
	t.Helper()
	compilesWithNoError(t, c, shards(t, c, harness.Core(c)))
}

// compilesWithNoError compiles each case of byShard, cases of c by version
// and target, as the harness's first three oracles hold a spec: verified, so a
// broken invariant or a panic fails the run, and with no error diagnostic.
// Each case is a valid document of its version, so a case the compiler refuses
// is the writer's defect. An info or a warning stands: it is the compiler's
// account of a valid construct, which the case oracles judge.
func compilesWithNoError(t *testing.T, c harness.Cases, byShard map[[2]int][][]int) {
	t.Helper()
	axes := c.Axes()
	for key, cases := range byShard {
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

// TestNew_EachCaseWritesADocumentOfItsOwn holds the schema cases to
// eachCaseWritesADocumentOfItsOwn, each writing one source.
func TestNew_EachCaseWritesADocumentOfItsOwn(t *testing.T) {
	t.Parallel()
	eachCaseWritesADocumentOfItsOwn(t, openapicases.New(), 1, alone)
}

// TestGroups_EachCaseWritesADocumentOfItsOwn holds the test suite's groups to
// eachCaseWritesADocumentOfItsOwn, each writing a root and its group's schema
// at least.
func TestGroups_EachCaseWritesADocumentOfItsOwn(t *testing.T) {
	t.Parallel()
	eachCaseWritesADocumentOfItsOwn(t, openapicases.Groups(), 2, func([]int) bool { return true })
}

// alone admits a schema case writing no second construct, and one writing a
// second at the first position: a position places the unit it is handed and
// reads neither construct, so a second written alike at every position needs
// holding at one, and the walk does not grow by the product of the two axes.
func alone(at []int) bool { return at[3] == 0 || at[4] == 0 }

// eachCaseWritesADocumentOfItsOwn writes every case of c's full product pick
// admits: each document holds minSources sources or more, each JSON, is
// written alike twice, and no two cases write the same one, as a value Write
// misreads would make them; and each value of each axis is written by some
// case. Each case's name is its own because CaseCount accepts the axes.
func eachCaseWritesADocumentOfItsOwn(t *testing.T, c harness.Cases, minSources int, pick func([]int) bool) {
	t.Helper()
	axes := c.Axes()

	writers := map[[sha256.Size]byte][]int{}
	written := make([][]bool, len(axes))
	for k, a := range axes {
		written[k] = make([]bool, len(a.Values))
	}
	each, err := harness.Each(c)
	require.NoError(t, err)
	for _, at := range each {
		if !pick(at) {
			continue
		}
		doc, ok := c.Write(at)
		if !ok {
			continue
		}
		require.GreaterOrEqual(t, len(doc.Sources), minSources, "%v", at)
		h := sha256.New()
		for _, src := range doc.Sources {
			require.True(t, jsontext.Value(src.Data).IsValid(), "%v: %s", at, src.Path)
			h.Write([]byte(src.Path + "\x00" + string(src.Data) + "\x00"))
		}
		again, _ := c.Write(at)
		require.Equal(t, doc.Sources, again.Sources, "%v is written alike twice", at)
		sum := [sha256.Size]byte(h.Sum(nil))
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

// TestNew_TheAxesInTheirOrder pins the axes' order and the values a later
// version, a narrowed target, a second construct or an entry written first is
// recorded under.
func TestNew_TheAxesInTheirOrder(t *testing.T) {
	axes := openapicases.New().Axes()
	names := make([]string, len(axes))
	for i, a := range axes {
		names[i] = a.Name
	}
	assert.Equal(t, []string{"version", "target", "construct", "second", "position", "spelling"}, names)
	assert.Equal(t, []harness.Value{{Name: "3.0.3"}, {Name: "3.1.0"}, {Name: "3.2.0", Like: "3.1.0"}}, axes[0].Values)
	assert.Equal(t, harness.Value{Name: "none"}, axes[2].Values[0])
	assert.Equal(t, []harness.Value{{Name: "$ref"}, {Name: "$ref~"}}, axes[2].Values[1:3])
	assert.Equal(t, []harness.Value{{Name: "object"}, {Name: "string"}, {Name: "integer"}, {Name: "array"},
		{Name: "enum", Like: "string"}, {Name: "union"}, {Name: "formatted", Like: "string"},
		{Name: "map", Like: "object"}}, axes[1].Values)
	assert.Equal(t, harness.Value{Name: "none"}, axes[3].Values[0])
	for _, v := range axes[3].Values[1:] {
		assert.Equal(t, "none", v.Like, "a second construct, %s, is recorded under none", v.Name)
	}
	assert.Equal(t, []harness.Value{{Name: "inline"}, {Name: "beside"}, {Name: "wrap"}, {Name: "entry"},
		{Name: "entryFirst", Like: "entry"}, {Name: "nested"}, {Name: "besideEntry"}}, axes[5].Values)
	assert.Equal(t, []harness.Value{{Name: "component"}, {Name: "property"}, {Name: "items"}, {Name: "additionalProperties"},
		{Name: "branch"}, {Name: "anyOfBranch"}, {Name: "prefixItem"}, {Name: "parameter"}, {Name: "requestBody"}, {Name: "responseBody"},
		{Name: "header"}}, axes[4].Values)
}

func TestNew_WritesNothingOutsideTheProduct(t *testing.T) {
	c := openapicases.New()
	for _, at := range [][]int{nil, {0, 0, 0, 0, 0}, {0, 0, 0, 0, 0, 6}, {0, -1, 0, 0, 0, 0}} {
		_, ok := c.Write(at)
		assert.False(t, ok, "%v", at)
	}
	_, ok := c.Write([]int{0, 0, 0, 0, 0, 0})
	assert.True(t, ok, "the first case writes the object target at a component")
}

// TestRecord_ReadsEachCaseOraclesRecord holds each record to its file and
// every line's reason to Reasons, and the full record to holding each line of
// the core's.
func TestRecord_ReadsEachCaseOraclesRecord(t *testing.T) {
	for _, o := range harness.CaseOracles() {
		r, err := openapicases.Record(o)
		require.NoError(t, err, o)
		_, _, _, unreasoned := r.Judge(nil, openapicases.Reasons())
		assert.Empty(t, unreasoned, o)
		every, err := openapicases.FullRecord(o)
		require.NoError(t, err, o)
		for name := range r {
			assert.Contains(t, every, name, o)
		}
	}
	for _, read := range []func(harness.Oracle) (harness.Recorded, error){openapicases.Record, openapicases.FullRecord} {
		_, err := read(harness.OracleOrder)
		require.Error(t, err)
		assert.Contains(t, err.Error(), `oracle "order-invariance" keeps no record`)
	}
}

// TestNew_WritesAPositionOnlyAtTheVersionsItHolds pins the positions written
// from 3.1, whose keywords exist from that version: no case writes them at 3.0.
func TestNew_WritesAPositionOnlyAtTheVersionsItHolds(t *testing.T) {
	c := openapicases.New()
	axes := c.Axes()
	for i, p := range axes[4].Values {
		for v, version := range axes[0].Values {
			_, ok := c.Write([]int{v, 0, 0, 0, i, 0})
			assert.Equal(t, v > 0 || !slices.Contains(from31Positions, p.Name), ok, "%s at %s", p.Name, version.Name)
		}
	}
}
