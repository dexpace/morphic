// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package load

import (
	"fmt"
	"os"
	"testing"

	soa "github.com/speakeasy-api/openapi/openapi"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dexpace/morphic/compilers"
	"github.com/dexpace/morphic/compilers/openapi/internal/bind/typed"
	"github.com/dexpace/morphic/compilers/openapi/internal/openapitest"
)

// TestBindAll_BindsEveryRefInTheCorpus holds every spec under testdata to the
// invariants of the bindings, with external references off and on, unless the
// budget or the merge bound stopped the reading: each reference wrapper of the
// source's model the library reads as one has the table's entry at its site,
// as its kind, each entry a wrapper held at its site, which the lowering reads
// it through, and each schema $ref an entry. The table also reads a $ref a
// `<<` merges in, which the library's wrapper calls inline, so only that
// direction is asked of the library.
func TestBindAll_BindsEveryRefInTheCorpus(t *testing.T) {
	t.Parallel()
	checked := 0
	for _, path := range openapitest.SpecFiles(t, "../../../../testdata") {
		data, err := os.ReadFile(path)
		require.NoError(t, err, path)
		for _, external := range []bool{false, true} {
			doc, _, err := Load(t.Context(), 0, compilers.Source{Path: path, Data: data},
				readingIf(external))
			if err != nil || doc == nil || doc.Bindings == nil {
				continue
			}
			table := doc.Bindings.Table()
			if table.Exhausted() || table.Truncated() {
				continue
			}
			checked++
			for item := range walkSource(t.Context(), doc.Doc, doc.Siblings, doc.MediaTypes) {
				require.NoError(t, item.Match(soa.Matcher{Any: func(model any) error {
					assertBound(t, doc.Bindings, model, fmt.Sprintf("%s (external %v) at %s", path, external,
						item.Location.ToJSONPointer()))
					return nil
				}}), path)
			}
			for _, r := range table.References() {
				assert.NotNil(t, doc.Bindings.ObjectAt(r.Site, r.Kind), "%s (external %v): a wrapper at %v",
					path, external, r.Site)
			}
		}
	}
	assert.Positive(t, checked, "the corpus loads")
}

// assertBound requires model, a wrapper or a schema of the source's model,
// written as a reference, to have the table's entry at the position it was
// read at, named at.
func assertBound(t *testing.T, b *typed.Bindings, model any, at string) {
	t.Helper()
	if js, ok := model.(*schemaRef); ok {
		if js.IsReference() {
			_, bound := b.Entry(js)
			assert.True(t, bound, "%s: the $ref", at)
		}
		return
	}
	w, ok := model.(interface{ IsReference() bool })
	if _, kind := refKindOf(model); !kind || !ok || !w.IsReference() {
		return
	}
	_, bound := b.Reference(model)
	assert.True(t, bound, "%s: the reference object", at)
}
