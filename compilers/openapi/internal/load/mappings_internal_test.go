package load

import (
	"context"
	"encoding/json/jsontext"
	"fmt"
	"iter"
	"path/filepath"
	"strings"
	"testing"

	"github.com/google/go-cmp/cmp"
	oas3 "github.com/speakeasy-api/openapi/jsonschema/oas3"
	soa "github.com/speakeasy-api/openapi/openapi"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dexpace/morphic/compilers"
	"github.com/dexpace/morphic/compilers/openapi/internal/diag"
	"github.com/dexpace/morphic/compilers/openapi/internal/openapitest"
	"github.com/dexpace/morphic/compilers/openapi/internal/overlay"
	"github.com/dexpace/morphic/ir"
)

// mappingSpec is a source whose component schemas are schemas, and whose
// other top-level entries are rest, which follows them.
func mappingSpec(schemas, rest string) string {
	return openapitest.ComponentSpec(schemas) + rest
}

// petMapping is a Pet base whose discriminator maps each tag to its target.
func petMapping(mapping string) string {
	return "    Pet:\n      type: object\n      discriminator: {propertyName: k, mapping: {" + mapping + "}}\n"
}

// loadTargets loads spec at path, external references allowed or not, and
// requires it to load.
func loadTargets(t *testing.T, path, spec string, external bool) (*Document, []ir.Diagnostic) {
	t.Helper()
	got, diags, err := Load(t.Context(), 0, compilers.Source{Path: path, Data: []byte(spec)},
		Options{AllowExternalRefs: external})
	require.NoError(t, err)
	require.NotNil(t, got, "%+v", diags)
	return got, diags
}

// describedAs returns the description of the schema at pointer in targets, or
// "" for none, which is how a test tells which declaration it was built from.
func describedAs(targets MappingTargets, pointer jsontext.Pointer) string {
	return targets.At(pointer).GetSchema().GetDescription()
}

// TestMappings_ATargetHeldAsRawYAMLIsResolvedAtLoad pins GitHub #757 at its
// source: each mapping target the model holds as raw YAML is resolved, as a
// $ref to it is, whether or not any $ref names it. Each row names it from a
// discriminator the lowering can meet: in the model, in what a $ref reaches,
// in a response a $ref reaches, and in another such target.
func TestMappings_ATargetHeldAsRawYAMLIsResolvedAtLoad(t *testing.T) {
	t.Parallel()
	const lib = `x-lib:
  Cat: {description: cat, type: object}
  Inner:
    type: object
    discriminator: {propertyName: k, mapping: {d: '#/x-lib/Dog'}}
  Dog: {description: dog, type: object}
  Resp:
    description: ok
    content:
      application/json:
        schema: {type: object, discriminator: {propertyName: k, defaultMapping: '#/x-lib/Dog'}}
`
	const toResp = "paths:\n  /a:\n    get:\n      responses:\n        \"200\": {$ref: '#/x-lib/Resp'}\n"
	for _, c := range []struct {
		name, schemas string
		paths         string // the document's paths, when not empty
		pointer       jsontext.Pointer
		want          string
	}{
		{"an extension's value, named by a mapping alone", petMapping("c: '#/x-lib/Cat'"), "", "/x-lib/Cat", "cat"},
		{"an enum member",
			petMapping("e: '#/components/schemas/Kennel/properties/e/enum/0'") +
				"    Kennel: {type: object, properties: {e: {enum: [{description: member, type: object}]}}}\n",
			"", "/components/schemas/Kennel/properties/e/enum/0", "member"},
		{"a mapping in what a $ref reaches", "    Holder: {$ref: '#/x-lib/Inner'}\n", "", "/x-lib/Dog", "dog"},
		{"a mapping in another target", petMapping("i: '#/x-lib/Inner'"), "", "/x-lib/Dog", "dog"},
		{"a defaultMapping in a response a $ref reaches", "    Pet: {type: object}\n", toResp, "/x-lib/Dog", "dog"},
	} {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			spec := mappingSpec(c.schemas, lib)
			if c.paths != "" {
				spec = strings.Replace(spec, "paths: {}\n", c.paths, 1)
			}
			got, diags := loadTargets(t, "spec.yaml", spec, false)

			assert.Empty(t, diags)
			assert.Equal(t, c.want, describedAs(got.Targets, c.pointer))
		})
	}
}

// badDiscriminator is a discriminated object whose one mapping entry names the
// raw schema at #/x-lib/Bad, which carries a finding.
const badDiscriminator = "{type: object, properties: {k: {type: string}}, " +
	"discriminator: {propertyName: k, mapping: {x: '#/x-lib/Bad'}}}"

// TestMappings_ADiscriminatorNothingLowersIsNotResolved pins that the load
// phase resolves no target of a discriminator the lowering never reads: under
// a keyword it keeps verbatim (ir-design §4.7), in a definition or component
// nothing it lowers references, beside a $ref, or in an inline allOf branch,
// whose properties alone it merges. Resolving such a target reported the
// finding in Bad, failing a compile over content nothing lowers.
func TestMappings_ADiscriminatorNothingLowersIsNotResolved(t *testing.T) {
	t.Parallel()
	const lib = "x-lib:\n  Bad: {type: object, minLength: abc}\n"
	d := badDiscriminator
	keyword := func(k, typ string) string { return "    A: {type: " + typ + ", " + k + ": " + d + "}\n" }
	component := func(kind, entry string) string {
		return "  " + kind + ":\n    E: " + entry + "\n"
	}
	content := "{content: {application/json: {schema: " + d + "}}}"
	op := func(get string) string { return "paths:\n  /a: {get: {" + get + "}}\n" }
	for _, c := range []struct {
		name, schemas, components, paths, lib string
		// source is what the source's own validation reports, as "pointer code".
		source []string
	}{
		{name: "not", schemas: keyword("not", "object")},
		{name: "if", schemas: keyword("if", "object")},
		{name: "then", schemas: keyword("then", "object")},
		{name: "else", schemas: keyword("else", "object")},
		{name: "dependentSchemas", schemas: "    A: {type: object, dependentSchemas: {p: " + d + "}}\n"},
		{name: "propertyNames", schemas: keyword("propertyNames", "object")},
		{name: "contains", schemas: keyword("contains", "array")},
		{name: "unevaluatedItems", schemas: keyword("unevaluatedItems", "array")},
		{name: "unevaluatedProperties", schemas: keyword("unevaluatedProperties", "object")},
		{name: "a definition nothing references", schemas: "    A: {type: object, $defs: {U: " + d + "}}\n"},
		{name: "beside a schema under a validation-only keyword a property names",
			schemas: "    A: {type: object, not: {type: object, properties: {p: {type: object}, q: " + d + "}}, " +
				"properties: {r: {$ref: '#/components/schemas/A/not/properties/p'}}}\n"},
		{name: "a definition only a validation-only keyword references",
			schemas: "    A: {type: object, $defs: {U: " + d + "}, not: {$ref: '#/components/schemas/A/$defs/U'}}\n"},
		{name: "beside a $ref", schemas: "    A: {$ref: '#/components/schemas/B', " +
			"discriminator: {propertyName: k, mapping: {x: '#/x-lib/Bad'}}}\n    B: {type: object}\n"},
		{name: "under a keyword beside a $ref",
			schemas: "    A: {$ref: '#/components/schemas/B', properties: {p: " + d + "}}\n    B: {type: object}\n"},
		{name: "under a keyword beside a held $defs $ref",
			schemas: "    A: {type: object, $defs: {D: {type: object}}, properties: {r: {$ref: '#/$defs/D', properties: {p: " +
				d + "}}}}\n"},
		{name: "an inline allOf branch", schemas: "    A: {allOf: [" + d + "]}\n"},
		{name: "under an inline allOf branch's items", schemas: "    A: {allOf: [{type: array, items: " + d + "}]}\n"},
		{name: "under an inline allOf branch's union", schemas: "    A: {allOf: [{oneOf: [" + d + ", {type: string}]}]}\n"},
		{name: "items beside prefixItems", schemas: "    A: {type: array, prefixItems: [{type: string}], items: " + d + "}\n"},
		{name: "anyOf beside oneOf", schemas: "    A: {oneOf: [{type: string}, {type: integer}], anyOf: [" + d + "]}\n"},
		{name: "allOf beside enum", schemas: "    A: {enum: [a], allOf: [{type: object, properties: {p: " + d + "}}]}\n"},
		{name: "a parameter's schema beside its content", paths: op("parameters: [{name: q, in: query, schema: " + d +
			", content: {application/json: {schema: {type: string}}}}], responses: {'200': {description: ok}}")},
		{name: "a parameter's second content entry", paths: op("parameters: [{name: q, in: query, content: " +
			"{text/plain: {schema: {type: string}}, application/json: {schema: " + d + "}}}], responses: {'200': {description: ok}}"),
			source: []string{" openapi/validation/validation-allowed-values"}},
		{name: "a header's schema beside its content", paths: op("responses: {'200': {description: ok, headers: " +
			"{X-H: {schema: " + d + ", content: {application/json: {schema: {type: string}}}}}}}")},
		{name: "a response nothing references", components: component("responses", "{description: x, content: "+
			"{application/json: {schema: "+d+"}}}")},
		{name: "a response only another unreferenced one references",
			components: "  responses:\n    E: {description: x, content: {application/json: {schema: " + d + "}}}\n" +
				"    F: {$ref: '#/components/responses/E'}\n"},
		{name: "a parameter nothing references", components: component("parameters", "{name: q, in: query, schema: "+d+"}")},
		{name: "a header nothing references", components: component("headers", "{schema: "+d+"}")},
		{name: "a request body nothing references", components: component("requestBodies", content)},
		{name: "a path item nothing references", components: component("pathItems",
			"{get: {responses: {'200': {description: ok, content: {application/json: {schema: "+d+"}}}}}}")},
		{name: "in a raw object a $ref reaches, under not", schemas: "    A: {$ref: '#/x-lib/W'}\n",
			lib: "  W: {type: object, not: " + d + "}\n"},
		{name: "in a raw object only a validation-only keyword reaches",
			schemas: "    A: {type: object, not: {$ref: '#/x-lib/W'}}\n", lib: "  W: {type: object, properties: {p: " + d + "}}\n"},
	} {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			schemas := c.schemas
			if schemas == "" {
				schemas = "    Z: {type: object}\n"
			}
			spec := mappingSpec(schemas, c.components+lib+c.lib)
			if c.paths != "" {
				spec = strings.Replace(spec, "paths: {}\n", c.paths, 1)
			}
			got, diags := loadTargets(t, "spec.yaml", spec, false)

			assert.ElementsMatch(t, c.source, diagLines(diags), "the finding in Bad is in content nothing lowers")
			assert.Nil(t, got.Targets.At("/x-lib/Bad"))
		})
	}
}

// TestMappings_ADiscriminatorALoweredReferenceReachesIsResolved pins the other
// half: content the lowering reaches only through a reference, or a mapping
// target, is lowered from there, so the targets of a discriminator in it are
// resolved, wherever that reference stands in the document.
func TestMappings_ADiscriminatorALoweredReferenceReachesIsResolved(t *testing.T) {
	t.Parallel()
	d := "{type: object, properties: {k: {type: string}}, discriminator: {propertyName: k, mapping: {x: '#/x-lib/T'}}}"
	const lib = "x-lib:\n  T: {description: marker, type: object}\n"
	respond := "paths:\n  /a: {get: {responses: {'200': {$ref: '#/components/responses/F'}}}}\n"
	for _, c := range []struct{ name, schemas, components, paths, lib string }{
		{name: "a definition a property names by pointer",
			schemas: "    A: {type: object, $defs: {U: " + d + "}, properties: {r: {$ref: '#/components/schemas/A/$defs/U'}}}\n"},
		{name: "a definition a property names by the relative rule",
			schemas: "    A: {type: object, $defs: {U: " + d + "}, properties: {r: {$ref: '#/$defs/U'}}}\n"},
		{name: "a definition named before the reference reaching it",
			schemas: "    A: {type: object, $defs: {U: " + d + "}}\n    B: {$ref: '#/components/schemas/A/$defs/U'}\n"},
		{name: "a definition a mapping target reaches",
			schemas: "    A: {type: object, $defs: {U: " + d + "}}\n" +
				"    P: {type: object, properties: {k: {type: string}}, " +
				"discriminator: {propertyName: k, mapping: {u: '#/components/schemas/A/$defs/U'}}}\n"},
		{name: "a validation-only keyword's schema a property names",
			schemas: "    A: {type: object, not: " + d + ", properties: {r: {$ref: '#/components/schemas/A/not'}}}\n"},
		{name: "a schema under a validation-only keyword a property names",
			schemas: "    A: {type: object, not: {type: object, properties: {p: " + d + "}}, " +
				"properties: {r: {$ref: '#/components/schemas/A/not/properties/p'}}}\n"},
		{name: "an inline allOf branch a $ref names",
			schemas: "    A: {allOf: [" + d + "]}\n    B: {$ref: '#/components/schemas/A/allOf/0'}\n"},
		{name: "a union in an inline allOf branch a $ref names",
			schemas: "    A: {allOf: [{oneOf: [" + d + ", {type: string}]}]}\n    B: {$ref: '#/components/schemas/A/allOf/0'}\n"},
		{name: "a parameter's content entry", paths: "paths:\n  /a: {get: {parameters: [{name: q, in: query, " +
			"content: {application/json: {schema: " + d + "}}}], responses: {'200': {description: ok}}}}\n"},
		{name: "a header's schema", paths: "paths:\n  /a: {get: {responses: {'200': {description: ok, " +
			"headers: {X-H: {schema: " + d + "}}}}}}\n"},
		{name: "a property of an inline allOf branch", schemas: "    A: {allOf: [{type: object, properties: {p: " + d + "}}]}\n"},
		{name: "a response a path names", paths: respond,
			components: "  responses:\n    F: {description: x, content: {application/json: {schema: " + d + "}}}\n"},
		{name: "a response reached through another a path names", paths: respond,
			components: "  responses:\n    E: {description: x, content: {application/json: {schema: " + d + "}}}\n" +
				"    F: {$ref: '#/components/responses/E'}\n"},
		{name: "a definition in a raw object a $ref reaches",
			schemas: "    A: {$ref: '#/x-lib/W/$defs/U'}\n", lib: "  W: {type: object, not: {}, $defs: {U: " + d + "}}\n"},
	} {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			schemas := c.schemas
			if schemas == "" {
				schemas = "    Z: {type: object}\n"
			}
			spec := mappingSpec(schemas, c.components+lib+c.lib)
			if c.paths != "" {
				spec = strings.Replace(spec, "paths: {}\n", c.paths, 1)
			}
			got, diags := loadTargets(t, "spec.yaml", spec, false)

			assert.Empty(t, diagLines(diags))
			assert.Equal(t, "marker", describedAs(got.Targets, "/x-lib/T"))
		})
	}
}

// TestMappings_AReferenceInABuiltObjectIsResolved pins that a schema $ref
// inside an object the load phase builds from raw YAML is resolved where the
// lowering lowers it, as the model's own are. Left unresolved, it resolved
// only through a node a mapping, or another $ref, happened to intern first, so
// whether it resolved followed declaration order. One under a keyword the
// lowering keeps verbatim, a "#/$defs/..." one (GitHub #570) and one naming
// another document stay as they were: unresolved, and other.yaml unread.
func TestMappings_AReferenceInABuiltObjectIsResolved(t *testing.T) {
	t.Parallel()
	dir := externalDir(t, map[string]string{"other.yaml": "X: {type: object, minLength: abc}\n"})
	spec := mappingSpec("    Box: {$ref: '#/x-lib/Wrapper'}\n"+petMapping("dog: '#/x-lib/Dog'"), `x-lib:
  Wrapper:
    type: object
    properties:
      pet: {$ref: '#/x-lib/Dog'}
      def: {$ref: '#/$defs/D'}
      far: {$ref: 'other.yaml#/X'}
    not: {$ref: '#/x-lib/Bad'}
  Dog: {description: dog, type: object}
  Bad: {type: object, minLength: abc}
`)
	got, diags := loadTargets(t, filepath.Join(dir, "root.yaml"), spec, true)
	require.Empty(t, diagLines(diags), "nothing past the lowered reference is resolved")

	box, ok := got.Doc.GetComponents().GetSchemas().Get("Box")
	require.True(t, ok)
	wrapper := box.GetResolvedSchema().GetSchema()
	require.NotNil(t, wrapper, "Box's $ref builds Wrapper")
	property := func(name string) *schemaRef {
		js, found := wrapper.GetProperties().Get(name)
		require.True(t, found, name)
		return js
	}
	pet := property("pet")
	require.True(t, pet.IsResolved())
	assert.Same(t, got.Targets.At("/x-lib/Dog"), pet.GetReferenceResolutionInfo().Object,
		"the $ref reaches the object Pet's mapping does")
	assert.False(t, property("def").IsResolved(), "a definition is read by GitHub #557's rule, which reads the model only")
	assert.False(t, property("far").IsResolved(), "another document is not read for content the source holds")
	assert.False(t, wrapper.GetNot().IsResolved(), "nothing lowers what not holds")
}

// TestMappings_AChainThatFailsPastItsFirstHopRecordsTheHop pins that a target
// whose chain fails further on still records the schema its first hop reached,
// which a $ref to the position lowers as well. Recorded nowhere, it resolved
// through the node such a $ref interned, and only in the order where that $ref
// came first.
func TestMappings_AChainThatFailsPastItsFirstHopRecordsTheHop(t *testing.T) {
	t.Parallel()
	got, diags := loadTargets(t, "spec.yaml", mappingSpec(petMapping("t: '#/x-lib/T'"),
		"x-lib:\n  T: {$ref: '#/x-lib/Missing', description: hop}\n"), false)

	assert.Empty(t, diags, "the failure is the lowering's to report")
	assert.Equal(t, "hop", describedAs(got.Targets, "/x-lib/T"))
}

// resolverOver runs the resolver pass over spec, as Load does with external
// references off, its mapping target work bounded by limit, and returns the
// mapping target resolver and what the pass reported.
func resolverOver(t *testing.T, spec string, limit int) (*mappings, []ir.Diagnostic) {
	t.Helper()
	doc, valErrs := parseSpec(t, spec)
	require.Empty(t, valErrs)
	pass := newResolution(t.Context(), pointerAt(0, overlay.Origin{}), doc, sourceDocument{path: "spec.yaml"}, Options{}, nil)
	pass.targets.limit = limit
	_, diags := pass.run(doc)
	return pass.targets, diags
}

// TestMappings_APositionIsResolvedOnce pins that the entries naming one
// position share one resolution, whatever their spelling, whether it resolves
// or not: resolving each again cost a full walk of the target, or a marshal of
// a whole non-schema one, per entry. An entry is read once wherever it is
// built, so the levels of a raw chain each mapping the next are read once each,
// not once for every level above them.
func TestMappings_APositionIsResolvedOnce(t *testing.T) {
	t.Parallel()
	const pet = "    Pet:\n      type: object\n      properties: {k: {type: string}}\n      discriminator:\n" +
		"        propertyName: k\n        mapping: {a: '#/x-lib/Cat', b: '#/x-lib/C%61t', c: 'spec.yaml#/x-lib/Cat', " +
		"p: '#/paths', q: '#/paths'}\n"
	const level = "{type: object, properties: {k: {type: string}, c: %s}, " +
		"discriminator: {propertyName: k, mapping: {a: '%s'}}}"
	chain := "{type: string}"
	next := "#/x-lib/L/properties/c/properties/c/properties/c"
	for depth := 3; depth > 0; depth-- {
		chain = fmt.Sprintf(level, chain, next)
		next = next[:strings.LastIndex(next, "/properties/c")]
	}
	spec := mappingSpec(pet+"    Z: {type: object, discriminator: {propertyName: k, mapping: {z: '#/x-lib/L'}}}\n",
		"x-lib:\n  Cat: {description: cat, type: object}\n  L: "+chain+"\n")
	m, diags := resolverOver(t, spec, maxMappingWork)
	require.Empty(t, diagLines(diags))

	assert.Equal(t, "cat", describedAs(m.targets(), "/x-lib/Cat"))
	assert.Len(t, m.named, 9, "Pet's five entries, Z's, and one at each level of L")
	assert.Equal(t, 6, m.resolutions, "Cat, /paths, L and the three positions under it, once each")
}

// TestMappings_TheWorkIsBounded pins maxMappingWork's bound: past it nothing
// more is resolved, which is reported once, at the document, since which
// target crosses it follows declaration order. Every bound short of the work
// the document takes is tried, so the bound is met at each kind of step.
func TestMappings_TheWorkIsBounded(t *testing.T) {
	t.Parallel()
	spec := mappingSpec(petMapping("a: '#/x-lib/A', b: '#/x-lib/B'"),
		"x-lib:\n  A: {description: a, type: object}\n  B: {description: b, type: object}\n")
	unbounded, diags := resolverOver(t, spec, maxMappingWork)
	require.Empty(t, diags)
	require.Equal(t, 2, unbounded.resolutions)

	stoppedAfter := map[int]bool{}
	for limit := 1; limit < unbounded.work; limit++ {
		bounded, diags := resolverOver(t, spec, limit)
		stoppedAfter[bounded.resolutions] = true

		assert.Equal(t, bounded.resolutions > 0, bounded.targets().At("/x-lib/A") != nil, "limit %d", limit)
		assert.Equal(t, bounded.resolutions > 1, bounded.targets().At("/x-lib/B") != nil, "limit %d", limit)
		require.Len(t, diags, 1, "limit %d: %+v", limit, diags)
		assert.Equal(t, diag.BudgetExceeded, diags[0].Code)
		assert.Equal(t, ir.SeverityError, diags[0].Severity)
		assert.Equal(t, ir.Provenance{Pointer: ""}, diags[0].Provenance)
		assert.Equal(t, fmt.Sprintf("resolving the discriminator mapping targets takes more than %d steps; "+
			"those past them are found only as the lowering meets them", limit), diags[0].Message)
	}
	assert.Equal(t, map[int]bool{0: true, 1: true, 2: true}, stoppedAfter,
		"some bound stops the work before each target, and one only after both")
}

// TestMappings_OnlyATargetInTheSourceIsResolved pins which targets the load
// phase resolves: those the lowering reads as naming a position in the source,
// spelled internally or by the source's file name, whether or not external
// references are allowed. A spelling through a directory is another document
// to the lowering (GitHub #576). A component's name is the component's, an
// anchor or the whole document names no pointer, and another document is not
// read for a mapping: the finding in its Cat would be reported if it were.
func TestMappings_OnlyATargetInTheSourceIsResolved(t *testing.T) {
	t.Parallel()
	dir := externalDir(t, map[string]string{"other.yaml": "x-lib:\n  Cat: {type: object, minLength: abc}\n"})
	path := filepath.Join(dir, "root.yaml")
	for _, c := range []struct {
		name, target string
		external     bool
		resolved     bool
	}{
		{"an internal pointer", "#/x-lib/Cat", false, true},
		{"the source's file name, read as the source", "root.yaml#/x-lib/Cat", true, true},
		{"the source's file name, with external references disallowed", "root.yaml#/x-lib/Cat", false, true},
		{"the source's file name through a directory", "./root.yaml#/x-lib/Cat", true, false},
		{"another document", "other.yaml#/x-lib/Cat", true, false},
		{"an anchor", "#cat", false, false},
		{"the whole source, by its file name", "root.yaml", true, false},
		{"the whole source, internally", "#", true, false},
	} {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			spec := mappingSpec(petMapping("c: '"+c.target+"'"), "x-lib:\n  Cat: {description: cat, type: object}\n")
			got, diags := loadTargets(t, path, spec, c.external)

			assert.Empty(t, diags, "nothing is reported, and no other document is read")
			assert.Equal(t, c.resolved, got.Targets.At("/x-lib/Cat") != nil)
		})
	}

	t.Run("a definition, however spelled", func(t *testing.T) {
		t.Parallel()
		// The document-rooted $defs is no definition the mapping names: the
		// lowering reads a $defs value relative to the discriminator. Resolving
		// it here reported the finding in it at the mapping entry.
		for _, target := range []string{"#/$defs/X", "root.yaml#/$defs/X"} {
			spec := mappingSpec("    Pet:\n      type: object\n"+
				"      discriminator: {propertyName: k, mapping: {x: '"+target+"'}}\n"+
				"      $defs: {X: {allOf: [{$ref: '#/components/schemas/Pet'}], type: object}}\n",
				"$defs:\n  X: {type: object, minLength: abc}\n")
			got, diags := loadTargets(t, path, spec, true)
			assert.Empty(t, diags, target)
			assert.Nil(t, got.Targets.At("/$defs/X"), target)
		}
	})

	t.Run("a declared component's name", func(t *testing.T) {
		t.Parallel()
		spec := mappingSpec(petMapping("c: '#/x-lib/Cat'")+"    '#/x-lib/Cat': {type: object}\n",
			"x-lib:\n  Cat: {description: cat, type: object}\n")
		got, diags := loadTargets(t, path, spec, false)
		assert.Empty(t, diags)
		assert.Nil(t, got.Targets.At("/x-lib/Cat"), "the name is the component's, not the pointer's")
	})
}

// TestMappings_ATargetThatDoesNotResolveIsLeftToTheLowering pins that the load
// phase records nothing for a target that does not resolve, and reports
// nothing either: the lowering reports every target it cannot resolve.
func TestMappings_ATargetThatDoesNotResolveIsLeftToTheLowering(t *testing.T) {
	t.Parallel()
	got, diags := loadTargets(t, "spec.yaml", mappingSpec(petMapping("m: '#/x-lib/Missing'"), "x-lib: {}\n"), false)
	assert.Empty(t, diags)
	assert.Nil(t, got.Targets.At("/x-lib/Missing"))
}

// TestMappings_AFindingInATargetIsReportedOnce pins where what resolving a
// target draws is reported: at the mapping entry, once, or at the least of the
// sites that reach the target when a $ref reaches it too, in either order.
func TestMappings_AFindingInATargetIsReportedOnce(t *testing.T) {
	t.Parallel()
	const lib = "x-lib:\n  Bad: {type: object, minLength: abc}\n"
	const finding = " openapi/validation/validation-type-mismatch"
	pet := petMapping("b: '#/x-lib/Bad'")
	holder := "    A: {$ref: '#/x-lib/Bad'}\n"
	for _, c := range []struct {
		name string
		docs []string
		want []string
	}{
		{"named by a mapping alone", []string{mappingSpec(pet, lib)},
			[]string{"/components/schemas/Pet/discriminator/mapping/b" + finding}},
		{"named by a $ref too, at the lesser site", []string{mappingSpec(pet+holder, lib), mappingSpec(holder+pet, lib)},
			[]string{"/components/schemas/A" + finding}},
	} {
		for _, spec := range c.docs {
			_, diags := loadTargets(t, "spec.yaml", spec, false)
			assert.Empty(t, cmp.Diff(c.want, diagLines(diags)), c.name)
		}
	}
}

// TestMappings_AFindingIsPlacedWhateverTheEntryOrder pins that every entry
// naming a target takes part in placing what resolving it draws: the finding
// lands at the least of their sites, in either declaration order, though only
// the first entry resolved draws it.
func TestMappings_AFindingIsPlacedWhateverTheEntryOrder(t *testing.T) {
	t.Parallel()
	const lib = "x-lib:\n  Bad: {type: object, minLength: abc}\n"
	a := strings.Replace(petMapping("b: '#/x-lib/Bad'"), "Pet:", "A:", 1)
	z := strings.Replace(petMapping("b: '#/x-lib/Bad'"), "Pet:", "Z:", 1)
	want := []string{"/components/schemas/A/discriminator/mapping/b openapi/validation/validation-type-mismatch"}
	for _, schemas := range []string{a + z, z + a} {
		_, diags := loadTargets(t, "spec.yaml", mappingSpec(schemas, lib), false)
		assert.Empty(t, cmp.Diff(want, diagLines(diags)))
	}
}

// TestMappings_AnEntryInABuiltObjectIsSitedAtItsKey pins where an entry in an
// object built from raw YAML is placed: under the key as written. The resolver
// records the pointer it reached decoded already, so decoding it again read a
// '+' in the key as a space and '%41' as 'A', siting the finding at a pointer
// that names nothing. Each row reaches the object by a mapping and by a $ref.
func TestMappings_AnEntryInABuiltObjectIsSitedAtItsKey(t *testing.T) {
	t.Parallel()
	for _, c := range []struct{ key, spelled string }{{"a+b", "a%2Bb"}, {"a%41", "a%2541"}} {
		lib := "x-lib:\n  '" + c.key + "':\n    type: object\n" +
			"    discriminator: {propertyName: k, mapping: {bad: '#/x-lib/Bad'}}\n" +
			"  Bad: {type: object, minLength: abc}\n"
		want := []string{"/x-lib/" + c.key + "/discriminator/mapping/bad openapi/validation/validation-type-mismatch"}
		for _, schemas := range []string{petMapping("r: '#/x-lib/" + c.spelled + "'"),
			"    Holder: {$ref: '#/x-lib/" + c.spelled + "'}\n"} {
			_, diags := loadTargets(t, "spec.yaml", mappingSpec(schemas, lib), false)
			assert.Empty(t, cmp.Diff(want, diagLines(diags)), "%s: %s", c.key, schemas)
		}
	}
}

// TestMappings_EverySpellingReachesOneObject pins that two spellings of one
// position resolve to one object, the one an internal $ref to it is cached as,
// in either order. Resolved as written, the file name spelling built an object
// of its own when it came first.
func TestMappings_EverySpellingReachesOneObject(t *testing.T) {
	t.Parallel()
	path := filepath.Join(t.TempDir(), "root.yaml")
	internal, byFile := "a: '#/x-lib/Cat'", "b: 'root.yaml#/x-lib/Cat'"
	for _, mapping := range []string{internal + ", " + byFile, byFile + ", " + internal} {
		got, diags := loadTargets(t, path,
			mappingSpec(petMapping(mapping), "x-lib:\n  Cat: {description: cat, type: object}\n"), true)
		require.Empty(t, diags)

		cached, ok := got.Doc.GetCachedReferencedObject(path + "#/x-lib/Cat")
		require.True(t, ok, "the internal spelling's object is cached under the source's key")
		assert.Same(t, cached, got.Targets.At("/x-lib/Cat"), mapping)
	}
}

// TestMappings_TheTargetsAreTheLastResolutions pins that a second resolution's
// targets are what Load returns: the first's belong to a model the rebuild
// replaced, and the lowering reads the one returned.
func TestMappings_TheTargetsAreTheLastResolutions(t *testing.T) {
	t.Parallel()
	trigger, _ := countingServer(t, anchoredExternalDoc)
	spec := mappingSpec(petMapping("k: '#/components/schemas/Kennel'")+"    Kennel: {type: object}\n", "")
	spec = strings.Replace(spec, "paths: {}\n",
		"paths:\n  /x: {$ref: \""+respelled(trigger.URL, "HTTP")+"/ext.yaml#/paths/~1x\"}\n", 1)
	data := []byte(spec)
	root, _, err := decodeStream(data)
	require.NoError(t, err)
	releaseAnchors(root)
	doc, _, err := unmarshal(t.Context(), data, root)
	require.NoError(t, err)
	rebuilt := false
	rebuild := func() (*soa.OpenAPI, error) {
		rebuilt = true
		again, _, err := unmarshal(t.Context(), data, root)
		return again, err
	}

	resolved, targets, _, err := resolveExternal(t.Context(), pointerAt(0, overlay.Origin{}), doc,
		newSourceDocument("root.yaml", data, root, nil), Options{AllowExternalRefs: true}, rebuild)

	require.NoError(t, err)
	require.True(t, rebuilt, "the fixture is resolved twice")
	kennel, ok := resolved.Components.Schemas.Get("Kennel")
	require.True(t, ok)
	assert.Same(t, kennel, targets.At("/components/schemas/Kennel"), "the target is the returned model's own")
}

// TestMappings_APanicIsReportedAtTheWorkRunning pins resolve's barrier: a panic
// stops it, as an error naming the reference or entry whose work was running.
func TestMappings_APanicIsReportedAtTheWorkRunning(t *testing.T) {
	t.Parallel()
	m := newMappings(sourceDocument{}, &soa.OpenAPI{}, oas3.ResolveOptions{}, nil)
	m.queue = []arrival{{site: "/x", record: record{object: 1,
		walk: func(context.Context) iter.Seq[soa.WalkItem] { panic("boom") }}}}

	site, err := m.resolve(t.Context(), &reachedFindings{})

	require.Error(t, err)
	assert.Equal(t, "discriminator mapping resolver panicked: boom", err.Error())
	assert.Equal(t, jsontext.Pointer("/x"), site)
}

// TestMappings_NoDiscriminatorOutsideTheSourceIsCollected pins that only what
// the source holds is searched for discriminators: one in another document
// maps by that document's pointers, which the source's would misread. Its
// mapping names #/x-lib/Y, which the source declares too, so collecting it
// would record the source's Y.
func TestMappings_NoDiscriminatorOutsideTheSourceIsCollected(t *testing.T) {
	t.Parallel()
	dir := externalDir(t, map[string]string{"other.yaml": `components:
  schemas:
    X: {type: object, discriminator: {propertyName: k, mapping: {y: '#/x-lib/Y'}}}
x-lib:
  Y: {type: object}
`})
	path := filepath.Join(dir, "root.yaml")
	const lib = "x-lib:\n  Y: {description: the source's, type: object}\n"
	for name, schemas := range map[string]string{
		"reached by a $ref": "    A: {$ref: './other.yaml#/components/schemas/X'}\n",
		"reached by a mapping": petMapping("e: '#/components/schemas/Ext'") +
			"    Ext: {$ref: './other.yaml#/components/schemas/X'}\n",
	} {
		got, diags := loadTargets(t, path, mappingSpec(schemas, lib), true)
		assert.Empty(t, diags, name)
		assert.Nil(t, got.Targets.At("/x-lib/Y"), name)
	}
}
