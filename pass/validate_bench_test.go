package pass_test // external test package — imports across layers is legal in tests

import (
	"os"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/dexpace/morphic/compilers"
	"github.com/dexpace/morphic/compilers/openapi"
	"github.com/dexpace/morphic/ir"
	"github.com/dexpace/morphic/pass"
)

// goldenPetstore is the larger real-ish spec the golden snapshot is taken from,
// addressed relative to this test file.
const goldenPetstore = "../testdata/golden/openapi/petstore.yaml"

// BenchmarkValidate_Petstore measures a referential-integrity pass over a
// compiled document. Validate walks every reference in the document, so its cost
// tracks document size rather than spec size, and it runs on every compile the
// engine drives — a regression here is paid by every consumer.
func BenchmarkValidate_Petstore(b *testing.B) {
	doc := compilePetstore(b)

	var diags []ir.Diagnostic
	b.ReportAllocs()
	b.ResetTimer()
	for range b.N {
		diags = pass.Validate(doc)
	}
	b.StopTimer()

	// Measure the clean path, and say so: a document Validate rejects would take
	// different branches and the number would not mean what the name claims.
	for _, d := range diags {
		require.NotEqual(b, ir.SeverityError, d.Severity, "unexpected validate error: %+v", d)
	}
}

// compilePetstore compiles the golden petstore once, so the benchmark measures
// Validate rather than the compile that feeds it.
func compilePetstore(b *testing.B) *ir.Document {
	b.Helper()
	data, err := os.ReadFile(goldenPetstore)
	require.NoError(b, err)
	require.NotEmpty(b, data)

	doc, _, err := openapi.New().Compile(b.Context(),
		[]compilers.Source{{Path: "petstore.yaml", Data: data}}, compilers.Options{})
	require.NoError(b, err)
	require.NotNil(b, doc)
	return doc
}
