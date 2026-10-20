package canary_test

import (
	"slices"
	"strings"
)

// oracleShape is a document the binding oracle compiles: its paths, its
// component schemas, written one a line, the rest of components, and what
// follows them. A shape that is not fixed is compiled with its schemas in both
// orders; a fixed one, whose YAML aliases must follow their anchors, in the
// order written.
type oracleShape struct {
	name       string
	paths      string
	schemas    []string
	components string
	tail       string
	fixed      bool
}

// document writes s, its schemas reversed when reversed is set.
func (s oracleShape) document(reversed bool) string {
	schemas := s.schemas
	if reversed {
		schemas = slices.Clone(schemas)
		slices.Reverse(schemas)
	}
	var b strings.Builder
	if s.paths == "" {
		b.WriteString(head)
	} else {
		b.WriteString("openapi: 3.1.0\ninfo: {title: t, version: '1'}\npaths:\n" + s.paths)
	}
	b.WriteString("components:\n  schemas:\n")
	for _, line := range schemas {
		b.WriteString("    " + line + "\n")
	}
	b.WriteString(s.components)
	b.WriteString(s.tail)
	return b.String()
}

// ref writes the component name as a $ref to pointer.
func ref(name, pointer string) string {
	return name + ": {$ref: '" + pointer + "'}"
}

// component is the pointer to the component schema name.
func component(name string) string {
	return "#/components/schemas/" + name
}

// oracleShapes are the shapes the binding oracle compiles: loops of $refs at
// each position a schema is written at, recursion through what a schema
// declares, chains, the $defs rule, pointer spellings, targets no schema is
// written at, YAML aliases and merges, and pointers through a reference
// object.
func oracleShapes() []oracleShape {
	return slices.Concat(loopShapes(), chainShapes(), defsShapes(), spellingShapes(), targetShapes(), yamlShapes())
}

// loopShapes are loops of $refs, each closing at another position.
func loopShapes() []oracleShape {
	return []oracleShape{
		{name: "loop/self", schemas: []string{ref("A", component("A"))}},
		{name: "loop/two", schemas: []string{ref("A", component("B")), ref("B", component("A"))}},
		{name: "loop/three", schemas: []string{ref("A", component("B")), ref("B", component("C")), ref("C", component("A"))}},
		{name: "loop/entered", schemas: []string{ref("O", component("A")), ref("A", component("B")), ref("B", component("A"))}},
		{name: "loop/entered-twice", schemas: []string{ref("O", component("A")), ref("P", component("B")),
			ref("A", component("B")), ref("B", component("A"))}},
		{name: "loop/property", schemas: []string{"A: {properties: {p: {$ref: '" + component("B") + "'}}}",
			ref("B", component("A")+"/properties/p")}},
		{name: "loop/items", schemas: []string{"A: {type: array, items: {$ref: '" + component("B") + "'}}",
			ref("B", component("A")+"/items")}},
		{name: "loop/all-of", schemas: []string{"A: {allOf: [{$ref: '" + component("B") + "'}]}",
			ref("B", component("A")+"/allOf/0")}},
		{name: "loop/through-own-schema", schemas: []string{"A: {$ref: '" + component("A") + "/properties/p', " +
			"properties: {p: {$ref: '" + component("A") + "'}}}"}},
		{name: "loop/raw", schemas: []string{ref("A", "#/x-lib/S")},
			tail: "x-lib:\n  S: {$ref: '#/x-lib/T'}\n  T: {$ref: '#/x-lib/S'}\n"},
		{name: "loop/root-defs", schemas: []string{ref("A", "#/$defs/X")}, tail: "$defs:\n  X: {$ref: '" + component("A") + "'}\n"},
		{name: "loop/parameter-schema", schemas: []string{ref("A", "#/components/parameters/P/schema")},
			components: "  parameters:\n    P: {name: p, in: query, schema: {$ref: '" + component("A") + "'}}\n"},
		{name: "loop/through-a-response", schemas: []string{ref("A", "#/components/responses/Alias/content/application~1json/schema")},
			components: "  responses:\n    Alias: {$ref: '#/components/responses/R'}\n" +
				"    R: {description: ok, content: {application/json: {schema: {$ref: '" + component("A") + "'}}}}\n"},
		{name: "recursion/property", schemas: []string{"A: {type: object, properties: {next: {$ref: '" + component("A") + "'}}}"}},
		{name: "recursion/items", schemas: []string{"A: {type: array, items: {$ref: '" + component("A") + "'}}"}},
		{name: "recursion/mutual", schemas: []string{"A: {properties: {b: {$ref: '" + component("B") + "'}}}",
			"B: {properties: {a: {$ref: '" + component("A") + "'}}}"}},
	}
}

// chainShapes are chains of $refs that end.
func chainShapes() []oracleShape {
	return []oracleShape{
		{name: "chain/five", schemas: []string{ref("A", component("B")), ref("B", component("C")), ref("C", component("D")),
			ref("D", component("E")), "E: {type: string}"}},
		{name: "chain/through-raw", schemas: []string{ref("A", "#/x-lib/S"), "B: {type: string}"},
			tail: "x-lib:\n  S: {$ref: '" + component("B") + "'}\n"},
		{name: "chain/into-a-property", schemas: []string{ref("A", component("H")+"/properties/p"),
			"H: {properties: {p: {$ref: '" + component("B") + "'}}}", "B: {type: string}"}},
		{name: "chain/through-a-path-item", schemas: []string{ref("A",
			"#/paths/~1a/get/responses/200/content/application~1json/schema"), "B: {type: string}"},
			paths: "  /a: {$ref: '#/components/pathItems/P'}\n",
			components: "  pathItems:\n    P: {get: {responses: {'200': {description: ok, content: " +
				"{application/json: {schema: {$ref: '" + component("B") + "'}}}}}}}\n"},
	}
}

// defsShapes read "#/$defs/..." pointers by the $defs rule.
func defsShapes() []oracleShape {
	return []oracleShape{
		{name: "defs/own-resource", schemas: []string{"A: {$id: 'https://x.test/a', $defs: {X: {type: string}}, " +
			"properties: {p: {$ref: '#/$defs/X'}}}"}},
		{name: "defs/ancestor", schemas: []string{"H: {$defs: {X: {type: string}}, properties: {p: {$ref: '#/$defs/X'}}}"}},
		{name: "defs/order", schemas: []string{ref("R", component("H")+"/properties/p"),
			"H: {$defs: {X: {type: string}}, properties: {p: {$ref: '#/$defs/X'}}}"}},
		{name: "defs/root", schemas: []string{ref("A", "#/$defs/X")}, tail: "$defs:\n  X: {type: string}\n"},
		{name: "defs/root-and-own", schemas: []string{"A: {$ref: '#/$defs/X', $defs: {X: {type: integer}}}",
			ref("C", component("A"))}, tail: "$defs:\n  X: {type: string}\n"},
		{name: "defs/within-defs", schemas: []string{"H: {$ref: '#/$defs/Y', $defs: {X: {type: string}, Y: {$ref: '#/$defs/X'}}}"}},
		{name: "defs/raw-in-a-definition", schemas: []string{"H: {$defs: {X: {type: string, example: {type: integer}}}, " +
			"properties: {p: {$ref: '#/$defs/X/example'}}}"}},
		{name: "defs/missing", schemas: []string{"A: {properties: {p: {$ref: '#/$defs/Missing'}}}"}},
	}
}

// spellingShapes spell a pointer in ways a byte-for-byte reader, RFC 6901
// and the library may each read otherwise.
func spellingShapes() []oracleShape {
	return []oracleShape{
		{name: "spelling/percent-space", schemas: []string{"'a b': {type: string}", ref("A", component("a%20b"))}},
		{name: "spelling/escaped-slash", schemas: []string{"'a/b': {type: string}", ref("A", component("a~1b"))}},
		{name: "spelling/plus", schemas: []string{"'a+b': {type: string}", ref("A", component("a+b"))}},
		{name: "spelling/trailing-slash", schemas: []string{ref("A", component("B")+"/"), "B: {type: string}"}},
		{name: "spelling/empty-key", schemas: []string{ref("A", component("B")+"/"), "B: {'': {type: integer}, type: string}"}},
		{name: "spelling/trailing-space", schemas: []string{ref("A", component("B")+" "), "B: {type: string}"}},
		{name: "spelling/bad-escape", schemas: []string{ref("A", component("a~2b")), "'a~2b': {type: string}"}},
		{name: "spelling/percent-encoded-defs", schemas: []string{ref("A", component("H")+"/%24defs/X"),
			"H: {$defs: {X: {type: string}}}"}},
		{name: "spelling/root", schemas: []string{ref("A", "#")}},
		{name: "spelling/root-slash", schemas: []string{ref("A", "#/")}},
	}
}

// targetShapes are $refs to what no schema is written at, or to what the
// compiler does not read as a target.
func targetShapes() []oracleShape {
	return []oracleShape{
		{name: "target/info", schemas: []string{ref("A", "#/info")}},
		{name: "target/paths", schemas: []string{ref("A", "#/paths")}},
		{name: "target/parameter", schemas: []string{ref("A", "#/components/parameters/P")},
			components: "  parameters:\n    P: {name: p, in: query, schema: {type: string}}\n"},
		{name: "target/enum-member", schemas: []string{ref("A", component("E")+"/enum/0"), "E: {enum: [{type: string}]}"}},
		{name: "target/anchor", schemas: []string{ref("A", "#x"), "B: {$anchor: x, type: string}"}},
		{name: "target/anchor-in-its-schema", schemas: []string{"A: {$anchor: x, properties: {p: {$ref: '#x'}}}"}},
		{name: "target/id", schemas: []string{ref("A", "https://x.test/b"), "B: {$id: 'https://x.test/b', type: string}"}},
		{name: "target/another-document", schemas: []string{ref("A", "other.yaml#/X")}},
		{name: "target/missing", schemas: []string{ref("A", component("Missing"))}},
	}
}

// yamlShapes place a $ref by a YAML alias or merge key, whose anchor must be
// written first, so each is compiled in the order written.
func yamlShapes() []oracleShape {
	return []oracleShape{
		{name: "yaml/alias", fixed: true, schemas: []string{"A: &a {properties: {p: {$ref: '" + component("B") + "'}}}",
			"B: {type: string}", "C: *a"}},
		{name: "yaml/merge", fixed: true, schemas: []string{"Base: &b {properties: {p: {$ref: '" + component("B") + "'}}}",
			"B: {type: string}", "Child: {<<: *b, type: object}"}},
		{name: "yaml/alias-loop", fixed: true, schemas: []string{"A: &a {$ref: '" + component("B") + "'}", "B: *a"}},
	}
}
