package pass_test // external test package — imports across layers is legal in tests

import (
	"fmt"
	"math/rand/v2"
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

// Shape of the hand-built document BenchmarkValidate_GraphQLReachability
// measures. No compiler emits GraphQL bindings yet, so the document is built
// directly.
const (
	gqlTypes      = 2000
	gqlOperations = 48
	gqlRootStride = 53
	gqlRefWindow  = 300 // references land among the most recent types, so the graph is deep rather than a star on early ones
)

// BenchmarkValidate_GraphQLReachability measures the GraphQL reachability walk
// inside Validate. Only operations carrying Bindings.GraphQL reach it, which no
// compiled fixture has, so the petstore and large benchmarks never exercise the
// traversal this one is for.
func BenchmarkValidate_GraphQLReachability(b *testing.B) {
	doc := buildGraphQLDoc(b)

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

// buildGraphQLDoc builds the document once and asserts Validate accepts it, so
// the benchmark times the clean path. Types cycle through scalar alias chains,
// lists, maps, unions and models, each referring only to earlier types; the
// operations' roots are spread across the registry so they reach most of it.
func buildGraphQLDoc(b *testing.B) *ir.Document {
	b.Helper()
	str := ir.TypeID("t/prim/string")
	doc := &ir.Document{
		IRVersion: ir.IRVersion, Name: "gql", Version: "1",
		Types: ir.TypeRegistry{str: &ir.Primitive{TypeCommon: ir.TypeCommon{ID: str}, Prim: "string"}},
	}
	ids := []ir.TypeID{str}
	var models []ir.TypeID
	rng := rand.New(rand.NewPCG(1, 2)) // fixed seed: the same document every run
	pick := func(int, int) ir.TypeRef {
		return ir.TypeRef{Target: ids[len(ids)-1-rng.IntN(min(len(ids), gqlRefWindow))]}
	}
	for i := range gqlTypes {
		id := ir.TypeID(fmt.Sprintf("t/g%d", i))
		common := ir.TypeCommon{ID: id}
		switch i % 10 {
		case 0, 1:
			base := ir.TypeRef{Target: str}
			if i >= 2 {
				base = ir.TypeRef{Target: ir.TypeID(fmt.Sprintf("t/g%d", i-2))}
			}
			doc.Types[id] = &ir.Scalar{TypeCommon: common, Base: &base}
		case 2:
			doc.Types[id] = &ir.List{TypeCommon: common, Elem: pick(i, 1)}
		case 3:
			doc.Types[id] = &ir.MapT{TypeCommon: common, Key: ir.TypeRef{Target: str}, Value: pick(i, 2)}
		case 4:
			doc.Types[id] = &ir.Union{TypeCommon: common, Variants: []ir.Variant{
				{Type: pick(i, 3)}, {Type: pick(i, 4)}, {Type: pick(i, 5)},
			}}
		default:
			doc.Types[id] = graphQLModel(common, i, pick, models, rng)
			models = append(models, id)
		}
		ids = append(ids, id)
	}
	addGraphQLOperations(doc, ids)

	for _, d := range pass.Validate(doc) {
		require.NotEqual(b, ir.SeverityError, d.Severity, "fixture rejected by validate: %+v", d)
	}
	return doc
}

// graphQLModel builds a model with three properties, an earlier model as its
// base and another as a mixin when any exist.
func graphQLModel(common ir.TypeCommon, i int, pick func(int, int) ir.TypeRef, models []ir.TypeID, rng *rand.Rand) *ir.Model {
	m := &ir.Model{TypeCommon: common}
	for p := range 3 {
		m.Properties = append(m.Properties, ir.Property{
			ID:       ir.PropID(fmt.Sprintf("p/%s/%d", common.ID, p)),
			Name:     ir.Naming{Source: fmt.Sprintf("f%d", p)},
			WireName: fmt.Sprintf("f%d", p),
			Type:     pick(i, 6+p),
		})
	}
	if len(models) >= 2 {
		base := ir.TypeRef{Target: models[rng.IntN(len(models))]}
		m.Base = &base
		m.Mixins = []ir.TypeRef{{Target: models[rng.IntN(len(models))]}}
	}
	return m
}

// addGraphQLOperations adds gqlOperations GraphQL-bound operations whose
// parameters name types spread over the registry, preferring the late ones that
// transitively reach the most.
func addGraphQLOperations(doc *ir.Document, ids []ir.TypeID) {
	ops := make([]ir.Operation, 0, gqlOperations)
	for o := range gqlOperations {
		root := ids[len(ids)-1-(o*gqlRootStride)%len(ids)]
		ops = append(ops, ir.Operation{
			ID:       ir.OpID(fmt.Sprintf("op/q%d", o)),
			Params:   []ir.Parameter{{Name: ir.Naming{Source: "in"}, Type: ir.TypeRef{Target: root}}},
			Bindings: ir.OpBindings{GraphQL: &ir.GraphQLBinding{Kind: "query", FieldPath: []string{fmt.Sprintf("q%d", o)}}},
		})
	}
	doc.Services = []ir.Service{{ID: "s", Groups: []ir.OperationGroup{{Operations: ops}}}}
}
