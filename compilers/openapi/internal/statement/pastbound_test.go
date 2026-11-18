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
	yaml "go.yaml.in/yaml/v3"

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

// keywordSamples holds a sample of each keyword fill reads that the parser's
// model holds a field for (statement.ParserNodes), which the test holds it
// to. A keyword the model holds no field for is the census's to report past
// the bound (Schema.Unknown).
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
	// Read from the key's node.
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
	for keyword, node := range statement.ParserNodes() {
		if node != nil {
			require.Contains(t, keywordSamples, keyword, "a keyword fill reads and the model holds has a sample")
		}
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
// from the model with no row of fillKeywords to clear it leaves one.
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
		{"schema.keepUnread", keywordSample{"3.1.0", "{required: [a], additionalProperties: false}",
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

// TestFillKeywords_EachComparesTheNodeTheParserReadItFrom holds each keyword
// fill reads to the node the parser's model read its value from: where the
// model's core declares the keyword, the row returns that field's node, and
// where it declares none, the row has no node, so a key merged in is taken
// for outranked. A keyword added to the fill without its node fails here.
func TestFillKeywords_EachComparesTheNodeTheParserReadItFrom(t *testing.T) {
	t.Parallel()
	m := &oas3.Schema{}
	core := reflect.ValueOf(m.GetCore()).Elem()
	fields := map[string]reflect.Value{}
	for i := range core.NumField() {
		if key := core.Type().Field(i).Tag.Get("key"); key != "" {
			fields[key] = core.Field(i)
		}
	}
	nodes := statement.ParserNodes()
	require.NotEmpty(t, nodes)
	for name, node := range nodes {
		field, held := fields[name]
		if !held {
			assert.Nil(t, node, "%s: the parser holds no field for it", name)
			continue
		}
		require.NotNil(t, node, "%s: the parser holds the node it read", name)
		read := &yaml.Node{Kind: yaml.ScalarNode, Value: name}
		field.FieldByName("ValueNode").Set(reflect.ValueOf(read))
		assert.Same(t, read, node(m), name)
	}
}

// outranking writes a document whose component P merges, beside what beside
// writes, a list of two sources: a chain of anchors past
// nodeview.MergeDepthLimit whose first writes deep, and a mapping within the
// bound writing listed, the chain first where deepFirst says.
func outranking(version, deep, listed, beside string, deepFirst bool) string {
	sources := []string{fmt.Sprintf("*c%d", nodeview.MergeDepthLimit), listed}
	if !deepFirst {
		slices.Reverse(sources)
	}
	return strings.Replace(chained(version, nodeview.MergeDepthLimit, deep, beside, ""),
		fmt.Sprintf("P: {<<: *c%d, ", nodeview.MergeDepthLimit),
		"P: {<<: ["+strings.Join(sources, ", ")+"], ", 1)
}

// TestRead_PastTheMergeBoundAnOutrankedKeyIsLeftOut compiles, for a keyword of
// each slot, a schema merging two sources that write it: one past
// nodeview.MergeDepthLimit and one within it, whose key the schema lists.
// Where the source past the bound comes first, it outranks the listed key,
// which is then not read, and the schema compiles as with neither source
// writing the keyword; where the listed source comes first, it is read as
// written. A listed key equal to the value it loses to is left out too.
func TestRead_PastTheMergeBoundAnOutrankedKeyIsLeftOut(t *testing.T) {
	t.Parallel()
	type outranks struct {
		version, deep, listed, beside string
	}
	cases := map[string]outranks{
		"nullable 3.0.3":   {"3.0.3", "{nullable: true}", "{nullable: false}", "type: string"},
		"nullable 3.1.0":   {"3.1.0", "{nullable: true}", "{nullable: false}", "type: string"},
		"nullable 3.2.0":   {"3.2.0", "{nullable: true}", "{nullable: false}", "type: string"},
		"nullable, a word": {"3.0.3", "{nullable: true}", "{nullable: 'yes'}", "type: string"},
		"nullable, equal":  {"3.0.3", "{nullable: true}", "{nullable: true}", "type: string"},
		"type":             {"3.1.0", "{type: integer}", "{type: string}", "description: d"},
		"enum":             {"3.1.0", "{enum: [a]}", "{enum: [b, c]}", "type: string"},
		"format":           {"3.1.0", "{format: date}", "{format: email}", "type: string"},
		"minimum":          {"3.1.0", "{minimum: 5}", "{minimum: 1}", "type: number"},
		"minLength":        {"3.1.0", "{minLength: 5}", "{minLength: 1}", "type: string"},
		"minLength, equal": {"3.1.0", "{minLength: 1}", "{minLength: 1}", "type: string"},
		"items":            {"3.1.0", "{items: {type: integer}}", "{items: {type: string}}", "type: array"},
		"properties":       {"3.1.0", "{properties: {a: {type: string}}}", "{properties: {b: {type: integer}}}", "type: object"},
		"required":         {"3.1.0", "{required: [a]}", "{required: [b]}", "type: object, properties: {a: {type: string}, b: {type: string}}"},
		"oneOf":            {"3.1.0", "{oneOf: [{type: integer}, {type: boolean}]}", "{oneOf: [{type: string}, {type: number}]}", "description: d"},
		"not":              {"3.1.0", "{not: {type: integer}}", "{not: {type: boolean}}", "type: string"},
		"allOf":            {"3.1.0", "{allOf: [{maxLength: 9}]}", "{allOf: [{minLength: 1}]}", "type: string"},
		"title":            {"3.1.0", "{title: Deep}", "{title: Listed}", "type: string"},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			at := func(deep, listed string, deepFirst bool) compiledP {
				return withoutReport(compileP(t, outranking(tc.version, deep, listed, tc.beside, deepFirst)))
			}
			require.NotEqual(t, at("{}", "{}", false), at("{}", tc.deep, false),
				"the value the parser holds changes what P compiles to")
			written := at("{}", tc.listed, false)

			assert.Equal(t, at("{}", "{}", true), at(tc.deep, tc.listed, true), "outranked: left out")
			assert.Equal(t, written, at(tc.deep, tc.listed, false), "listed first: read as written")
			reports := compileP(t, outranking(tc.version, tc.deep, tc.listed, tc.beside, true)).diags
			assert.Equal(t, 1, strings.Count(strings.Join(reports, "\n"), "/components/schemas/P: "+pastBoundReport))
		})
	}
}

// TestRead_PastTheMergeBoundAKeyNoParserNodeTellsStaysOnlyWhereWritten reads
// a schema merging past nodeview.MergeDepthLimit beside keywords the
// parser's model holds no field for, so no node tells whether a source past
// the bound outranks them: the one the schema writes itself is read, as no
// merge outranks that, and one a source within the bound merges in is left
// out with its value.
func TestRead_PastTheMergeBoundAKeyNoParserNodeTellsStaysOnlyWhereWritten(t *testing.T) {
	t.Parallel()
	spec := strings.Replace(chained("3.1.0", nodeview.MergeDepthLimit, "{}", "$comment: c, type: object", ""),
		fmt.Sprintf("P: {<<: *c%d, ", nodeview.MergeDepthLimit),
		fmt.Sprintf("P: {<<: [*c%d, {dependentRequired: {a: [b]}, minimum: 1}], ", nodeview.MergeDepthLimit), 1)
	table, c := tableOver(t, spec, rulesAt(t, "3.1.0", ""))
	p := table.At(component(c, "P"))
	require.True(t, p.Truncated)
	_, comment := p.Key("$comment")
	assert.True(t, comment, "written by the schema itself")
	require.NotNil(t, p.Notes)
	assert.Equal(t, "c", text(p.Notes.Comment))
	_, dependent := p.Key("dependentRequired")
	assert.False(t, dependent, "merged in, with no node to tell it from one a source past the bound outranks")
	assert.Nil(t, p.Object)
	assert.NotContains(t, p.Unknown, "dependentRequired", "nor does the census read it")
	model := c.Bindings().At(component(c, "P")).GetSchema().GetCore().GetUnknownProperties()
	assert.Contains(t, model, "dependentRequired", "the parser's own list is left as it was")
	compiled := compileP(t, spec)
	assert.NotContains(t, compiled.types, "dependentRequired")
	assert.NotContains(t, strings.Join(compiled.diags, "\n"), "dependentRequired")
	_, minimum := p.Key("minimum")
	assert.True(t, minimum, "merged in, and the node the parser read")
	assert.Equal(t, "1", text(p.Number.Minimum))
}

// bodySpec writes a document whose one operation takes an octet-stream body
// merging, beside what beside writes, a chain of n anchors whose first writes
// leaf, as chained does for a component.
func bodySpec(n int, leaf, beside string) string {
	var b strings.Builder
	fmt.Fprintf(&b, "openapi: 3.1.0\ninfo: {title: t, version: '1'}\nx-c0: &c0 %s\n", leaf)
	for i := 1; i <= n; i++ {
		fmt.Fprintf(&b, "x-c%d: &c%d {<<: *c%d}\n", i, i, i-1)
	}
	fmt.Fprintf(&b, "paths:\n  /u:\n    post:\n      requestBody:\n        content:\n"+
		"          application/octet-stream: {schema: {<<: *c%d, %s}}\n"+
		"      responses: {'204': {description: ok}}\n", n, beside)
	return b.String()
}

// compileBody compiles spec and returns its operation's body, its type and its
// file reading, with the types the body's pointer names, encoded.
func compileBody(t *testing.T, spec string) string {
	t.Helper()
	doc, _, err := openapi.New().Compile(t.Context(), []compilers.Source{openapitest.SourceOf(spec)}, compilers.Options{})
	require.NoError(t, err)
	op := openapitest.FirstOp(t, doc.Services[0])
	require.NotNil(t, op.Request)
	types := ir.TypeRegistry{}
	for id, td := range doc.Types {
		if strings.Contains(string(id), "/requestBody/") {
			types[id] = td
		}
	}
	raw, err := json.Marshal(struct {
		Contents []ir.Content
		Types    ir.TypeRegistry
	}{op.Request.Contents, types}, json.Deterministic(true))
	require.NoError(t, err)
	return string(raw)
}

// TestRead_PastTheMergeBoundABodyIsReadAsItsTypeIs compiles an operation's
// octet-stream body merging a keyword the file reading and the type both read,
// through as many `<<` as nodeview.MergeDepthLimit expands and one more:
// within the bound the body reads as written, and past it, its type and its
// file reading are both what the body's keys within the bound alone say.
func TestRead_PastTheMergeBoundABodyIsReadAsItsTypeIs(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct{ leaf, beside string }{
		{"{contentEncoding: base64}", "type: string, format: binary"},
		{"{pattern: '^a'}", "type: string"},
		{"{format: binary}", "type: string"},
	} {
		t.Run(tc.leaf, func(t *testing.T) {
			t.Parallel()
			written := compileBody(t, bodySpec(0, tc.leaf, tc.beside))
			require.NotEqual(t, compileBody(t, bodySpec(0, "{}", tc.beside)), written, "the leaf changes the body")
			assert.Equal(t, written, compileBody(t, bodySpec(nodeview.MergeDepthLimit-1, tc.leaf, tc.beside)))
			assert.Equal(t, compileBody(t, bodySpec(nodeview.MergeDepthLimit, "{}", tc.beside)),
				compileBody(t, bodySpec(nodeview.MergeDepthLimit, tc.leaf, tc.beside)))
		})
	}
}
