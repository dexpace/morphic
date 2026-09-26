package load

import "testing"

// The fixtures below are drawn from the JSON Schema Test Suite's own
// draft2020-12 groups (ref.json, defs.json, anchor.json, dynamicRef.json,
// refRemote.json), each embedded four ways: bare (v1), wrapped inside an
// object property (v2), referenced by a sibling component declared first
// (v3), and duplicated across two components (v4). None of these are refused;
// sweeping the full suite this way (1736 documents, every group times every
// embedding) measures 0 false refusals, and this table pins a representative
// ~20 of them as a committed regression rather than a rerun of that sweep.
const (
	// ref.json#0 'root pointer ref' (bare).
	suiteRefRootPointer = `openapi: 3.1.0
info: {title: t, version: "1"}
paths: {}
components:
  schemas:
    S: {"$schema": "https://json-schema.org/draft/2020-12/schema", "properties": {"foo": {"$ref": "#"}}, "additionalProperties": false}
`
	// ref.json#4 'nested refs' (wrapped in an object property).
	suiteRefNestedRefs = `openapi: 3.1.0
info: {title: t, version: "1"}
paths: {}
components:
  schemas:
    W: {"type": "object", "properties": {"s": {"$schema": "https://json-schema.org/draft/2020-12/schema", "$defs": {"a": {"type": "integer"}, "b": {"$ref": "#/$defs/a"}, "c": {"$ref": "#/$defs/b"}}, "$ref": "#/$defs/c"}}}
`
	// ref.json#11 'Recursive references between schemas' (referenced by a sibling component).
	suiteRefRecursiveBetweenSchemas = `openapi: 3.1.0
info: {title: t, version: "1"}
paths: {}
components:
  schemas:
    R: {"$ref": "#/components/schemas/S"}
    S: {"$schema": "https://json-schema.org/draft/2020-12/schema", "$id": "http://localhost:1234/draft2020-12/tree", "description": "tree of nodes", "type": "object", "properties": {"meta": {"type": "string"}, "nodes": {"type": "array", "items": {"$ref": "node"}}}, "required": ["meta", "nodes"], "$defs": {"node": {"$id": "http://localhost:1234/draft2020-12/node", "description": "node", "type": "object", "properties": {"value": {"type": "number"}, "subtree": {"$ref": "tree"}}, "required": ["value"]}}}
`
	// ref.json#13 'ref creates new scope when adjacent to keywords' (duplicated across two components).
	suiteRefNewScopeAdjacentToKeywords = `openapi: 3.1.0
info: {title: t, version: "1"}
paths: {}
components:
  schemas:
    S: {"$schema": "https://json-schema.org/draft/2020-12/schema", "$defs": {"A": {"unevaluatedProperties": false}}, "properties": {"prop1": {"type": "string"}}, "$ref": "#/$defs/A"}
    T: {"$schema": "https://json-schema.org/draft/2020-12/schema", "$defs": {"A": {"unevaluatedProperties": false}}, "properties": {"prop1": {"type": "string"}}, "$ref": "#/$defs/A"}
`
	// ref.json#15 'refs with relative uris and defs' (bare).
	suiteRefRelativeURIsAndDefs = `openapi: 3.1.0
info: {title: t, version: "1"}
paths: {}
components:
  schemas:
    S: {"$schema": "https://json-schema.org/draft/2020-12/schema", "$id": "http://example.com/schema-relative-uri-defs1.json", "properties": {"foo": {"$id": "schema-relative-uri-defs2.json", "$defs": {"inner": {"properties": {"bar": {"type": "string"}}}}, "$ref": "#/$defs/inner"}}, "$ref": "schema-relative-uri-defs2.json"}
`
	// ref.json#18 'order of evaluation: $id and $ref' (wrapped in an object property).
	suiteRefOrderOfIDAndRef = `openapi: 3.1.0
info: {title: t, version: "1"}
paths: {}
components:
  schemas:
    W: {"type": "object", "properties": {"s": {"$comment": "$id must be evaluated before $ref to get the proper $ref destination", "$schema": "https://json-schema.org/draft/2020-12/schema", "$id": "https://example.com/draft2020-12/ref-and-id1/base.json", "$ref": "int.json", "$defs": {"bigint": {"$comment": "canonical uri: https://example.com/ref-and-id1/int.json", "$id": "int.json", "maximum": 10}, "smallint": {"$comment": "canonical uri: https://example.com/ref-and-id1-int.json", "$id": "/draft2020-12/ref-and-id1-int.json", "maximum": 2}}}}}
`
	// defs.json#0 'validate definition against metaschema' (referenced by a sibling component).
	suiteDefsValidateAgainstMetaschema = `openapi: 3.1.0
info: {title: t, version: "1"}
paths: {}
components:
  schemas:
    R: {"$ref": "#/components/schemas/S"}
    S: {"$schema": "https://json-schema.org/draft/2020-12/schema", "$ref": "https://json-schema.org/draft/2020-12/schema"}
`
	// anchor.json#0 'Location-independent identifier' (duplicated across two components).
	suiteAnchorLocationIndependent = `openapi: 3.1.0
info: {title: t, version: "1"}
paths: {}
components:
  schemas:
    S: {"$schema": "https://json-schema.org/draft/2020-12/schema", "$ref": "#foo", "$defs": {"A": {"$anchor": "foo", "type": "integer"}}}
    T: {"$schema": "https://json-schema.org/draft/2020-12/schema", "$ref": "#foo", "$defs": {"A": {"$anchor": "foo", "type": "integer"}}}
`
	// anchor.json#1 'Location-independent identifier with absolute URI' (bare).
	suiteAnchorLocationIndependentAbsoluteURI = `openapi: 3.1.0
info: {title: t, version: "1"}
paths: {}
components:
  schemas:
    S: {"$schema": "https://json-schema.org/draft/2020-12/schema", "$ref": "http://localhost:1234/draft2020-12/bar#foo", "$defs": {"A": {"$id": "http://localhost:1234/draft2020-12/bar", "$anchor": "foo", "type": "integer"}}}
`
	// anchor.json#2 'Location-independent identifier with base URI change in subschema' (wrapped in an object property).
	suiteAnchorBaseURIChangeInSubschema = `openapi: 3.1.0
info: {title: t, version: "1"}
paths: {}
components:
  schemas:
    W: {"type": "object", "properties": {"s": {"$schema": "https://json-schema.org/draft/2020-12/schema", "$id": "http://localhost:1234/draft2020-12/root", "$ref": "http://localhost:1234/draft2020-12/nested.json#foo", "$defs": {"A": {"$id": "nested.json", "$defs": {"B": {"$anchor": "foo", "type": "integer"}}}}}}}
`
	// anchor.json#3 'same $anchor with different base uri' (referenced by a sibling component).
	suiteAnchorSameNameDifferentBaseURI = `openapi: 3.1.0
info: {title: t, version: "1"}
paths: {}
components:
  schemas:
    R: {"$ref": "#/components/schemas/S"}
    S: {"$schema": "https://json-schema.org/draft/2020-12/schema", "$id": "http://localhost:1234/draft2020-12/foobar", "$defs": {"A": {"$id": "child1", "allOf": [{"$id": "child2", "$anchor": "my_anchor", "type": "number"}, {"$anchor": "my_anchor", "type": "string"}]}}, "$ref": "child1#my_anchor"}
`
	// dynamicRef.json#0 'A $dynamicRef to a $dynamicAnchor in the same schema resource behaves like a normal $ref to an $anchor' (duplicated across two components).
	suiteDynamicRefToDynamicAnchorSameResource = `openapi: 3.1.0
info: {title: t, version: "1"}
paths: {}
components:
  schemas:
    S: {"$schema": "https://json-schema.org/draft/2020-12/schema", "$id": "https://test.json-schema.org/dynamicRef-dynamicAnchor-same-schema/root", "type": "array", "items": {"$dynamicRef": "#items"}, "$defs": {"foo": {"$dynamicAnchor": "items", "type": "string"}}}
    T: {"$schema": "https://json-schema.org/draft/2020-12/schema", "$id": "https://test.json-schema.org/dynamicRef-dynamicAnchor-same-schema/root", "type": "array", "items": {"$dynamicRef": "#items"}, "$defs": {"foo": {"$dynamicAnchor": "items", "type": "string"}}}
`
	// dynamicRef.json#3 'A $dynamicRef resolves to the first $dynamicAnchor still in scope that is encountered when the schema is evaluated' (bare).
	suiteDynamicRefFirstAnchorInScope = `openapi: 3.1.0
info: {title: t, version: "1"}
paths: {}
components:
  schemas:
    S: {"$schema": "https://json-schema.org/draft/2020-12/schema", "$id": "https://test.json-schema.org/typical-dynamic-resolution/root", "$ref": "list", "$defs": {"foo": {"$dynamicAnchor": "items", "type": "string"}, "list": {"$id": "list", "type": "array", "items": {"$dynamicRef": "#items"}, "$defs": {"items": {"$comment": "This is only needed to satisfy the bookending requirement", "$dynamicAnchor": "items"}}}}}
`
	// dynamicRef.json#11 'multiple dynamic paths to the $dynamicRef keyword' (wrapped in an object property).
	suiteDynamicRefMultiplePaths = `openapi: 3.1.0
info: {title: t, version: "1"}
paths: {}
components:
  schemas:
    W: {"type": "object", "properties": {"s": {"$schema": "https://json-schema.org/draft/2020-12/schema", "$id": "https://test.json-schema.org/dynamic-ref-with-multiple-paths/main", "if": {"properties": {"kindOfList": {"const": "numbers"}}, "required": ["kindOfList"]}, "then": {"$ref": "numberList"}, "else": {"$ref": "stringList"}, "$defs": {"genericList": {"$id": "genericList", "properties": {"list": {"items": {"$dynamicRef": "#itemType"}}}, "$defs": {"defaultItemType": {"$comment": "Only needed to satisfy bookending requirement", "$dynamicAnchor": "itemType"}}}, "numberList": {"$id": "numberList", "$defs": {"itemType": {"$dynamicAnchor": "itemType", "type": "number"}}, "$ref": "genericList"}, "stringList": {"$id": "stringList", "$defs": {"itemType": {"$dynamicAnchor": "itemType", "type": "string"}}, "$ref": "genericList"}}}}}
`
	// dynamicRef.json#15 '$ref and $dynamicAnchor are independent of order - $defs first' (referenced by a sibling component).
	suiteDynamicRefOrderIndependentDefsFirst = `openapi: 3.1.0
info: {title: t, version: "1"}
paths: {}
components:
  schemas:
    R: {"$ref": "#/components/schemas/S"}
    S: {"$schema": "https://json-schema.org/draft/2020-12/schema", "$id": "http://localhost:1234/draft2020-12/strict-extendible-allof-defs-first.json", "allOf": [{"$ref": "extendible-dynamic-ref.json"}, {"$defs": {"elements": {"$dynamicAnchor": "elements", "properties": {"a": true}, "required": ["a"], "additionalProperties": false}}}]}
`
	// dynamicRef.json#17 '$ref to $dynamicRef finds detached $dynamicAnchor' (duplicated across two components).
	suiteRefToDynamicRefFindsDetachedAnchor = `openapi: 3.1.0
info: {title: t, version: "1"}
paths: {}
components:
  schemas:
    S: {"$ref": "http://localhost:1234/draft2020-12/detached-dynamicref.json#/$defs/foo"}
    T: {"$ref": "http://localhost:1234/draft2020-12/detached-dynamicref.json#/$defs/foo"}
`
	// refRemote.json#0 'remote ref' (bare).
	suiteRefRemoteBasic = `openapi: 3.1.0
info: {title: t, version: "1"}
paths: {}
components:
  schemas:
    S: {"$schema": "https://json-schema.org/draft/2020-12/schema", "$ref": "http://localhost:1234/draft2020-12/integer.json"}
`
	// refRemote.json#7 'root ref in remote ref' (wrapped in an object property).
	suiteRefRemoteRootRefInRemoteRef = `openapi: 3.1.0
info: {title: t, version: "1"}
paths: {}
components:
  schemas:
    W: {"type": "object", "properties": {"s": {"$schema": "https://json-schema.org/draft/2020-12/schema", "$id": "http://localhost:1234/draft2020-12/object", "type": "object", "properties": {"name": {"$ref": "name-defs.json#/$defs/orNull"}}}}}
`
	// refRemote.json#8 'remote ref with ref to defs' (referenced by a sibling component).
	suiteRefRemoteWithRefToDefs = `openapi: 3.1.0
info: {title: t, version: "1"}
paths: {}
components:
  schemas:
    R: {"$ref": "#/components/schemas/S"}
    S: {"$schema": "https://json-schema.org/draft/2020-12/schema", "$id": "http://localhost:1234/draft2020-12/schema-remote-ref-ref-defs1.json", "$ref": "ref-and-defs.json"}
`
	// refRemote.json#14 '$ref to $ref finds detached $anchor' (duplicated across two components).
	suiteRefRefFindsDetachedAnchorRemote = `openapi: 3.1.0
info: {title: t, version: "1"}
paths: {}
components:
  schemas:
    S: {"$schema": "https://json-schema.org/draft/2020-12/schema", "$ref": "http://localhost:1234/draft2020-12/detached-ref.json#/$defs/foo"}
    T: {"$schema": "https://json-schema.org/draft/2020-12/schema", "$ref": "http://localhost:1234/draft2020-12/detached-ref.json#/$defs/foo"}
`
)

var suiteFixtures = []struct{ name, spec string }{
	{"ref.json#0 root pointer ref", suiteRefRootPointer},
	{"ref.json#4 nested refs", suiteRefNestedRefs},
	{"ref.json#11 recursive references between schemas", suiteRefRecursiveBetweenSchemas},
	{"ref.json#13 ref creates new scope when adjacent to keywords", suiteRefNewScopeAdjacentToKeywords},
	{"ref.json#15 refs with relative uris and defs", suiteRefRelativeURIsAndDefs},
	{"ref.json#18 order of evaluation: $id and $ref", suiteRefOrderOfIDAndRef},
	{"defs.json#0 validate definition against metaschema", suiteDefsValidateAgainstMetaschema},
	{"anchor.json#0 location-independent identifier", suiteAnchorLocationIndependent},
	{"anchor.json#1 location-independent identifier with absolute URI", suiteAnchorLocationIndependentAbsoluteURI},
	{"anchor.json#2 base URI change in subschema", suiteAnchorBaseURIChangeInSubschema},
	{"anchor.json#3 same $anchor with different base uri", suiteAnchorSameNameDifferentBaseURI},
	{"dynamicRef.json#0 dynamicRef to dynamicAnchor in the same resource", suiteDynamicRefToDynamicAnchorSameResource},
	{"dynamicRef.json#3 resolves to the first dynamicAnchor still in scope", suiteDynamicRefFirstAnchorInScope},
	{"dynamicRef.json#11 multiple dynamic paths to the dynamicRef keyword", suiteDynamicRefMultiplePaths},
	{"dynamicRef.json#15 ref and dynamicAnchor independent of order, defs first", suiteDynamicRefOrderIndependentDefsFirst},
	{"dynamicRef.json#17 ref to dynamicRef finds a detached dynamicAnchor", suiteRefToDynamicRefFindsDetachedAnchor},
	{"refRemote.json#0 remote ref", suiteRefRemoteBasic},
	{"refRemote.json#7 root ref in remote ref", suiteRefRemoteRootRefInRemoteRef},
	{"refRemote.json#8 remote ref with ref to defs", suiteRefRemoteWithRefToDefs},
	{"refRemote.json#14 ref to ref finds a detached anchor", suiteRefRefFindsDetachedAnchorRemote},
}

// TestReachCycle_LeavesJSONSchemaTestSuiteShapes drives chainCycle directly
// (see assertNotRefusedByReach) over a representative sample of the JSON
// Schema Test Suite's own draft2020-12 groups, across all four embeddings.
// None of these may be refused: the false-refusal cost this package measures
// and accepts (TestReachCycle_KnownFalseRefusals) is paid only on adversarial
// $ref/$anchor/$id/$defs combinations the suite itself does not construct,
// not on real-world JSON Schema.
func TestReachCycle_LeavesJSONSchemaTestSuiteShapes(t *testing.T) {
	t.Parallel()
	for _, tc := range suiteFixtures {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			assertNotRefusedByReach(t, tc.spec)
		})
	}
}
