package pass_test // external test package — imports across layers is legal in tests

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dexpace/morphic/compilers"
	"github.com/dexpace/morphic/compilers/openapi"
	"github.com/dexpace/morphic/ir"
	"github.com/dexpace/morphic/pass"
)

// TestValidate_Corpus walks the capability-conformance corpus through the
// OpenAPI compiler and asserts every lowered document passes referential-
// integrity validation with no error-severity diagnostics. It keeps the corpus
// honest: a compiler change that produces a dangling reference or an invalid
// discriminator mapping fails here even when the golden still round-trips.
func TestValidate_Corpus(t *testing.T) {
	t.Parallel()
	files, err := filepath.Glob("../testdata/conformance/openapi/*.yaml")
	require.NoError(t, err)
	require.NotEmpty(t, files, "corpus directory must contain spec files")
	for _, f := range files {
		t.Run(filepath.Base(f), func(t *testing.T) {
			t.Parallel()
			data, err := os.ReadFile(f)
			require.NoError(t, err)
			doc, _, err := openapi.New().Compile(t.Context(),
				[]compilers.Source{{Path: filepath.Base(f), Data: data}}, compilers.Options{})
			require.NoError(t, err)
			require.NotNil(t, doc)
			for _, d := range pass.Validate(doc) {
				assert.NotEqual(t, ir.SeverityError, d.Severity,
					"%s: unexpected validate error: %+v", filepath.Base(f), d)
			}
		})
	}
}
