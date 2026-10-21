package oas_test

import (
	"testing"

	"github.com/dexpace/morphic/compilers/openapi/internal/oas"
)

// TestCheck_TagParentsExist pins the rule on 3.2's nested tags.
func TestCheck_TagParentsExist(t *testing.T) {
	const head = "openapi: 3.2.0\ninfo: {title: t, version: v}\ncomponents: {}\n"
	verify(t, []scenario{
		{"parent exists", "3.2.0", oas.Document, head + "tags: [{name: a}, {name: b, parent: a}]\n", nil},
		{"siblings under one parent", "3.2.0", oas.Document, head + "tags: [{name: a}, {name: b, parent: a}, {name: c, parent: a}]\n", nil},
		{"parent missing", "3.2.0", oas.Document, head + "tags: [{name: a, parent: z}]\n", []finding{at(dangling, "/tags/0/parent")}},
		{"tag under itself", "3.2.0", oas.Document, head + "tags: [{name: a, parent: a}]\n", []finding{at(dangling, "/tags/0/parent")}},
		{"cycle", "3.2.0", oas.Document, head + "tags: [{name: a, parent: b}, {name: b, parent: a}]\n",
			[]finding{at(dangling, "/tags/0/parent"), at(dangling, "/tags/1/parent")}},
		{"a chain below a cycle", "3.2.0", oas.Document,
			head + "tags: [{name: a, parent: b}, {name: b, parent: a}, {name: c, parent: a}]\n",
			[]finding{at(dangling, "/tags/0/parent"), at(dangling, "/tags/1/parent")}},
		{"tag with an unreadable name", "3.2.0", oas.Document, head + "tags: [{parent: a}, {name: [x], parent: y}]\n",
			[]finding{at(required, "/tags/0"), at(mismatch, "/tags/1/name")}},
		{"no tags", "3.2.0", oas.Document, head, nil},
		{"tags that are no list", "3.2.0", oas.Document, head + "tags: x\n", []finding{at(mismatch, "/tags")}},
		{"parents are 3.2's", "3.1.0", oas.Document, "openapi: 3.1.0\ninfo: {title: t, version: v}\ncomponents: {}\ntags: [{name: a, parent: z}]\n",
			[]finding{at(later, "/tags/0/parent")}},
	})
}

// TestCheck_SecurityRequirementsNameDeclaredSchemes pins the rule that every
// name a requirement uses is declared.
func TestCheck_SecurityRequirementsNameDeclaredSchemes(t *testing.T) {
	doc := func(version, extra string) string {
		return "openapi: " + version + "\ninfo: {title: t, version: v}\npaths: {}\ncomponents: {securitySchemes: {k: {type: mutualTLS}}}\n" + extra
	}
	verify(t, []scenario{
		{"declared", "3.2.0", oas.Document, doc("3.2.0", "security: [{k: []}]\n"), nil},
		{"undeclared at the root", "3.1.0", oas.Document, doc("3.1.0", "security: [{k: [], z: []}, {}]\n"),
			[]finding{at(dangling, "/security/0/z")}},
		{"undeclared in an operation", "3.0.3", oas.Document,
			"openapi: 3.0.3\ninfo: {title: t, version: v}\npaths:\n  /a:\n    get:\n      responses: {}\n      security: [{z: []}]\n",
			[]finding{at(dangling, "/paths/~1a/get/security/0/z"), at(entries, "/paths/~1a/get/responses")}},
		{"undeclared in a webhook", "3.1.0", oas.Document,
			"openapi: 3.1.0\ninfo: {title: t, version: v}\nwebhooks:\n  w:\n    post:\n      security: [{z: []}]\n",
			[]finding{at(dangling, "/webhooks/w/post/security/0/z")}},
		{"a uri in 3.2", "3.2.0", oas.Document, doc("3.2.0", "security: [{./k: [], 'other.yaml#/s': [], '.x': []}]\n"), nil},
		{"a uri in 3.1 is just a name", "3.1.0", oas.Document, doc("3.1.0", "security: [{./k: []}]\n"),
			[]finding{at(dangling, "/security/0/.~1k")}},
		{"a bare name in 3.2 is a component name", "3.2.0", oas.Document, doc("3.2.0", "security: [{z: []}]\n"),
			[]finding{at(dangling, "/security/0/z")}},
		{"no components at all", "3.1.0", oas.Document, "openapi: 3.1.0\ninfo: {title: t, version: v}\npaths: {}\nsecurity: [{z: []}]\n",
			[]finding{at(dangling, "/security/0/z")}},
		{"security that is no list", "3.1.0", oas.Document, doc("3.1.0", "security: x\n"), []finding{at(mismatch, "/security")}},
		{"an operation with a security list that is no list", "3.1.0", oas.Document,
			"openapi: 3.1.0\ninfo: {title: t, version: v}\npaths:\n  /a:\n    get: {responses: {'200': {description: d}}, security: x}\n",
			[]finding{at(mismatch, "/paths/~1a/get/security")}},
	})
}

// TestCheck_PathTemplatesAnswerToPathParameters pins the rules on the Paths
// Object that match template expressions to parameters.
func TestCheck_PathTemplatesAnswerToPathParameters(t *testing.T) {
	param := "{name: id, in: path, required: true, schema: {}}"
	ok := "{responses: {'200': {description: d}}}"
	verify(t, []scenario{
		{"parameter on the operation", "3.1.0", oas.Paths, "{\"/a/{id}\": {get: {responses: {'200': {description: d}}, parameters: [" + param + "]}}}", nil},
		{"parameter on the item", "3.1.0", oas.Paths, "{\"/a/{id}\": {parameters: [" + param + "], get: " + ok + "}}", nil},
		{"parameter on the item, none on the operation", "3.1.0", oas.Paths, "{\"/a/{id}\": {parameters: [" + param + "], get: " + ok + ", put: " + ok + "}}", nil},
		{"parameter on every operation", "3.1.0", oas.Paths,
			"{\"/a/{id}\": {get: {responses: {'200': {description: d}}, parameters: [" + param + "]}, put: {responses: {'200': {description: d}}, parameters: [" + param + "]}}}", nil},
		{"parameter on one operation of two", "3.1.0", oas.Paths,
			"{\"/a/{id}\": {get: {responses: {'200': {description: d}}, parameters: [" + param + "]}, put: " + ok + "}}",
			[]finding{at(dangling, "/~1a~1{id}")}},
		{"no parameter at all", "3.1.0", oas.Paths, "{\"/a/{id}\": {get: " + ok + "}}", []finding{at(dangling, "/~1a~1{id}")}},
		{"an empty path item needs none", "3.1.0", oas.Paths, "{\"/a/{id}\": {}}", nil},
		{"a path item with only a reference", "3.1.0", oas.Paths, "{\"/a/{id}\": {$ref: '#/components/pathItems/p'}}", nil},
		{"a path with no expression", "3.1.0", oas.Paths, "{/a: {get: " + ok + "}}", nil},
		{"extensions are not paths", "3.1.0", oas.Paths, "{x-a: {get: 1}}", nil},
		{"parameter naming no expression", "3.1.0", oas.Paths,
			"{/a: {get: {responses: {'200': {description: d}}, parameters: [" + param + "]}}}", []finding{at(dangling, "/~1a/get/parameters/0")}},
		{"item parameter naming no expression", "3.1.0", oas.Paths, "{/a: {parameters: [" + param + "], get: " + ok + "}}",
			[]finding{at(dangling, "/~1a/parameters/0")}},
		{"query parameters name nothing in the path", "3.1.0", oas.Paths,
			"{/a: {get: {responses: {'200': {description: d}}, parameters: [{name: q, in: query, schema: {}}]}}}", nil},
		{"two expressions, one answered", "3.1.0", oas.Paths,
			"{\"/a/{id}/{other}\": {get: {responses: {'200': {description: d}}, parameters: [" + param + "]}}}", []finding{at(dangling, "/~1a~1{id}~1{other}")}},
		{"an unclosed brace", "3.1.0", oas.Paths, "{\"/a/{id\": {get: " + ok + "}}", nil},
		{"parameters that are no list", "3.1.0", oas.Paths, "{\"/a/{id}\": {get: {responses: {'200': {description: d}}, parameters: x}}}",
			[]finding{at(mismatch, "/~1a~1{id}/get/parameters"), at(dangling, "/~1a~1{id}")}},
		{"additional operations", "3.2.0", oas.Paths,
			"{\"/a/{id}\": {additionalOperations: {COPY: {responses: {'200': {description: d}}, parameters: [" + param + "]}}}}", nil},
	})
}

// TestCheck_PathParametersMayBeReferences pins that a $ref into the components
// is followed one hop in the document being checked, and that a parameter the
// check cannot read answers for any template expression of its path.
func TestCheck_PathParametersMayBeReferences(t *testing.T) {
	doc := func(ref string) string {
		return "openapi: 3.1.0\ninfo: {title: t, version: v}\npaths:\n  /a/{id}:\n    get:\n      responses: {'200': {description: d}}\n      parameters: [{$ref: '" + ref + "'}]\ncomponents:\n  parameters:\n    id: {name: id, in: path, required: true, schema: {}}\n    'a/b': {name: other, in: path, required: true, schema: {}}\n    'c~d': {name: id, in: path, required: true, schema: {}}\n"
	}
	badKeys := []finding{at(badSyntax, "/components/parameters/a~1b"), at(badSyntax, "/components/parameters/c~0d")}
	verify(t, []scenario{
		{"followed", "3.1.0", oas.Document, doc("#/components/parameters/id"), badKeys},
		{"followed with an escaped name", "3.1.0", oas.Document, doc("#/components/parameters/c~0d"), badKeys},
		{"escaped slash is a nested path", "3.1.0", oas.Document, doc("#/components/parameters/a~1b"),
			append(badKeys, at(dangling, "/paths/~1a~1{id}/get/parameters/0"), at(dangling, "/paths/~1a~1{id}"))},
		{"a pointer deeper than a name is unread", "3.1.0", oas.Document, doc("#/components/parameters/id/name"), badKeys},
		{"another document is unread", "3.1.0", oas.Document, doc("other.yaml#/components/parameters/id"), badKeys},
		{"no such parameter is unread", "3.1.0", oas.Document, doc("#/components/parameters/none"), badKeys},
		{"a parameter that is itself a reference is unread", "3.1.0", oas.Document,
			doc("#/components/parameters/again") + "    again: {$ref: 'other.yaml#/id'}\n", badKeys},
		{"an unread parameter on one operation answers for it only", "3.1.0", oas.Paths,
			"{\"/a/{id}\": {get: {responses: {'200': {description: d}}, parameters: [{$ref: 'other.yaml#/id'}]}, put: {responses: {'200': {description: d}}}}}",
			[]finding{at(dangling, "/~1a~1{id}")}},
		{"an unread parameter on the item answers for every operation", "3.1.0", oas.Paths,
			"{\"/a/{id}\": {parameters: [{$ref: 'other.yaml#/id'}], get: {responses: {'200': {description: d}}}}}", nil},
	})
}

// TestCheck_IdenticalTemplatesAreFound pins the rule on templated paths.
func TestCheck_IdenticalTemplatesAreFound(t *testing.T) {
	verify(t, []scenario{
		{"same shape, other names", "3.1.0", oas.Paths, "{\"/a/{x}\": {}, \"/a/{y}\": {}}", []finding{at(duplicate, "/~1a~1{y}")}},
		{"different shapes", "3.1.0", oas.Paths, "{\"/a/{x}\": {}, \"/a/{y}/b\": {}, /a/c: {}}", nil},
		{"extensions are not paths", "3.1.0", oas.Paths, "{x-a: {}, x-b: {}}", nil},
		{"two expressions", "3.1.0", oas.Paths, "{\"/{a}/{b}\": {}, \"/{b}/{a}\": {}}", []finding{at(duplicate, "/~1{b}~1{a}")}},
	})
}

// TestCheck_ServerUrlTemplatesAreWellFormed pins the 3.2 template grammar.
func TestCheck_ServerUrlTemplatesAreWellFormed(t *testing.T) {
	url := func(u string) string { return `{url: "` + u + `"}` }
	verify(t, []scenario{
		{"no variables", "3.2.0", oas.Server, url("https://a.example/v1"), nil},
		{"variables", "3.2.0", oas.Server, url("https://{host}:{port}/v1"), nil},
		{"a variable twice", "3.2.0", oas.Server, url("https://{a}/{a}"), []finding{at(badSyntax, "/url")}},
		{"a brace never closed", "3.2.0", oas.Server, url("https://{a"), []finding{at(badSyntax, "/url")}},
		{"a brace closed first", "3.2.0", oas.Server, url("https://a}/{b}"), []finding{at(badSyntax, "/url")}},
		{"a closing brace alone", "3.2.0", oas.Server, url("https://a}"), []finding{at(badSyntax, "/url")}},
		{"a variable with no name", "3.2.0", oas.Server, url("https://{}/"), []finding{at(badSyntax, "/url")}},
		{"nested braces", "3.2.0", oas.Server, url("https://{a{b}}/"), []finding{at(badSyntax, "/url")}},
		{"the template grammar is 3.2's", "3.1.0", oas.Server, url("https://{a}/{a}"), nil},
		{"a url that is no scalar", "3.2.0", oas.Server, `{url: [a]}`, []finding{at(mismatch, "/url")}},
	})
}

// TestCheck_AdditionalOperationsExcludeStandardMethods pins the 3.2 rule.
func TestCheck_AdditionalOperationsExcludeStandardMethods(t *testing.T) {
	verify(t, []scenario{
		{"a custom method", "3.2.0", oas.PathItem, `{additionalOperations: {COPY: {}}}`, nil},
		{"a standard method", "3.2.0", oas.PathItem, `{additionalOperations: {POST: {}}}`, []finding{at(allowed, "/additionalOperations/POST")}},
		{"a lower-case standard method", "3.2.0", oas.PathItem, `{additionalOperations: {query: {}}}`, []finding{at(allowed, "/additionalOperations/query")}},
		{"in 3.1 the field is unknown", "3.1.0", oas.PathItem, `{additionalOperations: {POST: {}}}`, []finding{at(later, "/additionalOperations")}},
	})
}
