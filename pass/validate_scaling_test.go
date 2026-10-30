// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package pass_test

import (
	"fmt"
	"math"
	"runtime"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dexpace/morphic/ir"
	"github.com/dexpace/morphic/pass"
)

// chainedDiscriminators is the document of GitHub #850: n+1 models chained by
// Base, and n bases, each routing a discriminator mapping to the bottom of the
// chain. Validate asks, for every base, whether that model is its subtype, and
// the answer is no.
func chainedDiscriminators(n int) *ir.Document {
	chain := func(i int) ir.TypeID { return ir.TypeID(fmt.Sprintf("t/c%05d", i)) }
	types := ir.TypeRegistry{chain(0): &ir.Model{ID: chain(0)}}
	for i := 1; i <= n; i++ {
		types[chain(i)] = &ir.Model{ID: chain(i), Base: &ir.TypeRef{Target: chain(i - 1)}}
	}
	for k := range n {
		id := ir.TypeID(fmt.Sprintf("t/m%05d", k))
		types[id] = &ir.Model{ID: id, Discriminator: &ir.Discriminator{
			Mapping: map[string]ir.TypeID{"x": chain(n)},
		}}
	}
	return &ir.Document{IRVersion: ir.IRVersion, Name: "t", Version: "1", Types: types}
}

// spread is how many sizes costAround averages over across one doubling.
const spread = 8

// costAround returns the allocations and bytes one Validate of build's document
// makes, averaged over spread sizes spread evenly across the doubling from n,
// after holding each document to holds.
//
// Maps double and slices grow by about a quarter as they fill, so the bytes one
// size allocates step up wherever a table or a backing array happens to grow.
// Averaged across a whole doubling the steps cancel, rather than one falling
// between two of the sizes a test compares and reading as superlinear growth.
func costAround(t *testing.T, n int, build func(n int) *ir.Document,
	holds func(t *testing.T, size int, diags []ir.Diagnostic),
) (allocs, bytes float64) {
	t.Helper()
	for j := range spread {
		size := int(math.Round(float64(n) * math.Exp2(float64(j)/spread)))
		doc := build(size)
		holds(t, size, validated(t, doc))
		a, b := validateCost(t, doc)
		allocs += a / spread
		bytes += b / spread
	}
	return allocs, bytes
}

// TestValidate_DiscriminatorRoutesCostLinearWork holds the work of validating
// #850's document to linear growth, counted in allocations and in bytes rather
// than timed, around three sizes (see costAround): the growth of each from 2n
// to 4n is at most 2.5 times its growth from n to 2n, so fixed costs cancel,
// linear work gives 2 and a walk of the chain per route 4. Bytes catch a walk
// that reuses its buffers, which allocates few times for much work. It does not
// run in parallel, since both counts are process-wide.
func TestValidate_DiscriminatorRoutesCostLinearWork(t *testing.T) {
	var allocs, bytes [3]float64
	for k, n := range []int{250, 500, 1000} {
		allocs[k], bytes[k] = costAround(t, n, chainedDiscriminators, func(t *testing.T, size int, diags []ir.Diagnostic) {
			var rejected int
			for _, d := range diags {
				if d.Code == "pass/discriminator-missing-variant" {
					rejected++
				}
			}
			require.Equal(t, size, rejected, "every base's route must reach the subtype question, or the count compares nothing")
		})
	}
	assert.LessOrEqual(t, allocs[2]-allocs[1], 2.5*(allocs[1]-allocs[0]),
		"allocations around 250, 500, 1000: %v", allocs)
	assert.LessOrEqual(t, bytes[2]-bytes[1], 2.5*(bytes[1]-bytes[0]),
		"bytes around 250, 500, 1000: %v", bytes)
}

// chainedProperties is n+1 models chained by Base, the top declaring property
// "p/kind", shaped further by mutate.
func chainedProperties(n int, mutate func(m *ir.Model)) (ir.TypeRegistry, ir.TypeID) {
	chain := func(i int) ir.TypeID { return ir.TypeID(fmt.Sprintf("t/c%05d", i)) }
	types := ir.TypeRegistry{"t/string": &ir.Primitive{ID: "t/string", Prim: ir.PrimString}}
	for i := 0; i <= n; i++ {
		m := &ir.Model{ID: chain(i)}
		if i == 0 {
			m.Properties = []ir.Property{{ID: "p/kind", Type: ir.TypeRef{Target: "t/string"}}}
		} else {
			m.Base = &ir.TypeRef{Target: chain(i - 1)}
		}
		if mutate != nil {
			mutate(m)
		}
		types[m.ID] = m
	}
	return types, chain(n)
}

// exposureShapes are the documents whose exposed-property questions Validate
// asks once per site: a chain of discriminators, each tagging the property its
// top declares, and n operations each sending the chain's bottom as multipart
// with that property encoded as a part. Every question is answered yes.
func exposureShapes() map[string]func(n int) *ir.Document {
	return map[string]func(n int) *ir.Document{
		"a chain of discriminators tagging the top's property": func(n int) *ir.Document {
			types, _ := chainedProperties(n, func(m *ir.Model) { m.Discriminator = &ir.Discriminator{Property: "p/kind"} })
			return &ir.Document{IRVersion: ir.IRVersion, Name: "t", Version: "1", Types: types}
		},
		"multipart parts naming the top's property of the bottom": func(n int) *ir.Document {
			types, bottom := chainedProperties(n, nil)
			ops := make([]ir.Operation, n)
			for i := range ops {
				ops[i] = ir.Operation{ID: ir.OpID(fmt.Sprintf("op%05d", i)), Request: &ir.Payload{Contents: []ir.Content{{
					MediaType: "multipart/form-data", Type: ir.TypeRef{Target: bottom},
					Encoding: map[ir.PropID]ir.PartEncoding{"p/kind": {}},
				}}}}
			}
			doc := &ir.Document{IRVersion: ir.IRVersion, Name: "t", Version: "1", Types: types}
			doc.Services = []ir.Service{{Groups: []ir.OperationGroup{{Operations: ops}}}}
			return doc
		},
	}
}

// validateCost returns the fewest allocations and bytes a Validate of doc made
// in three runs after one that warms it up, on one processor as
// testing.AllocsPerRun counts. The fewest, because under the race detector
// sync.Pool drops a share of what it is handed, so the fmt calls Validate makes
// allocate a random amount more than they must, and never less.
func validateCost(t *testing.T, doc *ir.Document) (allocs, bytes float64) {
	t.Helper()
	ctx := t.Context()
	defer runtime.GOMAXPROCS(runtime.GOMAXPROCS(1))
	_, err := pass.Validate(ctx, doc)
	require.NoError(t, err)
	allocs, bytes = math.Inf(1), math.Inf(1)
	for range 3 {
		var before, after runtime.MemStats
		runtime.ReadMemStats(&before)
		_, err := pass.Validate(ctx, doc)
		runtime.ReadMemStats(&after)
		require.NoError(t, err)
		allocs = min(allocs, float64(after.Mallocs-before.Mallocs))
		bytes = min(bytes, float64(after.TotalAlloc-before.TotalAlloc))
	}
	return allocs, bytes
}

// TestValidate_ExposedPropertiesCostLinearBytes holds the bytes validating each
// exposure shape allocates to linear growth around three sizes, as the test
// above does for routes. Bytes, not allocations: a walk per question
// collects into maps that grow by doubling, which allocates few times for
// work that grows with the chain. It does not run in parallel, since the count
// is process-wide.
func TestValidate_ExposedPropertiesCostLinearBytes(t *testing.T) {
	for name, build := range exposureShapes() {
		t.Run(name, func(t *testing.T) {
			var bytes [3]float64
			for k, n := range []int{250, 500, 1000} {
				_, bytes[k] = costAround(t, n, build, func(t *testing.T, _ int, diags []ir.Diagnostic) {
					for _, d := range diags {
						require.NotContains(t, []string{"pass/discriminator-unknown-property", "ir/encoding-key-unknown-property"}, d.Code,
							"every question must be answered yes, or the sizes compare different work")
					}
				})
			}
			assert.LessOrEqual(t, bytes[2]-bytes[1], 2.5*(bytes[1]-bytes[0]),
				"bytes around 250, 500, 1000: %v", bytes)
		})
	}
}
