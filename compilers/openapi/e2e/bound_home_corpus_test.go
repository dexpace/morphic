// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package e2e_test

import (
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dexpace/morphic/compilers"
	"github.com/dexpace/morphic/compilers/openapi"
	"github.com/dexpace/morphic/ir"
)

// statedBounds returns the names of the fields constraints states, none for a nil constraints.
func statedBounds(constraints *ir.Constraints) []string {
	if constraints == nil {
		return nil
	}
	var names []string
	v := reflect.ValueOf(*constraints)
	for i := range v.NumField() {
		if !v.Field(i).IsZero() {
			names = append(names, v.Type().Field(i).Name)
		}
	}
	return names
}

// nodeBounds returns the bounds the anonymous node id names in doc states, and
// none for a named node, a primitive or a kind with no Constraints.
func nodeBounds(doc *ir.Document, id ir.TypeID) []string {
	td, ok := doc.Types[id]
	if !ok || !td.Common().Anonymous {
		return nil
	}
	held := reflect.Indirect(reflect.ValueOf(td)).FieldByName("Constraints")
	if !held.IsValid() {
		return nil
	}
	constraints, _ := reflect.TypeAssert[*ir.Constraints](held)
	return statedBounds(constraints)
}

// restatedBounds returns, for each property, header and parameter of doc that
// states a bound the anonymous node its type names states too, its ID and
// those bounds, and how many carriers name an anonymous node stating any.
func restatedBounds(doc *ir.Document) (restated map[string][]string, owning int) {
	restated = map[string][]string{}
	judge := func(id string, ref ir.TypeRef, own *ir.Constraints) {
		node := nodeBounds(doc, ref.Target)
		if len(node) == 0 {
			return
		}
		owning++
		for _, name := range statedBounds(own) {
			if slices.Contains(node, name) {
				restated[id] = append(restated[id], name)
			}
		}
	}
	ir.WalkValues(doc, ir.DocumentPath, ir.VisitFunc(func(v reflect.Value, _ ir.WalkPath) bool {
		if t := v.Type(); t != reflect.TypeFor[ir.Property]() && t != reflect.TypeFor[ir.Parameter]() {
			return true
		}
		ref, isRef := reflect.TypeAssert[ir.TypeRef](v.FieldByName("Type"))
		own, isBounds := reflect.TypeAssert[*ir.Constraints](v.FieldByName("Constraints"))
		if isRef && isBounds {
			judge(v.FieldByName("ID").String(), ref, own)
		}
		return true
	}))
	return restated, owning
}

// TestCorpus_NoCarrierRestatesABoundItsOwnNodeHolds sweeps every committed spec
// and holds each property, header and parameter to stating no bound the
// anonymous node it names states too, so a consumer joining a carrier's bounds
// with its type's meets each once (docs/ir/annotations.md). A field the
// redeclared-property fold composes passes, as the model it names holds none.
// The claim stops at a field folded from a declaration owning its node and one
// owning none that write one bound alike: it keeps the second's on the
// property beside the first's node, and no committed spec writes one.
func TestCorpus_NoCarrierRestatesABoundItsOwnNodeHolds(t *testing.T) {
	t.Parallel()
	var owning int
	for _, path := range corpusSpecs(t) {
		data, err := os.ReadFile(path)
		require.NoError(t, err)
		doc, _, err := openapi.New().Compile(t.Context(), []compilers.Source{{Path: path, Data: data}}, compilers.Options{})
		if err != nil || doc == nil {
			continue
		}
		restated, n := restatedBounds(doc)
		owning += n
		assert.Empty(t, restated, "%s: carriers stating a bound their own node states", filepath.Base(path))
	}
	assert.Positive(t, owning, "the sweep met no carrier over a node stating a bound")
}
