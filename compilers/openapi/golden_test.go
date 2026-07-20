package openapi_test // external test package — exercises only the public API

import (
	"os"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dexpace/morphic/compilers"
	"github.com/dexpace/morphic/compilers/openapi"
	"github.com/dexpace/morphic/ir"
	"github.com/dexpace/morphic/ir/irtest"
)

// TestGolden lowers a full petstore-style document and compares its IR against a
// byte-exact golden snapshot. Regenerate it with
// `go test ./compilers/openapi -run TestGolden -update`.
func TestGolden(t *testing.T) {
	t.Parallel()
	data, err := os.ReadFile("../../testdata/golden/openapi/petstore.yaml")
	require.NoError(t, err)
	doc, diags, err := openapi.New().Compile(t.Context(),
		[]compilers.Source{{Path: "petstore.yaml", Data: data}}, compilers.Options{})
	require.NoError(t, err)
	require.NotNil(t, doc)
	for _, d := range diags {
		assert.NotEqual(t, ir.SeverityError, d.Severity, "unexpected error diagnostic: %+v", d)
	}
	irtest.CompareGolden(t, "../../testdata/golden/openapi/petstore.golden.json", doc)
}
