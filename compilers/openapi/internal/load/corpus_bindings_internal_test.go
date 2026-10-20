package load

import (
	"os"
	"testing"

	soa "github.com/speakeasy-api/openapi/openapi"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dexpace/morphic/compilers"
	"github.com/dexpace/morphic/compilers/openapi/internal/openapitest"
)

// TestBindSchemas_PlacesEveryEndAndBindsEveryRefInTheCorpus holds every spec
// under testdata to two invariants of the bindings, with external references
// off and on: each of the source's reference objects the parser resolved has
// its end placed (bind.Table.Through), and each schema $ref of the source's
// model has an entry, unless the budget or the merge bound stopped the
// reading. Neither shows in the IR when a construct the parser resolves
// leaves an end unplaced with nothing below it to lower, so a corpus
// differential cannot see one: a document's $self did that (GitHub #863).
func TestBindSchemas_PlacesEveryEndAndBindsEveryRefInTheCorpus(t *testing.T) {
	t.Parallel()
	checked := 0
	for _, path := range openapitest.SpecFiles(t, "../../../../testdata") {
		data, err := os.ReadFile(path)
		require.NoError(t, err, path)
		for _, external := range []bool{false, true} {
			doc, _, err := Load(t.Context(), 0, compilers.Source{Path: path, Data: data},
				Options{AllowExternalRefs: external})
			if err != nil || doc == nil || doc.Bindings == nil {
				continue
			}
			checked++
			table := doc.Bindings.Table()
			for site := range referenceEnds(t.Context(), doc.Doc, doc.Siblings, sourceDocument{path: path}) {
				_, placed := table.Through(site)
				assert.True(t, placed, "%s (external %v): the end of the reference at %s", path, external, site)
			}
			if table.Exhausted() || table.Truncated() {
				continue
			}
			for item := range walkSource(t.Context(), doc.Doc, doc.Siblings) {
				require.NoError(t, item.Match(soa.Matcher{Schema: func(js *schemaRef) error {
					if js.IsReference() {
						_, ok := doc.Bindings.Entry(js)
						assert.True(t, ok, "%s (external %v): the $ref at %s", path, external,
							item.Location.ToJSONPointer())
					}
					return nil
				}}), path)
			}
		}
	}
	assert.Positive(t, checked, "the corpus loads")
}
