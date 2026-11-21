// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package openapicases

// position is a place a document writes a case's schema: its name, and place,
// which returns the paths and the component schemas holding schema there.
// Where the validate pass judges a default, carry writes a carrier of each of
// uses there, and returns the ID each carrier compiles to. pointer is the
// pointer of the schema place writes, and span the versions it is written at.
type position struct {
	name    string
	place   func(schema object) (paths, schemas object)
	carry   func(uses []object) (paths, schemas object, nodes []string)
	pointer string
	span    span
}

// caseSchema is the component, and casePath the path, a position writes the
// case's schema in; userSchema is the component naming the case's own.
const (
	caseSchema = "C"
	userSchema = "U"
	casePath   = "/c"
)

// okResponses is the responses of an operation whose response writes no schema.
const okResponses = raw(`{"200": {"description": "ok"}}`)

// positions returns the positions, in the order the position axis lists them.
// The component position also writes a property of a second component naming
// the case's by $ref, as a fact lowered onto a use is seen at the use alone.
func positions() []position {
	in := func(objectValue object) (object, object) { return nil, object{{caseSchema, objectValue}} }
	return []position{
		{"component", func(objectValue object) (object, object) {
			user := object{{"type", "object"}, {"properties", object{{"p", object{{"$ref", "#/components/schemas/" + caseSchema}}}}}}
			return nil, object{{caseSchema, objectValue}, {userSchema, user}}
		}, nil, "/components/schemas/C", every},
		{"property", func(objectValue object) (object, object) {
			return in(object{{"type", "object"}, {"properties", object{{"p", objectValue}}}})
		}, carryProperties, "/components/schemas/C/properties/p", every},
		{"items", func(objectValue object) (object, object) {
			return in(object{{"type", "array"}, {"items", objectValue}})
		}, nil, "/components/schemas/C/items", every},
		{"additionalProperties", func(objectValue object) (object, object) {
			return in(object{{"type", "object"}, {"additionalProperties", objectValue}})
		}, nil, "/components/schemas/C/additionalProperties", every},
		{"branch", func(objectValue object) (object, object) {
			return in(object{{"oneOf", []any{objectValue, object{{"type", "boolean"}}}}})
		}, nil, "/components/schemas/C/oneOf/0", every},
		{name: "anyOfBranch", place: func(objectValue object) (object, object) {
			return in(object{{"anyOf", []any{objectValue, object{{"type", "boolean"}}}}})
		}, pointer: "/components/schemas/C/anyOf/0"},
		{name: "prefixItem", place: func(objectValue object) (object, object) {
			return in(object{{"type", "array"}, {"prefixItems", []any{objectValue}}})
		}, pointer: "/components/schemas/C/prefixItems/0", span: from31},
		{name: "patternProperty", place: func(objectValue object) (object, object) {
			return in(object{{"type", "object"}, {"patternProperties", object{{"^x", objectValue}}}})
		}, pointer: "/components/schemas/C/patternProperties/^x", span: from31},
		{name: "contentSchema", place: func(objectValue object) (object, object) {
			return in(object{{"type", "string"}, {"contentMediaType", "application/json"}, {"contentSchema", objectValue}})
		}, pointer: "/components/schemas/C/contentSchema", span: from31},
		{"parameter", func(objectValue object) (object, object) {
			param := object{{"name", "q"}, {"in", "query"}, {"schema", objectValue}}
			return atCasePath(object{{"get", object{{"parameters", []any{param}}, {"responses", okResponses}}}})
		}, carryParameters, "/paths/~1c/get/parameters/0/schema", every},
		{"requestBody", func(objectValue object) (object, object) {
			body := object{{"content", object{{"application/json", object{{"schema", objectValue}}}}}}
			return atCasePath(object{{"post", object{{"requestBody", body}, {"responses", okResponses}}}})
		}, nil, "/paths/~1c/post/requestBody/content/application~1json/schema", every},
		{"responseBody", func(objectValue object) (object, object) {
			return atCasePath(response(member{"content", object{{"application/json", object{{"schema", objectValue}}}}}))
		}, nil, "/paths/~1c/get/responses/200/content/application~1json/schema", every},
		{"header", func(objectValue object) (object, object) {
			return atCasePath(response(member{"headers", object{{"H", object{{"schema", objectValue}}}}}))
		}, carryHeaders, "/paths/~1c/get/responses/200/headers/H/schema", every},
	}
}

// atCasePath returns the paths holding operation at the case's path, and no
// component.
func atCasePath(operation object) (object, object) { return object{{casePath, operation}}, nil }

// response returns the operation whose response 200 writes of.
func response(of member) object {
	return object{{"get", object{{"responses", object{{"200", object{{"description", "ok"}, of}}}}}}}
}
