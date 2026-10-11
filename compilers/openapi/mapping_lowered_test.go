// This file is a package-level suite, not a per-source-file test: it holds the
// load phase's resolution of mapping targets to what the lowering reads, so it
// has no single source file to pair with.
package openapi_test // external test package — exercises only the public API

import (
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"testing"

	"github.com/google/go-cmp/cmp"
	"github.com/speakeasy-api/openapi/jsonschema/oas3/core"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dexpace/morphic/compilers"
	"github.com/dexpace/morphic/compilers/openapi"
	"github.com/dexpace/morphic/ir"
)

// namesT is a discriminated object whose one mapping entry names the raw schema
// at #/x-lib/T, which nothing else in a document names.
const namesT = "{type: object, properties: {k: {type: string}}, " +
	"discriminator: {propertyName: k, mapping: {x: '#/x-lib/T'}}}"

// readCase is a document placing namesT somewhere: its paths, component
// schemas, other components and further x-lib entries. unreached is set for a
// document in which nothing the load phase resolves reaches namesT.
type readCase struct {
	name, paths, schemas, components, lib string
	unreached                             bool
}

// doc writes c's document. T carries a finding, which the load phase reports
// once it resolves T, and the lowering interns T once it reads the entry.
func (c readCase) doc() string {
	paths := " {}"
	if c.paths != "" {
		paths = "\n" + c.paths
	}
	schemas := c.schemas
	if schemas == "" {
		schemas = "    Z: {type: object}\n"
	}
	return "openapi: 3.1.0\ninfo: {title: T, version: \"1\"}\npaths:" + paths + "\ncomponents:\n  schemas:\n" +
		schemas + c.components + "x-lib:\n  T: {type: object, minLength: abc}\n" + c.lib
}

// readAndReported compiles src and reports whether the lowering read the
// discriminator naming T, interning T or reporting the entry unresolved, and
// whether the finding in T was reported, which the load phase draws only by
// resolving T.
func readAndReported(t *testing.T, src string) (read, reported bool) {
	t.Helper()
	doc, diags, err := openapi.New().Compile(t.Context(), []compilers.Source{{Path: "spec.yaml", Data: []byte(src)}},
		compilers.Options{})
	require.NoError(t, err)
	require.NotNil(t, doc)
	_, read = doc.Types["t/anon/x-lib/T"]
	for _, d := range diags {
		switch {
		case d.Code == "openapi/unresolved-ref" && strings.HasSuffix(string(d.Provenance.Pointer), "/mapping/x"):
			read = true
		case d.Code == "openapi/validation/validation-type-mismatch":
			reported = true
		}
	}
	return read, reported
}

// schemaKeywordCases returns a case per keyword the parser reads a schema
// under, namesT written under it in the shape its value takes: one schema, a
// list or a map of them. The keywords are read off the parser's own model, so
// one it comes to read is held here without being listed.
func schemaKeywordCases(t *testing.T) []readCase {
	t.Helper()
	typeOf := map[string]string{
		"properties": "object", "patternProperties": "object", "additionalProperties": "object",
		"dependentSchemas": "object", "propertyNames": "object", "unevaluatedProperties": "object",
		"$defs": "object", "items": "array", "prefixItems": "array", "contains": "array",
		"unevaluatedItems": "array", "contentSchema": "string",
	}
	schema := reflect.TypeFor[core.Schema]()
	var cases []readCase
	for i := range schema.NumField() {
		field := schema.Field(i)
		keyword, shape := field.Tag.Get("key"), field.Type.String()
		if !strings.Contains(shape, "jsonschema/oas3/core.Schema,bool]") {
			continue
		}
		value := namesT
		switch {
		case strings.Contains(shape, "sequencedmap.Map"):
			value = "{p: " + namesT + "}"
		case strings.HasPrefix(shape, "marshaller.Node[[]"):
			value = "[" + namesT + ", {type: object}]"
		}
		typed := ""
		if typ := typeOf[keyword]; typ != "" {
			typed = "type: " + typ + ", "
		}
		cases = append(cases, readCase{name: "under " + keyword, schemas: "    A: {" + typed + keyword + ": " + value + "}\n"})
	}
	require.GreaterOrEqual(t, len(cases), 19, "every schema-valued keyword the parser reads")
	return cases
}

// structureCases are the places a discriminator stands that a keyword alone
// does not decide: a component, a definition, what a $ref reaches or stands
// beside, an election's loser, and what the operation walk lowers.
func structureCases() []readCase {
	response := "{description: x, content: {application/json: {schema: " + namesT + "}}}"
	return []readCase{
		{name: "a component", schemas: "    A: " + namesT + "\n"},
		{name: "a definition a property names by pointer",
			schemas: "    A: {type: object, $defs: {U: " + namesT + "}, properties: {r: {$ref: '#/components/schemas/A/$defs/U'}}}\n"},
		{name: "a definition a property names by the relative rule",
			schemas: "    A: {type: object, $defs: {U: " + namesT + "}, properties: {r: {$ref: '#/$defs/U'}}}\n"},
		{name: "a definition only not names",
			schemas: "    A: {type: object, $defs: {U: " + namesT + "}, not: {$ref: '#/components/schemas/A/$defs/U'}}\n"},
		{name: "what not holds, which a property names",
			schemas: "    A: {type: object, not: " + namesT + ", properties: {r: {$ref: '#/components/schemas/A/not'}}}\n"},
		{name: "beside a $ref", schemas: "    A: {$ref: '#/components/schemas/B', " +
			"discriminator: {propertyName: k, mapping: {x: '#/x-lib/T'}}}\n    B: {type: object}\n"},
		{name: "under a keyword beside a $ref",
			schemas: "    A: {$ref: '#/components/schemas/B', properties: {p: " + namesT + "}}\n    B: {type: object}\n"},
		{name: "under a keyword beside a $defs $ref", schemas: "    A: {type: object, $defs: {D: {type: object}}, " +
			"properties: {r: {$ref: '#/$defs/D', properties: {p: " + namesT + "}}}}\n"},
		{name: "a schema under not a property names", schemas: "    A: {type: object, not: {type: object, " +
			"properties: {p: " + namesT + "}}, properties: {r: {$ref: '#/components/schemas/A/not/properties/p'}}}\n"},
		{name: "beside a schema under not a property names", schemas: "    A: {type: object, not: {type: object, " +
			"properties: {p: {type: object}, q: " + namesT + "}}, properties: {r: {$ref: '#/components/schemas/A/not/properties/p'}}}\n"},
		{name: "a property of an inline allOf branch", schemas: "    A: {allOf: [{type: object, properties: {p: " + namesT + "}}]}\n"},
		{name: "an inline allOf branch a $ref names",
			schemas: "    A: {allOf: [" + namesT + "]}\n    B: {$ref: '#/components/schemas/A/allOf/0'}\n"},
		{name: "items beside prefixItems", schemas: "    A: {type: array, prefixItems: [{type: string}], items: " + namesT + "}\n"},
		{name: "anyOf beside oneOf", schemas: "    A: {oneOf: [{type: string}, {type: integer}], anyOf: [" + namesT + ", {type: string}]}\n"},
		{name: "allOf beside enum", schemas: "    A: {enum: [a], allOf: [{type: object, properties: {p: " + namesT + "}}]}\n"},
		{name: "allOf beside const", schemas: "    A: {const: a, allOf: [{type: object, properties: {p: " + namesT + "}}]}\n"},
		{name: "a response nothing names", components: "  responses:\n    R: " + response + "\n"},
		{name: "a response a path names", paths: "  /a: {get: {responses: {'200': {$ref: '#/components/responses/R'}}}}",
			components: "  responses:\n    R: " + response + "\n"},
		{name: "a response only an unnamed response names",
			components: "  responses:\n    R: " + response + "\n    S: {$ref: '#/components/responses/R'}\n"},
		{name: "a parameter nothing names", components: "  parameters:\n    P: {name: q, in: query, schema: " + namesT + "}\n"},
		{name: "a header nothing names", components: "  headers:\n    H: {schema: " + namesT + "}\n"},
		{name: "a request body nothing names",
			components: "  requestBodies:\n    B: {content: {application/json: {schema: " + namesT + "}}}\n"},
		{name: "a path item nothing names",
			components: "  pathItems:\n    I: {get: {responses: {'200': " + response + "}}}\n"},
		{name: "a callback nothing names",
			components: "  callbacks:\n    C: {'{$url}': {post: {responses: {'200': " + response + "}}}}\n"},
		{name: "a path's response", paths: "  /a: {get: {responses: {'200': " + response + "}}}"},
		{name: "a webhook's response", paths: "  /a: {get: {responses: {'200': {description: ok}}}}\nwebhooks:\n" +
			"  hook: {post: {responses: {'200': " + response + "}}}"},
		{name: "a parameter's schema",
			paths: "  /a: {get: {parameters: [{name: q, in: query, schema: " + namesT + "}], responses: {'200': {description: ok}}}}"},
		{name: "a parameter's schema beside its content", paths: "  /a: {get: {parameters: [{name: q, in: query, " +
			"schema: " + namesT + ", content: {application/json: {schema: {type: string}}}}], responses: {'200': {description: ok}}}}"},
		{name: "a parameter's second content entry", paths: "  /a: {get: {parameters: [{name: q, in: query, " +
			"content: {text/plain: {schema: {type: string}}, application/json: {schema: " + namesT + "}}}], " +
			"responses: {'200': {description: ok}}}}"},
		{name: "a header's schema beside its content", paths: "  /a: {get: {responses: {'200': {description: ok, " +
			"headers: {X-H: {schema: " + namesT + ", content: {application/json: {schema: {type: string}}}}}}}}}"},
		{name: "a raw object a $ref reaches", schemas: "    A: {$ref: '#/x-lib/W'}\n",
			lib: "  W: {type: object, properties: {p: " + namesT + "}}\n"},
		{name: "what not holds in a raw object a $ref reaches", schemas: "    A: {$ref: '#/x-lib/W'}\n",
			lib: "  W: {type: object, not: " + namesT + "}\n"},
		{name: "a raw object only not reaches", schemas: "    A: {type: object, not: {$ref: '#/x-lib/W'}}\n",
			lib: "  W: {type: object, properties: {p: " + namesT + "}}\n"},
		{name: "a raw object nothing reaches", unreached: true,
			lib: "  W: {type: object, properties: {p: " + namesT + "}}\n"},
		{name: "a scalar's", schemas: "    A: {type: string, discriminator: {propertyName: k, mapping: {x: '#/x-lib/T'}}}\n"},
		{name: "an untyped schema's", schemas: "    A: {discriminator: {propertyName: k, mapping: {x: '#/x-lib/T'}}}\n"},
		{name: "a nullable union's that collapses",
			schemas: "    A: {oneOf: [{type: object, properties: {k: {type: string}}}, {type: 'null'}], " +
				"discriminator: {propertyName: k, mapping: {x: '#/x-lib/T'}}}\n"},
	}
}

// TestMappingTargets_TheLoadPhaseResolvesEveryEntryItReaches holds the load
// phase's resolution of mapping targets to what the lowering reads: wherever a
// discriminator stands that the model or a $ref's chain reaches, the finding in
// a target is reported, as a $ref's is. The lowering reads some of those
// positions and not others, as each row's answer shows, and what it reads is
// never left to what it lowered first (GitHub #757). The keyword rows come from
// the parser's model, so a keyword it comes to read reddens a row.
func TestMappingTargets_TheLoadPhaseResolvesEveryEntryItReaches(t *testing.T) {
	t.Parallel()
	cases := slices.Concat(schemaKeywordCases(t), structureCases())
	seen := map[bool]bool{}
	for _, c := range cases {
		read, reported := readAndReported(t, c.doc())
		seen[read] = true
		assert.Equal(t, !c.unreached, reported, "%s: the finding in T is reported unless nothing reaches the entry", c.name)
		if read {
			assert.True(t, reported, "%s: the lowering read the entry, so its target is resolved", c.name)
		}
	}
	assert.Equal(t, map[bool]bool{false: true, true: true}, seen, "the rows tell both answers apart")
}

// TestMappingTargets_ARawReferenceResolvesInEitherOrder pins that a $ref inside
// raw YAML a $ref reaches resolves whichever is declared first: Box, reaching
// the $ref, or Animal, whose mapping names its target. Declared as below, the
// $ref was lowered before the mapping had interned Dog and was reported
// unresolved; reversed, it resolved through the node the mapping interned.
func TestMappingTargets_ARawReferenceResolvesInEitherOrder(t *testing.T) {
	t.Parallel()
	components := []string{
		"    Box: {$ref: '#/x-defs/Wrapper'}\n",
		"    Animal:\n      type: object\n      properties: {kind: {type: string}}\n" +
			"      discriminator: {propertyName: kind, mapping: {dog: '#/x-defs/Dog'}}\n",
	}
	const raw = "x-defs:\n  Wrapper: {type: object, properties: {pet: {$ref: '#/x-defs/Dog'}}}\n" +
		"  Dog: {allOf: [{$ref: '#/components/schemas/Animal'}], type: object, properties: {bark: {type: string}}}\n"
	orders := [][]string{components, {components[1], components[0]}}
	types := make([]ir.TypeRegistry, 0, len(orders))
	for _, order := range orders {
		src := "openapi: 3.1.0\ninfo: {title: T, version: \"1\"}\npaths: {}\ncomponents:\n  schemas:\n" +
			strings.Join(order, "") + raw
		doc, diags, err := openapi.New().Compile(t.Context(), []compilers.Source{{Path: "spec.yaml", Data: []byte(src)}},
			compilers.Options{})
		require.NoError(t, err)
		for _, d := range diags {
			assert.NotEqual(t, ir.SeverityError, d.Severity, "%s %s: %s", d.Code, d.Provenance.Pointer, d.Message)
		}
		types = append(types, doc.Types)
	}
	assert.Empty(t, cmp.Diff(types[0], types[1]), "declaration order changed the registry")
	assert.Contains(t, types[0], ir.TypeID("t/anon/x-defs/Dog"))
}

// TestMappingTargets_AReferenceTheLoadPhaseRefusedIsReportedInEitherOrder pins
// that the schema walked at a raw position answers only a $ref that reached a
// copy of it: C's $ref, spelled with the file's name and external references
// off, is refused, and whether the load phase reports it follows whether M's
// chain passed it first. Answered by the walked schema, C resolved in the
// order where the load phase reported nothing, so its report followed order.
func TestMappingTargets_AReferenceTheLoadPhaseRefusedIsReportedInEitherOrder(t *testing.T) {
	t.Parallel()
	schemas := []string{"    C: {$ref: 'spec.yaml#/components/schemas/H/default'}\n",
		"    H: {default: {$ref: '#/components/schemas/C', description: d}}\n",
		"    M: {not: {$ref: '#/components/schemas/H/default'}}\n"}
	reversed := slices.Clone(schemas)
	slices.Reverse(reversed)
	reports := make([][]string, 0, 2)
	for _, order := range [][]string{schemas, reversed} {
		src := "openapi: 3.1.0\ninfo: {title: T, version: \"1\"}\npaths: {}\ncomponents:\n  schemas:\n" +
			strings.Join(order, "")
		_, diags, err := openapi.New().Compile(t.Context(), []compilers.Source{{Path: "spec.yaml", Data: []byte(src)}},
			compilers.Options{})
		require.NoError(t, err)
		var errs []string
		for _, d := range diags {
			if d.Severity == ir.SeverityError {
				errs = append(errs, string(d.Provenance.Pointer)+" "+d.Code)
			}
		}
		slices.Sort(errs)
		reports = append(reports, errs)
	}
	assert.Empty(t, cmp.Diff(reports[0], reports[1]), "declaration order changed the report")
	assert.Contains(t, reports[1], "/components/schemas/C openapi/unresolved-ref")
}

// TestMappingTargets_AReferenceInACopyResolvesInEitherOrder pins a $ref inside
// a raw object two references reach: an internal one, which builds the object
// from the model, and one from another document by the source's file name,
// which builds it from the source's tree. Its pointer passes a path item's
// $ref, which the tree does not read through, so resolved against whichever
// copy was walked first, Y's $ref was reported unresolved in one order only.
func TestMappingTargets_AReferenceInACopyResolvesInEitherOrder(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, "other.yaml"),
		[]byte("components:\n  schemas:\n    V: {$ref: 'root.yaml#/x-lib/Y0'}\n"), 0o600))
	schemas := []string{"    W: {$ref: 'other.yaml#/components/schemas/V'}\n", "    Z: {$ref: '#/x-lib/Y'}\n"}
	const rest = "  pathItems:\n    P:\n      get:\n        responses:\n          '200':\n" +
		"            description: ok\n            content: {application/json: {schema: {type: string}}}\n" +
		"x-lib:\n  Y0: {$ref: '#/x-lib/Y'}\n  Y:\n    type: object\n" +
		"    properties:\n      q: {$ref: '#/paths/~1p/get/responses/200/content/application~1json/schema'}\n"
	for _, order := range [][]string{schemas, {schemas[1], schemas[0]}} {
		src := "openapi: 3.1.0\ninfo: {title: t, version: '1'}\npaths:\n  /p: {$ref: '#/components/pathItems/P'}\n" +
			"components:\n  schemas:\n" + strings.Join(order, "") + rest
		_, diags, err := openapi.New().Compile(t.Context(),
			[]compilers.Source{{Path: filepath.Join(dir, "root.yaml"), Data: []byte(src)}},
			compilers.Options{FormatOptions: openapi.Options{AllowExternalRefs: true}})
		require.NoError(t, err)
		for _, d := range diags {
			assert.NotEqual(t, "/x-lib/Y/properties/q", string(d.Provenance.Pointer), "%s: %s", d.Code, d.Message)
		}
	}
}

// TestMappingTargets_ARawPositionACopyReachesResolvesInEitherOrder pins a
// position two references reach as two objects: a $ref spelled with the
// source's file name, or one from another document, builds a copy of what it
// names, beside the object an internal $ref or a mapping reaches. The load
// phase resolved the $refs in one of them, and the lowering hoisted whichever
// it met first, so Cat's own $ref resolved in one order only.
func TestMappingTargets_ARawPositionACopyReachesResolvesInEitherOrder(t *testing.T) {
	t.Parallel()
	const lib = "x-lib:\n  Cat: {allOf: [{$ref: '#/components/schemas/Pet'}], type: object, " +
		"properties: {friend: {$ref: '#/x-lib/Dog'}}}\n  Dog: {type: object}\n" +
		"  Via: {$ref: '#/components/schemas/Home/properties/cat'}\n"
	pet := "    Pet:\n      type: object\n      required: [kind]\n      properties: {kind: {type: string}}\n" +
		"      discriminator: {propertyName: kind, mapping: {cat: '#/x-lib/Cat'}}\n"
	for name, components := range map[string][]string{
		"a mapping and a $ref by the file's name": {pet, "    Box: {$ref: 'spec.yaml#/x-lib/Cat'}\n"},
		"a $ref and one from another document": {"    Box: {$ref: '#/x-lib/Cat'}\n",
			"    Far: {$ref: 'other.yaml#/components/schemas/Back'}\n"},
		"a mapping and a chain by the file's name to a position the model declares": {
			strings.Replace(pet, "'#/x-lib/Cat'", "'#/components/schemas/Home/properties/cat'", 1),
			"    Y: {$ref: 'spec.yaml#/x-lib/Via'}\n    Home:\n      type: object\n      properties:\n" +
				"        cat: {allOf: [{$ref: '#/components/schemas/Pet'}], type: object, properties: {friend: {$ref: '#/x-lib/Dog'}}}\n"},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			dir := t.TempDir()
			require.NoError(t, os.WriteFile(filepath.Join(dir, "other.yaml"), []byte(
				"components:\n  schemas:\n    Back: {$ref: 'spec.yaml#/x-lib/Cat'}\n"), 0o600))
			orders := [][]string{components, {components[1], components[0]}}
			types := make([]ir.TypeRegistry, 0, len(orders))
			for _, order := range orders {
				src := "openapi: 3.1.0\ninfo: {title: T, version: \"1\"}\npaths: {}\ncomponents:\n  schemas:\n" +
					strings.Join(order, "") + lib
				doc, diags, err := openapi.New().Compile(t.Context(),
					[]compilers.Source{{Path: filepath.Join(dir, "spec.yaml"), Data: []byte(src)}},
					compilers.Options{FormatOptions: openapi.Options{AllowExternalRefs: true}})
				require.NoError(t, err)
				for _, d := range diags {
					if strings.HasSuffix(string(d.Provenance.Pointer), "/properties/friend") {
						assert.Fail(t, "Cat's own $ref is unresolved", "%s: %s", d.Code, d.Message)
					}
				}
				types = append(types, doc.Types)
			}
			assert.Empty(t, cmp.Diff(types[0], types[1]), "declaration order changed the registry")
			assert.Contains(t, types[0], ir.TypeID("t/anon/x-lib/Dog"))
		})
	}
}
