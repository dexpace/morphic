package operation_test

import (
	"encoding/json/jsontext"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dexpace/morphic/compilers/openapi/internal/diag"
	"github.com/dexpace/morphic/compilers/openapi/internal/openapitest"
	"github.com/dexpace/morphic/ir"
)

func TestContent_ExampleWithoutValueSkipped(t *testing.T) {
	t.Parallel()
	doc, diags := parseFull(t, multipartVariantsSpec)
	op := openapitest.FindOp(t, doc, "exGet")
	c := op.Responses[0].Payload.Contents[0]
	assert.Empty(t, c.Examples, "an example carrying no example is skipped")

	// Skipped, but not in silence: the entry declares no value in any spelling.
	d, ok := openapitest.FirstDegradedWarning(diags)
	require.True(t, ok, "the skipped entry is reported")
	assert.Equal(t, jsontext.Pointer("/paths/~1examples/get/responses/200/content/application~1json/examples/empty"),
		d.Provenance.Pointer)
}

func TestContent_UnconvertibleExamplesDiagnosed(t *testing.T) {
	t.Parallel()
	// Covers both the singular `example` and one entry of the plural `examples`
	// map — each carries a custom, structurally
	// unconvertible tag, and each conversion failure must be diagnosed rather
	// than discarded silently.
	spec := openapitest.PathsSpec(`  /items:
    get:
      operationId: getItem
      responses:
        "200":
          description: ok
          content:
            application/json:
              schema: {type: string}
              example: !foo bar
              examples:
                one: {value: !foo baz}
`)
	_, svc, diags := lowerServiceSpec(t, spec)
	op := openapitest.FirstOp(t, svc)
	require.Len(t, op.Responses, 1)
	c := op.Responses[0].Payload.Contents[0]
	assert.Empty(t, c.Examples, "both unconvertible examples are skipped, not appended")

	require.Equal(t, 2, openapitest.CountDiagsAt(diags, diag.DegradedConstruct, ir.SeverityWarning))
	pointers := map[jsontext.Pointer]bool{}
	for _, d := range diags {
		if d.Code == diag.DegradedConstruct && d.Severity == ir.SeverityWarning {
			pointers[d.Provenance.Pointer] = true
			assert.Contains(t, d.Message, "example:")
		}
	}
	const base = "/paths/~1items/get/responses/200/content/application~1json"
	assert.True(t, pointers[base+"/example"], "the single example's pointer")
	assert.True(t, pointers[base+"/examples/one/value"], "the plural example's pointer, keyed by its name")
}

func TestContent_RefdExampleDiagnosedAtReferenceSite(t *testing.T) {
	t.Parallel()
	// An `examples` entry written as a $ref holds no value of its own — the
	// value lives in the referenced component — so `.../examples/<name>/value`
	// would name a location the source never had. The diagnostic belongs at the
	// reference site. The convertible sibling pins that a $ref'd example still
	// lowers normally.
	spec := `openapi: 3.1.0
info: {title: T, version: "1.0.0"}
paths:
  /items:
    get:
      operationId: getItem
      responses:
        "200":
          description: ok
          content:
            application/json:
              schema: {type: string}
              examples:
                good: {$ref: '#/components/examples/Good'}
                bad: {$ref: '#/components/examples/Bad'}
components:
  examples:
    Good: {value: fine}
    Bad: {value: !foo baz}
`
	_, svc, diags := lowerServiceSpec(t, spec)
	op := openapitest.FirstOp(t, svc)
	c := op.Responses[0].Payload.Contents[0]
	require.Len(t, c.Examples, 1, "the convertible $ref'd example still lowers")
	require.NotNil(t, c.Examples[0].Value)
	assert.Equal(t, ir.Value{Kind: ir.ValueString, Str: "fine"}, *c.Examples[0].Value)

	require.Equal(t, 1, openapitest.CountDiagsAt(diags, diag.DegradedConstruct, ir.SeverityWarning))
	d, ok := openapitest.FirstDegradedWarning(diags)
	require.True(t, ok)
	assert.Equal(t, jsontext.Pointer("/paths/~1items/get/responses/200/content/application~1json/examples/bad"),
		d.Provenance.Pointer, "the reference site, not a /value the source never had")
	assert.Contains(t, d.Message, "example:")
}

// TestExample_ExternalValueOnlyIsCarried pins the half of an example the IR can
// hold without a value. An example may declare a URL instead of the data, and
// that is a complete example rather than a degraded one — so it is appended
// like any other, and only an example declaring neither is reported.
func TestExample_ExternalValueOnlyIsCarried(t *testing.T) {
	t.Parallel()
	doc, diags := parseFull(t, openapitest.PathsSpec(`  /a:
    get:
      operationId: getA
      responses:
        "200":
          description: ok
          content:
            application/json:
              schema: {type: string}
              examples:
                remote: {externalValue: 'https://e.example/one.json', summary: s}
`))
	openapitest.RequireNoErrorDiags(t, diags)

	resp := openapitest.FindOp(t, doc, "getA").Responses
	require.NotEmpty(t, resp)
	require.NotEmpty(t, resp[0].Payload.Contents)
	examples := resp[0].Payload.Contents[0].Examples
	require.Len(t, examples, 1, "an externalValue-only example is kept: %+v", examples)
	assert.Equal(t, "https://e.example/one.json", examples[0].ExternalURL)
	assert.Nil(t, examples[0].Value, "and carries no inline value")
}

// TestExamples_RefSiteDocsOverrideTheDeclaration pins the example half of GitHub
// #610: an entry written as a Reference Object carries its summary and
// description beside the $ref, and they override the Example Object's own pair.
func TestExamples_RefSiteDocsOverrideTheDeclaration(t *testing.T) {
	t.Parallel()
	spec := `openapi: 3.1.0
info: {title: T, version: "1"}
paths:
  /a:
    get:
      operationId: a
      responses:
        "200":
          description: ok
          content:
            application/json:
              schema: {type: object, properties: {n: {type: string}}}
              examples:
                one:
                  $ref: '#/components/examples/Sample'
                  summary: ES
                  description: ED
components:
  examples:
    Sample: {summary: declared summary, description: declared description, value: {n: x}}
`
	_, svc, diags := lowerServiceSpec(t, spec)
	openapitest.RequireNoErrorDiags(t, diags)
	payload := openapitest.FirstOp(t, svc).Responses[0].Payload
	require.NotNil(t, payload)
	require.Len(t, payload.Contents[0].Examples, 1)
	example := payload.Contents[0].Examples[0]
	assert.Equal(t, "ES", example.Summary)
	assert.Equal(t, "ED", example.Description)
}

// TestParamContent_OwnAndMediaExamplesAreBothKept pins how a parameter's or a
// header's examples combine (GitHub #611): those the object writes itself and
// those its elected media type writes are both kept, its own first, and
// together they replace the schema's — neither drops the other.
func TestParamContent_OwnAndMediaExamplesAreBothKept(t *testing.T) {
	t.Parallel()
	spec := openapitest.PathsSpec(`  /a:
    get:
      operationId: a
      parameters:
        - name: f
          in: query
          example: 1
          content:
            application/json:
              schema: {type: integer, examples: [9]}
              examples: {m: {value: 2}}
      responses:
        "200":
          description: ok
          headers:
            X-H:
              examples: {h: {value: 3}}
              content:
                text/plain:
                  schema: {type: integer, examples: [9]}
                  example: 4
`)
	_, svc, diags := lowerServiceSpec(t, spec)
	openapitest.RequireNoErrorDiags(t, diags)
	op := openapitest.FirstOp(t, svc)
	nums := func(examples []ir.Example) []ir.BigVal {
		out := make([]ir.BigVal, 0, len(examples))
		for _, e := range examples {
			require.NotNil(t, e.Value)
			out = append(out, e.Value.Num)
		}
		return out
	}
	require.Len(t, op.Params, 1)
	assert.Equal(t, []ir.BigVal{"1", "2"}, nums(op.Params[0].Examples),
		"the parameter's own example, then its media type's; the schema's is replaced")
	require.Len(t, op.Responses[0].Headers, 1)
	assert.Equal(t, []ir.BigVal{"3", "4"}, nums(op.Responses[0].Headers[0].Examples),
		"the same at a header")
}

// TestExamples_EverySpellingReachesTheIR pins GitHub #612: a 3.2 dataValue
// lowers into Value as `value` does, and a spelling ir.Example has no field for
// is kept verbatim beside it rather than dropped — serializedValue alone or
// beside a dataValue, one merged in through `<<`, and a dataValue written beside
// `value`. None of them is announced as an entry declaring no value.
func TestExamples_EverySpellingReachesTheIR(t *testing.T) {
	t.Parallel()
	const at = "/paths/~1a/get/responses/200/content/application~1json/examples/e"
	for _, tc := range []struct {
		name      string
		entry     string
		value     ir.BigVal // "" when no value lowers
		kept      string    // the Unmodeled key expected, "" for none
		reason    ir.UnmodeledReason
		keptValue string
	}{
		{"dataValue", `{dataValue: 5}`, "5", "", "", ""},
		{"serializedValue", `{serializedValue: '"5"'}`, "", "openapi:serializedValue", ir.ReasonNoIRHome, `"\"5\""`},
		{"both forms", `{dataValue: 5, serializedValue: '"5"'}`, "5",
			"openapi:serializedValue", ir.ReasonNoIRHome, `"\"5\""`},
		{"merged in", "\n                  <<: {serializedValue: '\"6\"'}\n                  summary: S", "",
			"openapi:serializedValue", ir.ReasonNoIRHome, `"\"6\""`},
		{"dataValue beside value", `{value: 1, dataValue: 2}`, "1",
			"openapi:dataValue", ir.ReasonDegradedLowering, `2`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			spec := openapitest.PathsSpecVer("3.2.0", `  /a:
    get:
      operationId: a
      responses:
        "200":
          description: ok
          content:
            application/json:
              schema: {type: integer}
              examples:
                e: `+tc.entry+`
`)
			_, svc, diags := lowerServiceSpec(t, spec)
			examples := openapitest.FirstOp(t, svc).Responses[0].Payload.Contents[0].Examples
			require.Len(t, examples, 1, "the entry is kept, not dropped")
			if tc.value == "" {
				assert.Nil(t, examples[0].Value)
			} else {
				require.NotNil(t, examples[0].Value)
				assert.Equal(t, tc.value, examples[0].Value.Num)
			}
			if tc.kept == "" {
				assert.Empty(t, examples[0].Unmodeled)
			} else {
				entry, ok := examples[0].Unmodeled[tc.kept]
				require.True(t, ok, "%s is kept verbatim", tc.kept)
				assert.Equal(t, tc.reason, entry.Reason)
				assert.JSONEq(t, tc.keptValue, string(entry.Value))
				assert.Equal(t, jsontext.Pointer(at+"/"+strings.TrimPrefix(tc.kept, "openapi:")),
					entry.Provenance.Pointer)
			}
			assert.False(t, openapitest.HasDiagCodeAt(diags, diag.DegradedConstruct, at),
				"an entry with a value in any spelling is not announced as empty")
		})
	}
}

// TestExamples_ReferencedSpellingIsKeptAtItsDeclaration pins where a $ref'd
// example's serializedValue is kept: at the components/examples entry that
// writes it, not beneath the reference site, which holds no such key.
func TestExamples_ReferencedSpellingIsKeptAtItsDeclaration(t *testing.T) {
	t.Parallel()
	spec := `openapi: 3.2.0
info: {title: T, version: "1"}
paths:
  /a:
    get:
      operationId: a
      responses:
        "200":
          description: ok
          content:
            application/json:
              schema: {type: integer}
              examples:
                e: {$ref: '#/components/examples/Serial'}
components:
  examples:
    Serial: {serializedValue: '"5"'}
`
	_, svc, diags := lowerServiceSpec(t, spec)
	openapitest.RequireNoErrorDiags(t, diags)
	examples := openapitest.FirstOp(t, svc).Responses[0].Payload.Contents[0].Examples
	require.Len(t, examples, 1)
	const decl = "/components/examples/Serial/serializedValue"
	assert.Equal(t, jsontext.Pointer(decl), examples[0].Unmodeled["openapi:serializedValue"].Provenance.Pointer)
	openapitest.AssertInfoDiagAt(t, diags, decl)
}
