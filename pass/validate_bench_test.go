package pass_test // external test package — imports across layers is legal in tests

import (
	"fmt"
	"os"
	"strings"
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

// Shape of the generated document BenchmarkValidate_Large measures: chains of
// allOf-extended models, one discriminated root with its subtypes, multipart
// bodies carrying encoding, and operations spread over tags.
const (
	largeChains       = 100
	largeChainLength  = 10
	largeSubtypes     = 20
	largeOperations   = 300
	largeMultipartMod = 5
	largeTags         = 12
)

// BenchmarkValidate_Large measures Validate over a generated document of about a
// thousand models. The petstore benchmark cannot show how a check scales with
// the registry: the subtype and exposed-property walks climb allOf chains, the
// reference walk visits every node, and the group walk visits every operation.
func BenchmarkValidate_Large(b *testing.B) {
	doc := compileLarge(b)

	var diags []ir.Diagnostic
	b.ReportAllocs()
	b.ResetTimer()
	for range b.N {
		diags = pass.Validate(doc)
	}
	b.StopTimer()

	for _, d := range diags {
		require.NotEqual(b, ir.SeverityError, d.Severity, "unexpected validate error: %+v", d)
	}
}

// compileLarge compiles the generated spec once and asserts the compile itself
// raised no error, so the benchmark measures a clean document.
func compileLarge(b *testing.B) *ir.Document {
	b.Helper()
	doc, diags, err := openapi.New().Compile(b.Context(),
		[]compilers.Source{{Path: "large.yaml", Data: []byte(largeSpec())}}, compilers.Options{})
	require.NoError(b, err)
	require.NotNil(b, doc)
	for _, d := range diags {
		require.NotEqual(b, ir.SeverityError, d.Severity, "unexpected compile error: %+v", d)
	}
	require.GreaterOrEqual(b, len(doc.Types), largeChains*largeChainLength)
	return doc
}

// largeSpec renders the OpenAPI document BenchmarkValidate_Large compiles.
func largeSpec() string {
	var sb strings.Builder
	sb.WriteString("openapi: 3.0.3\ninfo:\n  title: Large\n  version: '1'\npaths:\n")
	for i := range largeOperations {
		writeLargeOperation(&sb, i)
	}
	sb.WriteString("components:\n  schemas:\n")
	for c := range largeChains {
		writeLargeChain(&sb, c)
	}
	writeLargeDiscriminated(&sb)
	return sb.String()
}

// writeLargeOperation writes operation i: a tagged GET over a chain model, or a
// multipart POST every largeMultipartMod operations.
func writeLargeOperation(sb *strings.Builder, i int) {
	tag := fmt.Sprintf("tag%d", i%largeTags)
	model := fmt.Sprintf("M%d_%d", i%largeChains, largeChainLength-1)
	fmt.Fprintf(sb, "  /r%d:\n", i)
	if i%largeMultipartMod == 0 {
		fmt.Fprintf(sb, `    post:
      operationId: upload%d
      tags: [%s]
      requestBody:
        content:
          multipart/form-data:
            schema:
              type: object
              properties:
                file: {type: string, format: binary}
                meta: {$ref: '#/components/schemas/%s'}
            encoding:
              file: {contentType: application/octet-stream}
              meta: {contentType: application/json}
      responses:
        '204': {description: ok}
`, i, tag, model)
		return
	}
	fmt.Fprintf(sb, `    get:
      operationId: get%d
      tags: [%s]
      parameters:
        - {name: id, in: query, schema: {type: string}}
      responses:
        '200':
          description: ok
          content:
            application/json:
              schema: {$ref: '#/components/schemas/%s'}
`, i, tag, model)
}

// writeLargeChain writes chain c: a plain root and largeChainLength-1 models each
// extending the one before it through allOf.
func writeLargeChain(sb *strings.Builder, c int) {
	fmt.Fprintf(sb, "    M%d_0:\n      type: object\n      properties:\n        p0: {type: string}\n", c)
	for k := 1; k < largeChainLength; k++ {
		fmt.Fprintf(sb, `    M%d_%d:
      allOf:
        - $ref: '#/components/schemas/M%d_%d'
        - type: object
          properties:
            p%d: {type: string}
`, c, k, c, k-1, k)
	}
}

// writeLargeDiscriminated writes a discriminated root and its subtypes.
func writeLargeDiscriminated(sb *strings.Builder) {
	sb.WriteString("    Root:\n      type: object\n      required: [kind]\n      properties:\n        kind: {type: string}\n")
	sb.WriteString("      discriminator:\n        propertyName: kind\n        mapping:\n")
	for s := range largeSubtypes {
		fmt.Fprintf(sb, "          s%d: '#/components/schemas/Sub%d'\n", s, s)
	}
	for s := range largeSubtypes {
		fmt.Fprintf(sb, `    Sub%d:
      allOf:
        - $ref: '#/components/schemas/Root'
        - type: object
          properties:
            extra%d: {type: string}
`, s, s)
	}
}
