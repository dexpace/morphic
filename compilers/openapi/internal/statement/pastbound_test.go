// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package statement_test

import (
	"encoding/json/v2"
	"fmt"
	"reflect"
	"slices"
	"strings"
	"testing"

	"github.com/speakeasy-api/openapi/jsonschema/oas3"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dexpace/morphic/compilers"
	"github.com/dexpace/morphic/compilers/openapi"
	"github.com/dexpace/morphic/compilers/openapi/internal/diag"
	"github.com/dexpace/morphic/compilers/openapi/internal/nodeview"
	"github.com/dexpace/morphic/compilers/openapi/internal/openapitest"
	"github.com/dexpace/morphic/compilers/openapi/internal/statement"
	"github.com/dexpace/morphic/ir"
)

// pastBoundReport is the start of the warning a schema merging past
// nodeview.MergeDepthLimit draws at its own pointer.
const pastBoundReport = "the schema merges through `<<` deeper than"

// chained writes GitHub #2020's document: an anchor holding leaf, n anchors
// each merging the one before, and the component P merging the last beside
// what beside writes, with the other components extra writes. At n
// nodeview.MergeDepthLimit-1, P merges through as many `<<` as the bound
// expands; at n nodeview.MergeDepthLimit, one more.
func chained(version string, n int, leaf, beside, extra string) string {
	var b strings.Builder
	fmt.Fprintf(&b, "openapi: %s\ninfo: {title: t, version: '1'}\npaths: {}\nx-c0: &c0 %s\n", version, leaf)
	for i := 1; i <= n; i++ {
		fmt.Fprintf(&b, "x-c%d: &c%d {<<: *c%d}\n", i, i, i-1)
	}
	fmt.Fprintf(&b, "components:\n  schemas:\n    P: {<<: *c%d, %s}\n", n, beside)
	if extra != "" {
		b.WriteString("    " + extra + "\n")
	}
	return b.String()
}

// compiledP is what a compile makes of the component P: the types whose IDs
// name its pointer, encoded, and the diagnostics at or beneath it.
type compiledP struct {
	types string
	diags []string
}

// compileP compiles spec and returns what it makes of P.
func compileP(t *testing.T, spec string) compiledP {
	t.Helper()
	doc, diags, err := openapi.New().Compile(t.Context(), []compilers.Source{openapitest.SourceOf(spec)}, compilers.Options{})
	require.NoError(t, err)
	types := ir.TypeRegistry{}
	for id, td := range doc.Types {
		if strings.Contains(string(id), "/components/schemas/P") {
			types[id] = td
		}
	}
	raw, err := json.Marshal(types, json.Deterministic(true))
	require.NoError(t, err)
	var out compiledP
	out.types = string(raw)
	for _, d := range diags {
		if strings.HasPrefix(string(d.Provenance.Pointer), "/components/schemas/P") {
			out.diags = append(out.diags, d.Code+" "+string(d.Provenance.Pointer)+": "+d.Message)
		}
	}
	slices.Sort(out.diags)
	return out
}

// pastBoundReports returns how many of diags are the warning at pointer that
// a schema merges past nodeview.MergeDepthLimit.
func pastBoundReports(diags []ir.Diagnostic, pointer string) int {
	n := 0
	for _, d := range diags {
		if d.Code == diag.DegradedConstruct && string(d.Provenance.Pointer) == pointer &&
			strings.HasPrefix(d.Message, pastBoundReport) {
			n++
		}
	}
	return n
}

// withoutReport returns c without the warning that P merges past the bound.
func withoutReport(c compiledP) compiledP {
	c.diags = slices.DeleteFunc(slices.Clone(c.diags), func(d string) bool {
		return strings.Contains(d, pastBoundReport)
	})
	return c
}

// keywordSample is a leaf writing one keyword, what P writes beside the merge
// so that the keyword has something to say, and the other components it
// names.
type keywordSample struct {
	version, leaf, beside, extra string
}

// keywordSamples holds a sample of each keyword fill reads from the parser's
// model (statement.ModelKeywords), which the test holds it to, and of each
// bound it reads from the key's node. A keyword the model holds no field for
// is the census's to report past the bound (Schema.Unknown).
var keywordSamples = map[string]keywordSample{
	"allOf":                 {"3.1.0", "{allOf: [{minLength: 1}]}", "type: string", ""},
	"type":                  {"3.1.0", "{type: integer}", "description: d", ""},
	"nullable":              {"3.0.3", "{nullable: true}", "type: string", ""},
	"exclusiveMinimum":      {"3.1.0", "{exclusiveMinimum: 1}", "type: number", ""},
	"exclusiveMaximum":      {"3.1.0", "{exclusiveMaximum: 9}", "type: number", ""},
	"enum":                  {"3.1.0", "{enum: [a, b]}", "type: string", ""},
	"const":                 {"3.1.0", "{const: a}", "type: string", ""},
	"format":                {"3.1.0", "{format: date}", "type: string", ""},
	"minLength":             {"3.1.0", "{minLength: 1}", "type: string", ""},
	"maxLength":             {"3.1.0", "{maxLength: 5}", "type: string", ""},
	"pattern":               {"3.1.0", "{pattern: '^a'}", "type: string", ""},
	"contentEncoding":       {"3.1.0", "{contentEncoding: base64}", "type: string", ""},
	"contentMediaType":      {"3.1.0", "{contentMediaType: image/png}", "type: string", ""},
	"contentSchema":         {"3.1.0", "{contentSchema: {type: object}}", "type: string, contentMediaType: application/json", ""},
	"items":                 {"3.1.0", "{items: {type: string}}", "type: array", ""},
	"prefixItems":           {"3.1.0", "{prefixItems: [{type: string}]}", "type: array", ""},
	"contains":              {"3.1.0", "{contains: {type: string}}", "type: array", ""},
	"minContains":           {"3.1.0", "{minContains: 1}", "type: array, contains: {type: string}", ""},
	"maxContains":           {"3.1.0", "{maxContains: 2}", "type: array, contains: {type: string}", ""},
	"minItems":              {"3.1.0", "{minItems: 1}", "type: array, items: {type: string}", ""},
	"maxItems":              {"3.1.0", "{maxItems: 3}", "type: array, items: {type: string}", ""},
	"uniqueItems":           {"3.1.0", "{uniqueItems: true}", "type: array, items: {type: string}", ""},
	"unevaluatedItems":      {"3.1.0", "{unevaluatedItems: false}", "type: array", ""},
	"properties":            {"3.1.0", "{properties: {a: {type: string}}}", "type: object", ""},
	"patternProperties":     {"3.1.0", "{patternProperties: {'^x': {type: string}}}", "type: object", ""},
	"additionalProperties":  {"3.1.0", "{additionalProperties: false}", "type: object, properties: {a: {type: string}}", ""},
	"propertyNames":         {"3.1.0", "{propertyNames: {pattern: '^a'}}", "type: object", ""},
	"unevaluatedProperties": {"3.1.0", "{unevaluatedProperties: false}", "type: object", ""},
	"required":              {"3.1.0", "{required: [a]}", "type: object, properties: {a: {type: string}}", ""},
	"minProperties":         {"3.1.0", "{minProperties: 1}", "type: object", ""},
	"maxProperties":         {"3.1.0", "{maxProperties: 3}", "type: object", ""},
	"discriminator": {"3.1.0", "{discriminator: {propertyName: k}}", "oneOf: [{$ref: '#/components/schemas/M'}]",
		"M: {type: object, properties: {k: {type: string}}, required: [k]}"},
	"oneOf":            {"3.1.0", "{oneOf: [{type: string}, {type: integer}]}", "description: d", ""},
	"anyOf":            {"3.1.0", "{anyOf: [{type: string}, {type: integer}]}", "description: d", ""},
	"not":              {"3.1.0", "{not: {type: integer}}", "type: string", ""},
	"if":               {"3.1.0", "{if: {minLength: 1}}", "type: string", ""},
	"then":             {"3.1.0", "{then: {maxLength: 2}}", "type: string, if: {minLength: 1}", ""},
	"else":             {"3.1.0", "{else: {maxLength: 2}}", "type: string, if: {minLength: 1}", ""},
	"dependentSchemas": {"3.1.0", "{dependentSchemas: {a: {required: [b]}}}", "type: object", ""},
	"title":            {"3.1.0", "{title: T}", "type: string", ""},
	"description":      {"3.1.0", "{description: D}", "type: string", ""},
	"default":          {"3.1.0", "{default: a}", "type: string", ""},
	"example":          {"3.1.0", "{example: a}", "type: string", ""},
	"examples":         {"3.1.0", "{examples: [a]}", "type: string", ""},
	"deprecated":       {"3.1.0", "{deprecated: true}", "type: string", ""},
	"readOnly":         {"3.1.0", "{readOnly: true}", "type: string", ""},
	"writeOnly":        {"3.1.0", "{writeOnly: true}", "type: string", ""},
	"xml":              {"3.1.0", "{xml: {name: n}}", "type: object", ""},
	"externalDocs":     {"3.1.0", "{externalDocs: {url: 'https://example.com'}}", "type: string", ""},
	// Read from the key's node, which a key past the bound does not list.
	"minimum":    {"3.1.0", "{minimum: 1}", "type: number", ""},
	"maximum":    {"3.1.0", "{maximum: 9}", "type: number", ""},
	"multipleOf": {"3.1.0", "{multipleOf: 2}", "type: number", ""},
}

// TestRead_PastTheMergeBoundNoKeywordIsHalfRead compiles GitHub #2020's
// document for each keyword: one `<<` within nodeview.MergeDepthLimit, P
// compiles as with the keyword written in place; one past it, as with
// nothing merged in, the keyword neither lowered nor kept by any reader, and
// P draws one warning at its own pointer. Each sample changes what P
// compiles to, so the second comparison can fail.
func TestRead_PastTheMergeBoundNoKeywordIsHalfRead(t *testing.T) {
	t.Parallel()
	for _, keyword := range statement.ModelKeywords() {
		require.Contains(t, keywordSamples, keyword, "a keyword fill reads from the model has a sample")
	}
	for keyword, k := range keywordSamples {
		t.Run(keyword, func(t *testing.T) {
			t.Parallel()
			at := func(n int, leaf string) compiledP {
				return compileP(t, chained(k.version, n, leaf, k.beside, k.extra))
			}
			written := at(0, k.leaf)
			require.NotEqual(t, at(0, "{}"), written, "the sample changes what P compiles to")

			assert.Equal(t, written, at(nodeview.MergeDepthLimit-1, k.leaf), "within the bound")

			past, nothing := at(nodeview.MergeDepthLimit, k.leaf), at(nodeview.MergeDepthLimit, "{}")
			assert.Equal(t, withoutReport(nothing), withoutReport(past), "past the bound")
			assert.Equal(t, 1, strings.Count(strings.Join(past.diags, "\n"), "/components/schemas/P: "+pastBoundReport))
		})
	}
}

// TestRead_PastTheMergeBoundTheSchemasOwnKeysStay compiles a schema merging
// past nodeview.MergeDepthLimit beside keywords of its own: those are read,
// one the merge past the bound writes too among them, which the schema's own
// outranks, and the rest of what the merge brings is not.
func TestRead_PastTheMergeBoundTheSchemasOwnKeysStay(t *testing.T) {
	t.Parallel()
	got := compileP(t, chained("3.1.0", nodeview.MergeDepthLimit, "{maxLength: 9, minLength: 1, pattern: '^a'}",
		"type: string, maxLength: 3", ""))
	want := compileP(t, chained("3.1.0", 0, "{}", "type: string, maxLength: 3", ""))
	assert.Equal(t, want.types, got.types)
	assert.Contains(t, got.types, `"maxLength":3`)
	assert.Contains(t, got.types, `"target":"t/prim/string"`)
}

// TestRead_PastTheMergeBoundEachSchemaIsReportedOnce compiles one chain past
// nodeview.MergeDepthLimit merged into a property, an inline allOf entry, an
// operation's parameter, a shared parameter two operations name and a shared
// response two name: each schema object draws one warning, at its own
// pointer, however many positions lower it.
func TestRead_PastTheMergeBoundEachSchemaIsReportedOnce(t *testing.T) {
	t.Parallel()
	var b strings.Builder
	b.WriteString("openapi: 3.1.0\ninfo: {title: t, version: '1'}\nx-c0: &c0 {minLength: 2}\n")
	for i := 1; i <= nodeview.MergeDepthLimit; i++ {
		fmt.Fprintf(&b, "x-c%d: &c%d {<<: *c%d}\n", i, i, i-1)
	}
	chain := fmt.Sprintf("{<<: *c%d, type: string}", nodeview.MergeDepthLimit)
	b.WriteString(`paths:
  /a:
    get:
      parameters:
        - {name: p, in: query, schema: ` + chain + `}
        - {$ref: '#/components/parameters/Q'}
      responses: {'200': {$ref: '#/components/responses/R'}}
  /b:
    get:
      parameters: [{$ref: '#/components/parameters/Q'}]
      responses: {'200': {$ref: '#/components/responses/R'}}
components:
  parameters:
    Q: {name: q, in: query, schema: ` + chain + `}
  responses:
    R: {description: ok, content: {application/json: {schema: ` + chain + `}}}
  schemas:
    S:
      type: object
      properties: {a: ` + chain + `, b: {$ref: '#/components/schemas/S/properties/a'}}
      allOf: [` + strings.Replace(chain, "string", "object", 1) + `, {properties: {c: {type: string}}}]
`)
	_, diags, err := openapi.New().Compile(t.Context(), []compilers.Source{openapitest.SourceOf(b.String())},
		compilers.Options{})
	require.NoError(t, err)
	for _, pointer := range []string{"/components/schemas/S/properties/a", "/components/schemas/S/allOf/0",
		"/paths/~1a/get/parameters/0/schema", "/components/parameters/Q/schema",
		"/components/responses/R/content/application~1json/schema"} {
		assert.Equal(t, 1, pastBoundReports(diags, pointer), pointer)
	}
	total := 0
	for _, d := range diags {
		if strings.HasPrefix(d.Message, pastBoundReport) {
			total++
		}
	}
	assert.Equal(t, 5, total, "%v", diags)
}

// TestFill_ATruncatedSchemaHoldsNoKeywordItsKeysDoNotList holds listedOnly to
// fill: a Truncated schema whose keys list nothing, read from a model holding
// a value in every field, holds no typed value at all. A keyword fill reads
// from the model with no row of modelKeywords to clear it leaves one.
func TestFill_ATruncatedSchemaHoldsNoKeywordItsKeysDoNotList(t *testing.T) {
	t.Parallel()
	m := &oas3.Schema{}
	v := reflect.ValueOf(m).Elem()
	for i := range v.NumField() {
		f := v.Field(i)
		if !v.Type().Field(i).IsExported() || v.Type().Field(i).Anonymous {
			continue
		}
		switch f.Kind() {
		case reflect.Pointer:
			f.Set(reflect.New(f.Type().Elem()))
			// An either-value holding neither arm is one no decode makes.
			if f.Elem().Kind() != reflect.Struct {
				continue
			}
			if arm := f.Elem().FieldByName("Right"); arm.IsValid() && arm.Kind() == reflect.Pointer {
				arm.Set(reflect.New(arm.Type().Elem()))
			}
		case reflect.Slice:
			f.Set(reflect.MakeSlice(f.Type(), 1, 1))
		default:
			require.Failf(t, "unhandled field kind", "%s is %s", v.Type().Field(i).Name, f.Kind())
		}
	}
	s := statement.FillTruncated(m)
	got := reflect.ValueOf(s).Elem()
	for i := range got.NumField() {
		field := got.Type().Field(i)
		if !field.IsExported() || field.Name == "Truncated" {
			continue
		}
		assert.True(t, got.Field(i).IsZero(), "%s holds what the keys do not list", field.Name)
	}
}

// TestRead_PastTheMergeBoundEachReaderReadsNeither compiles, for each reader
// that reads some keywords of a schema from its keys and others from the
// parser's model, a leaf it reads both ways merged in past
// nodeview.MergeDepthLimit: P compiles as with nothing merged in, so the two
// readings agree that the leaf is not there.
func TestRead_PastTheMergeBoundEachReaderReadsNeither(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		reader string
		keywordSample
	}{
		{"annotation.Constraints", keywordSample{"3.1.0", "{minLength: 2, pattern: '^a'}", "type: string", ""}},
		{"annotation.applyExclusive, a flag", keywordSample{"3.0.3", "{exclusiveMinimum: true}", "type: number, minimum: 1", ""}},
		{"annotation.applyExclusive, a bound", keywordSample{"3.1.0", "{exclusiveMinimum: 3}", "type: number, minimum: 1", ""}},
		{"schema.ownCollectionConstraints", keywordSample{"3.1.0", "{minItems: 1, uniqueItems: true}",
			"type: array, items: {type: string}", ""}},
		{"schema.missingArrayItems", keywordSample{"3.0.3", "{items: {type: string}}", "type: array", ""}},
		{"schema.referenceHolds", keywordSample{"3.1.0", "{minItems: 1, uniqueItems: true}",
			"$ref: '#/components/schemas/L'", "L: {type: array, items: {type: string}}"}},
		{"annotation.inertBeside, items", keywordSample{"3.1.0", "{items: {type: string}}",
			"type: array, unevaluatedItems: false", ""}},
		{"annotation.inertBeside, additionalProperties", keywordSample{"3.1.0", "{additionalProperties: true}",
			"type: object, unevaluatedProperties: false", ""}},
		{"schema.declaresValidationOnly", keywordSample{"3.1.0", "{not: {type: integer}}", "type: string", ""}},
		{"schema.bodyReading.keysRestate", keywordSample{"3.1.0", "{properties: {a: {type: string}}}",
			"allOf: [{$ref: '#/components/schemas/L'}]", "L: {type: object, properties: {a: {type: string}}}"}},
		{"schema.keywordHome", keywordSample{"3.1.0", "{required: [a], additionalProperties: false}",
			"properties: {a: {type: string}}", ""}},
		{"schema.branchesSay", keywordSample{"3.1.0", "{oneOf: [{type: string}, {type: integer}]}",
			"type: [string, integer]", ""}},
		{"schema.nullableTrue, 3.0 true", keywordSample{"3.0.3", "{nullable: true}", "type: string", ""}},
		{"schema.nullableTrue, 3.0 a word", keywordSample{"3.0.3", "{nullable: 'yes'}", "type: string", ""}},
		{"schema.nullableTrue, 3.1 true", keywordSample{"3.1.0", "{nullable: true}", "type: string", ""}},
		{"schema.nullableTrue, 3.1 a word", keywordSample{"3.1.0", "{nullable: 'yes'}", "type: string", ""}},
		{"schema.nullableTrue, 3.2 true", keywordSample{"3.2.0", "{nullable: true}", "type: string", ""}},
		{"schema.nullableTrue, 3.2 a word", keywordSample{"3.2.0", "{nullable: 'yes'}", "type: string", ""}},
	} {
		t.Run(tc.reader, func(t *testing.T) {
			t.Parallel()
			at := func(leaf string) compiledP {
				return compileP(t, chained(tc.version, nodeview.MergeDepthLimit, leaf, tc.beside, tc.extra))
			}
			assert.Equal(t, withoutReport(at("{}")), withoutReport(at(tc.leaf)))
		})
	}
}
