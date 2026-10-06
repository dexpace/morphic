package resolve_test

import (
	"os"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dexpace/morphic/compilers"
	"github.com/dexpace/morphic/compilers/openapi/internal/load"
	"github.com/dexpace/morphic/compilers/openapi/internal/openapitest"
	"github.com/dexpace/morphic/compilers/openapi/internal/resolve"
)

// TestScope_ModelAt_AnswersAsTheWholeReadDoesAcrossTheCorpus holds ModelAt,
// which reads a pointer a token at a time, to one read of the whole pointer,
// for every position the corpus's specs hold (openapitest.Positions). A step
// that answers otherwise than the library's walk shows here, as the one past
// an operation's responses, which the model holds by value, did.
func TestScope_ModelAt_AnswersAsTheWholeReadDoesAcrossTheCorpus(t *testing.T) {
	t.Parallel()
	specs, schemas := 0, 0
	for _, file := range openapitest.SpecFiles(t, "../../../../testdata") {
		data, err := os.ReadFile(file)
		require.NoError(t, err)
		got, _, err := load.Load(t.Context(), 0, compilers.Source{Path: file, Data: data}, load.Options{})
		if err != nil || got == nil {
			continue // not a document the loader builds a model of
		}
		specs++
		sc := resolve.Scope{Doc: got.Doc}
		for _, pointer := range openapitest.Positions(t.Context(), t, got.Doc) {
			whole := resolve.WholeModelAt(got.Doc, pointer)
			if whole != nil {
				schemas++
			}
			assert.Same(t, whole, sc.ModelAt(pointer), "%s %q", file, pointer)
		}
	}
	assert.Greater(t, specs, 100, "the corpus is read")
	assert.Greater(t, schemas, 500, "and its pointers reach schemas, not only nothing")
}
