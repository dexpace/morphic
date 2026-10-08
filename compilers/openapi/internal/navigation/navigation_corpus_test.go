package navigation_test

import (
	"encoding/json/jsontext"
	"os"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dexpace/morphic/compilers"
	"github.com/dexpace/morphic/compilers/openapi/internal/load"
	"github.com/dexpace/morphic/compilers/openapi/internal/navigation"
	"github.com/dexpace/morphic/compilers/openapi/internal/openapitest"
)

// TestWalk_AnswersAsTheWholeReadDoesAcrossTheCorpus holds Walk, followed by the
// library's read of what it leaves in raw YAML, to the library's read of the
// whole pointer at every position the corpus's specs hold, as the loader
// builds their models: each place the model's walk reaches, each path through
// its tree, and each with a stray token appended.
func TestWalk_AnswersAsTheWholeReadDoesAcrossTheCorpus(t *testing.T) {
	t.Parallel()
	specs, found, raw := 0, 0, 0
	for _, file := range openapitest.SpecFiles(t, "../../../../testdata") {
		data, err := os.ReadFile(file)
		require.NoError(t, err)
		got, _, err := load.Load(t.Context(), 0, compilers.Source{Path: file, Data: data}, load.Options{})
		if err != nil || got == nil {
			continue // not a document the loader builds a model of
		}
		specs++
		for _, pointer := range openapitest.Positions(t.Context(), t, got.Doc) {
			want, wantErr := whole(got.Doc, pointer)
			read, readErr := walked(got.Doc, pointer)
			if wantErr != nil {
				require.Error(t, readErr, "%s %q: the whole read fails with %v", file, pointer, wantErr)
				require.Equal(t, settled(wantErr), settled(readErr), "%s %q: %v, then %v", file, pointer, wantErr, readErr)
				continue
			}
			require.NoError(t, readErr, "%s %q", file, pointer)
			requireSameTarget(t, want, read, pointer)
			found++
			if leftModel(t, got.Doc, pointer) {
				raw++
			}
		}
	}
	assert.Greater(t, specs, 100, "the corpus is read")
	assert.Greater(t, found, 5000, "its pointers find things, not only nothing")
	assert.Greater(t, raw, 400, "and hundreds of them leave the model for raw YAML")
}

// leftModel reports whether Walk's read of pointer in doc leaves the model.
func leftModel(t *testing.T, doc any, pointer jsontext.Pointer) bool {
	t.Helper()
	tokens, ok := navigation.Tokens(pointer)
	require.True(t, ok)
	_, rest, err := navigation.Walk(doc, tokens)
	return err == nil && len(rest) > 0
}
