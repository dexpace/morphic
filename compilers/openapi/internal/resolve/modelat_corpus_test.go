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

// TestScope_Locate_AnswersAsAtDoesAcrossTheCorpus holds the scope Locate's one
// walk reads a position in to the one At's own walk does, at every position the
// corpus's specs hold. Every reference the loader resolved is read as ending in
// another document, so each one a walk passes shows in the scope.
func TestScope_Locate_AnswersAsAtDoesAcrossTheCorpus(t *testing.T) {
	t.Parallel()
	asked, passed := 0, 0
	for _, file := range openapitest.SpecFiles(t, "../../../../testdata") {
		data, err := os.ReadFile(file)
		require.NoError(t, err)
		got, _, err := load.Load(t.Context(), 0, compilers.Source{Path: file, Data: data},
			load.Options{AllowExternalRefs: true})
		if err != nil || got == nil {
			continue // not a document the loader builds a model of
		}
		sc := resolve.Scope{SelfPath: file, Doc: got.Doc, Ends: endsElsewhere}
		for _, pointer := range openapitest.Positions(t.Context(), t, got.Doc) {
			want, located := sc.At(pointer), sc.Locate(pointer).At()
			assert.Equal(t, want.Foreign, located.Foreign, "%s %q", file, pointer)
			assert.Equal(t, want.Holder, located.Holder, "%s %q", file, pointer)
			asked++
			if want.Foreign {
				passed++
			}
		}
	}
	assert.Greater(t, asked, 10000, "the corpus is read")
	assert.Greater(t, passed, 100, "and hundreds of positions lie past a reference")
}
