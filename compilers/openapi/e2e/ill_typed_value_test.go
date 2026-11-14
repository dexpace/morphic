// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package e2e_test

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/google/go-cmp/cmp"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dexpace/morphic/compilers/openapi"
	"github.com/dexpace/morphic/ir"
)

// head31 opens every document below.
const head31 = "openapi: 3.1.0\ninfo: {title: t, version: \"1\"}\n"

// falseSchema is the code of a boolean false schema.
const falseSchema = "openapi/false-schema"

// pointersOf returns the pointers of diags, sorted.
func pointersOf(diags []ir.Diagnostic) []string {
	out := make([]string, 0, len(diags))
	for _, d := range diags {
		out = append(out, string(d.Provenance.Pointer))
	}
	slices.Sort(out)
	return out
}

// compileTwins compiles written, a document writing a value the rule table
// refuses, and twin, the same document with null written at the node, and holds
// the two to one IR and to findings at the same pointers: only the finding at
// the node differs in its severity.
func compileTwins(t *testing.T, written, twin string) (*ir.Document, []ir.Diagnostic) {
	t.Helper()
	doc, diags := compileText(t, "spec.yaml", written, openapi.Options{})
	twinDoc, twinDiags := compileText(t, "twin.yaml", twin, openapi.Options{})
	require.NotNil(t, doc, "%+v", diags)
	require.NotNil(t, twinDoc, "%+v", twinDiags)
	assert.Equal(t, jsonOf(t, twinDoc), jsonOf(t, doc), "a refused value is read as the field left out, as null is")
	assert.Equal(t, pointersOf(twinDiags), pointersOf(diags), "the findings sit where the twin's do")
	return doc, diags
}

// TestCompile_AValueWhereASchemaBelongsThatIsNoMappingIsNoSchema holds a string,
// a number, a list or a tagged scalar written where a schema belongs to the
// reading of a null there: the table's error alone, no false schema, and the
// IR of its twin.
func TestCompile_AValueWhereASchemaBelongsThatIsNoMappingIsNoSchema(t *testing.T) {
	t.Parallel()
	const comp = "paths: {}\ncomponents:\n  schemas:\n"
	for name, tc := range map[string]struct{ written, twin, pointer string }{
		"a component string": {comp + "    S: hello\n", comp + "    S: ~\n", "/components/schemas/S"},
		"a component list":   {comp + "    S: [a]\n", comp + "    S: ~\n", "/components/schemas/S"},
		"a component number": {comp + "    S: 7\n", comp + "    S: ~\n", "/components/schemas/S"},
		"a tagged boolean":   {comp + "    S: !local true\n", comp + "    S: ~\n", "/components/schemas/S"},
		"a string-tagged boolean": {
			comp + "    S: !!str true\n", comp + "    S: ~\n", "/components/schemas/S",
		},
		"an allOf branch": {
			comp + "    S: {allOf: [hello, {type: object}]}\n", comp + "    S: {allOf: [~, {type: object}]}\n",
			"/components/schemas/S/allOf/0",
		},
		"items": {
			comp + "    S: {type: array, items: 3}\n", comp + "    S: {type: array, items: ~}\n",
			"/components/schemas/S/items",
		},
		"additionalProperties": {
			comp + "    S: {type: object, additionalProperties: hello}\n",
			comp + "    S: {type: object, additionalProperties: ~}\n", "/components/schemas/S/additionalProperties",
		},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			doc, diags := compileTwins(t, head31+tc.written, head31+tc.twin)

			assert.Empty(t, byCode(diags, falseSchema), "the document writes no false: %+v", diags)
			var errs []ir.Diagnostic
			for _, d := range diags {
				if d.Severity == ir.SeverityError {
					errs = append(errs, d)
				}
			}
			require.Len(t, errs, 1, "%+v", diags)
			assert.Equal(t, typeMismatch, errs[0].Code)
			assert.Equal(t, tc.pointer, string(errs[0].Provenance.Pointer))
			assert.NotContains(t, jsonOf(t, doc), `"value":false`, "no false is kept as the source's")
		})
	}
}

// TestCompile_AWrittenFalseSchemaStillDrawsTheInfo holds the other side of the
// reading above: a `false` written where a schema belongs is a false schema.
func TestCompile_AWrittenFalseSchemaStillDrawsTheInfo(t *testing.T) {
	t.Parallel()
	_, diags := compileText(t, "spec.yaml", head31+"paths: {}\ncomponents:\n  schemas:\n    S: false\n", openapi.Options{})

	assert.Len(t, byCode(diags, falseSchema), 1, "%+v", diags)
	assert.Empty(t, byCode(diags, typeMismatch), "%+v", diags)
}

// boolSites is #1793's document: a boolean written as a string at the five
// places the IR tells a declared false from a field left out.
const boolSites = `openapi: 3.1.0
info: {title: t, version: '1'}
paths:
  /a:
    post:
      operationId: a
      parameters:
        - {name: p, in: query, explode: %, schema: {type: array, items: {type: string}}}
      requestBody:
        content:
          application/x-www-form-urlencoded:
            schema: {$ref: '#/components/schemas/F'}
            encoding:
              f: {style: form, explode: %}
      responses:
        '200':
          description: ok
          headers:
            H: {explode: %, schema: {type: array, items: {type: string}}}
components:
  schemas:
    F:
      type: object
      properties:
        f: {type: array, items: {type: string}}
        a: {type: string, xml: {attribute: %}}
        w: {type: array, items: {type: string}, xml: {wrapped: %}}
`

// TestCompile_ABooleanWrittenAsAStringIsReadAsTheFieldLeftOut holds #1793's
// five sites: each is refused as an error, and none states a boolean the
// document never wrote.
func TestCompile_ABooleanWrittenAsAStringIsReadAsTheFieldLeftOut(t *testing.T) {
	t.Parallel()
	for _, written := range []string{`"true"`, `"false"`, `"yes"`, `on`, `"x"`, `!!str true`} {
		t.Run(written, func(t *testing.T) {
			t.Parallel()
			doc, diags := compileTwins(t, strings.ReplaceAll(boolSites, "%", written), strings.ReplaceAll(boolSites, "%", "~"))

			var errs []string
			for _, d := range diags {
				if d.Severity == ir.SeverityError {
					assert.Equal(t, typeMismatch, d.Code)
					errs = append(errs, string(d.Provenance.Pointer))
				}
			}
			slices.Sort(errs)
			assert.Equal(t, []string{
				"/components/schemas/F/properties/a/xml/attribute",
				"/components/schemas/F/properties/w/xml/wrapped",
				"/paths/~1a/post/parameters/0/explode",
				"/paths/~1a/post/requestBody/content/application~1x-www-form-urlencoded/encoding/f/explode",
				"/paths/~1a/post/responses/200/headers/H/explode",
			}, errs)
			for _, key := range []string{`"explode":`, `"attribute":`, `"wrapped":`} {
				assert.False(t, containsJSON(t, doc, key), "no %s is declared: the field is left out", key)
			}
		})
	}
}

// TestCompile_ABooleanWrittenWithAValueOf30IsReadAsLeftOut holds #1793's
// fields beside the ones whose unset and false mean the same: a string for
// required, nullable or deprecated lowers as the field left out, beside its
// error, on 3.0.
func TestCompile_ABooleanWrittenWithAValueOf30IsReadAsLeftOut(t *testing.T) {
	t.Parallel()
	const tmpl = "openapi: 3.0.3\ninfo: {title: t, version: \"1\"}\npaths:\n  /a:\n    get:\n      responses: {\"200\": {description: ok}}\n" +
		"      parameters:\n" +
		"        - {name: p, in: query, required: %1, schema: {type: string, nullable: %2}}\n" +
		"        - {name: q, in: query, required: %3, deprecated: %4, schema: {type: string}}\n"
	fill := func(a, b, c, d string) string {
		return strings.NewReplacer("%1", a, "%2", b, "%3", c, "%4", d).Replace(tmpl)
	}

	doc, diags := compileTwins(t, fill("on", "y", `"yes"`, `"true"`), fill("~", "~", "~", "~"))

	assert.Equal(t, []string{
		"/paths/~1a/get/parameters/0/required",
		"/paths/~1a/get/parameters/0/schema/nullable",
		"/paths/~1a/get/parameters/1/deprecated",
		"/paths/~1a/get/parameters/1/required",
	}, pointersOf(diags))
	for _, d := range diags {
		assert.Equal(t, ir.SeverityError, d.Severity, "%+v", d)
	}
	ops := allOperations(doc)
	require.Len(t, ops, 1)
	require.Len(t, ops[0].Params, 2)
	for _, p := range ops[0].Params {
		assert.False(t, p.Required, "%s is not required: the field is left out", p.Name.Source)
		assert.False(t, p.Type.Nullable, "%s is not nullable", p.Name.Source)
		assert.Nil(t, p.Deprecation, "%s carries no deprecation", p.Name.Source)
	}
}

// taggedSource is #1368's document, whose $ref is one the compiler can follow.
const taggedSource = `openapi: 3.1.0
info: {title: !local t, version: !!timestamp 2021-01-01, summary: !!binary aGk=}
components:
  securitySchemes:
    k: {type: apiKey, in: header, name: X-Key}
  parameters:
    P: {name: p, in: query, schema: {type: string}}
security: !local [{k: []}]
paths:
  /b:
    get:
      operationId: !local b
      tags: [a, !local x, !!binary aGk=]
      parameters:
        - $ref: !local '#/components/parameters/P'
      responses:
        '200': {description: ok}
`

// TestCompile_AScalarOrSequenceTaggedOutsideTheRulesetIsReadAsLeftOut holds
// each tagged typed field of #1368's document to the table's error, saying the
// tag, and to the reading of a field left out.
func TestCompile_AScalarOrSequenceTaggedOutsideTheRulesetIsReadAsLeftOut(t *testing.T) {
	t.Parallel()
	twin := strings.NewReplacer("!local t", "~", "!!timestamp 2021-01-01", "~", "!!binary aGk=", "~",
		"!local [{k: []}]", "~", "!local b", "~", "[a, ~, ~]", "~", "!local '#/components/parameters/P'", "~").Replace(taggedSource)
	require.NotEqual(t, taggedSource, twin)

	doc, diags := compileText(t, "spec.yaml", taggedSource, openapi.Options{})
	twinDoc, _ := compileText(t, "twin.yaml", twin, openapi.Options{})

	require.NotNil(t, doc, "%+v", diags)
	require.NotNil(t, twinDoc)
	assert.Equal(t, jsonOf(t, twinDoc), jsonOf(t, doc), "each tagged field reads as left out: name, version, auth, the operation's name")
	got := map[string]string{}
	for _, d := range diags {
		if d.Severity == ir.SeverityError {
			assert.Equal(t, typeMismatch, d.Code, "%+v", d)
			got[string(d.Provenance.Pointer)] = d.Message
		}
	}
	for _, pointer := range []string{
		"/info/title", "/info/version", "/info/summary", "/security", "/paths/~1b/get/operationId",
		"/paths/~1b/get/tags", "/paths/~1b/get/parameters/0/$ref",
	} {
		msg, ok := got[pointer]
		assert.True(t, ok, "no error at %s: %+v", pointer, diags)
		assert.Contains(t, msg, "outside YAML 1.2's JSON schema ruleset, to which OpenAPI 3.1 limits tags (Format)", pointer)
	}
	assert.Contains(t, got["/info/title"], `it is tagged "!local"`)
	assert.Contains(t, got["/info/summary"], `it is tagged "!!binary"`)
	assert.Empty(t, byCode(diags, "openapi/incomplete-security-scheme"), "no scheme was written with a tag")
}

// TestCompile_ATagInsideTheRulesetDrawsNothing holds the tags YAML 1.2's JSON
// schema ruleset has, a plain date, and an explicit tag where the field's value
// is any, to no finding.
func TestCompile_ATagInsideTheRulesetDrawsNothing(t *testing.T) {
	t.Parallel()
	spec := "openapi: 3.1.0\ninfo: {title: !!str x, version: \"1\", summary: 2021-01-01}\npaths: {}\ncomponents:\n  schemas:\n" +
		"    S: {type: object, properties: {n: {type: integer, maximum: !!int 3, default: !!int 3, example: !!binary aGk=}}}\n"

	doc, diags := compileText(t, "spec.yaml", spec, openapi.Options{})

	require.NotNil(t, doc)
	assert.Empty(t, diags)
}

// TestCompile_ATaggedValueWhereAnyIsAdmittedIsUntouched holds the conformance
// specs that write !!binary where the field takes any value to no finding of
// the type check, so that what a tag means there stays the values' to read.
func TestCompile_ATaggedValueWhereAnyIsAdmittedIsUntouched(t *testing.T) {
	t.Parallel()
	for _, name := range []string{"defaults", "extensions-x", "literal-const"} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			data, err := os.ReadFile(filepath.Join("..", "..", "..", "testdata", "conformance", "openapi", name+".yaml"))
			require.NoError(t, err)
			require.Contains(t, string(data), "!!binary", "the fixture writes the tag this row is about")

			doc, diags := compileText(t, name+".yaml", string(data), openapi.Options{})

			require.NotNil(t, doc)
			assert.Empty(t, byCode(diags, typeMismatch), "%+v", diags)
		})
	}
}

// TestCompile_ANodeASchemaAndAPropertiesMapReadStaysAsWritten holds a node two
// places read, one that refuses it and one that accepts it, to the place that
// accepts it: it is not turned, and the refusing place reports it.
func TestCompile_ANodeASchemaAndAPropertiesMapReadStaysAsWritten(t *testing.T) {
	t.Parallel()
	spec := head31 + "paths: {}\ncomponents:\n  schemas:\n    A: &m {type: string, maxLength: 3}\n    B: {type: object, properties: *m}\n"

	doc, diags := compileText(t, "spec.yaml", spec, openapi.Options{})

	require.NotNil(t, doc)
	assert.Empty(t, byCode(diags, falseSchema), "a scalar that is no boolean is no schema, not a false one: %+v", diags)
	assert.Equal(t, []string{"/components/schemas/A/maxLength", "/components/schemas/A/type"}, pointersOf(byCode(diags, typeMismatch)),
		"the two nodes the properties map refuses are reported once each, at their least place")
	a, ok := typeByName(doc, "A").(*ir.Scalar)
	require.True(t, ok, "A reads the shared node as a schema: it was not turned")
	require.NotNil(t, a.Base)
	assert.Equal(t, ir.TypeID("t/prim/string"), a.Base.Target)
	require.NotNil(t, a.Constraints)
	require.NotNil(t, a.Constraints.MaxLength)
	assert.EqualValues(t, 3, *a.Constraints.MaxLength)
}

// TestCompile_ACycleFixtureWritingASharedNodeAsSchemaAndPropertiesKeepsItsCycle
// holds the committed fixture that mounts one anchored mapping as a schema and
// as a properties map to its cyclic-ref finding, without a false schema.
func TestCompile_ACycleFixtureWritingASharedNodeAsSchemaAndPropertiesKeepsItsCycle(t *testing.T) {
	t.Parallel()

	_, diags := compileText(t, "cycle.yaml", string(readReproducer(t, "cycle_alias_dual_position")), openapi.Options{})

	assert.Len(t, byCode(diags, "openapi/cyclic-ref"), 1, "%+v", diags)
	assert.Empty(t, byCode(diags, falseSchema), "%+v", diags)
}

// srbPathSpec writes a parameter as inline or through an alias.
const srbParam = `{name: q, in: query, required: "yes", explode: "false", schema: {type: array, items: {type: string}}}`

// TestCompile_ASharedNodeIsReadAsItsInlineSpellingIs holds a parameter written
// inline and the same parameter an alias mounts to one reading: not required, no
// explode, the same two errors, and the alias's extension still showing what
// the source wrote.
func TestCompile_ASharedNodeIsReadAsItsInlineSpellingIs(t *testing.T) {
	t.Parallel()
	inline := head31 + "paths:\n  /a:\n    get:\n      parameters:\n        - " + srbParam +
		"\n      responses: {\"200\": {description: ok}}\n"
	aliased := head31 + "x-p: &p " + srbParam + "\npaths:\n  /a:\n    get:\n      parameters:\n        - *p\n" +
		"      responses: {\"200\": {description: ok}}\n"

	inlineDoc, inlineDiags := compileText(t, "inline.yaml", inline, openapi.Options{})
	aliasedDoc, aliasedDiags := compileText(t, "aliased.yaml", aliased, openapi.Options{})

	require.NotNil(t, inlineDoc)
	require.NotNil(t, aliasedDoc)
	for name, doc := range map[string]*ir.Document{"inline": inlineDoc, "aliased": aliasedDoc} {
		ops := allOperations(doc)
		require.Len(t, ops, 1, name)
		require.Len(t, ops[0].Params, 1, name)
		assert.False(t, ops[0].Params[0].Required, "%s: q is not required", name)
		require.NotEmpty(t, ops[0].Bindings.HTTP, name)
		require.Len(t, ops[0].Bindings.HTTP[0].ParamBindings, 1, name)
		assert.Nil(t, ops[0].Bindings.HTTP[0].ParamBindings[0].Explode, "%s: its binding states no explode", name)
	}
	assert.Equal(t, []string{"/paths/~1a/get/parameters/0/explode", "/paths/~1a/get/parameters/0/required"}, pointersOf(inlineDiags))
	assert.Equal(t, pointersOf(inlineDiags), pointersOf(aliasedDiags),
		"the aliased node's findings sit at the least place that mounts it: paths comes before x-p")
	assert.Empty(t, cmp.Diff(strippedTree(t, inlineDoc), strippedTree(t, aliasedDoc)), "equal but for provenance and the extension")
	assert.True(t, containsJSON(t, aliasedDoc, `"required":"yes"`), "the extension shows what the source wrote")
	assert.True(t, containsJSON(t, aliasedDoc, `"explode":"false"`), "the extension shows what the source wrote")
}

// TestCompile_AScalarAValueSharesIsReadAsTheRefusingPlaceReadsIt holds a scalar
// a parameter's required refuses, which an alias also mounts where a value
// belongs, to the field left out, as with no second use: a value reads no type,
// so it does not keep the node as written for the field, and still renders
// what the source wrote.
func TestCompile_AScalarAValueSharesIsReadAsTheRefusingPlaceReadsIt(t *testing.T) {
	t.Parallel()
	const required = "/paths/~1a/get/parameters/0/required"
	for name, row := range map[string]struct{ use, at string }{
		"schema default": {"schema: {type: string, default: *r}", required},
		"schema example": {"schema: {type: string, example: *r}", required},
		"schema const":   {"schema: {type: string, const: *r}", required},
		"enum member":    {"schema: {type: string, enum: [*r]}", required},
		// The finding sits at the least place that mounts the node.
		"parameter example": {"schema: {type: string}, example: *r", "/paths/~1a/get/parameters/0/example"},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			spec := head31 + "x-flag: &r \"yes\"\npaths:\n  /a:\n    get:\n      parameters:\n        - {name: a, in: query, required: *r, " +
				row.use + "}\n      responses: {\"200\": {description: ok}}\n"

			doc, diags := compileText(t, "spec.yaml", spec, openapi.Options{})

			require.NotNil(t, doc)
			assert.Equal(t, []string{row.at}, pointersOf(diags), "%+v", diags)
			ops := allOperations(doc)
			require.Len(t, ops, 1)
			require.Len(t, ops[0].Params, 1)
			assert.False(t, ops[0].Params[0].Required, "required is read as left out")
			assert.True(t, containsJSON(t, doc, `"str":"yes"`), "the value renders what the source wrote")
		})
	}
}

// strippedTree is doc's encoding without what a comparison of two spellings of
// one parameter must leave out: provenance, the extensions and the findings.
func strippedTree(t *testing.T, doc *ir.Document) any {
	t.Helper()
	stripped := *doc
	stripped.Diagnostics = nil
	stripped.Sources = nil
	var drop func(v any) any
	drop = func(v any) any {
		switch x := v.(type) {
		case map[string]any:
			delete(x, "provenance")
			delete(x, "unmodeled")
			for k, c := range x {
				x[k] = drop(c)
			}
		case []any:
			for i, c := range x {
				x[i] = drop(c)
			}
		}
		return v
	}
	return drop(irTree(t, &stripped))
}

// TestCompile_AVerbatimCopyShowsWhatTheSourceWrote holds a construct kept
// verbatim beside a refused value in it to the text written: the error stands
// at the node, and the copy renders the string, not the null it is read as.
func TestCompile_AVerbatimCopyShowsWhatTheSourceWrote(t *testing.T) {
	t.Parallel()
	spec := head31 + "paths: {}\ncomponents:\n  schemas:\n    S: {type: string, not: {maxLength: hello}}\n"

	doc, diags := compileText(t, "spec.yaml", spec, openapi.Options{})

	require.NotNil(t, doc)
	require.Len(t, byCode(diags, typeMismatch), 1, "%+v", diags)
	assert.Equal(t, "/components/schemas/S/not/maxLength", string(byCode(diags, typeMismatch)[0].Provenance.Pointer))
	assert.True(t, containsJSON(t, doc, `{"maxLength":"hello"}`), "the verbatim copy renders what was written")
	assert.False(t, containsJSON(t, doc, `{"maxLength":null}`), "and never the null the field is read as")
}
