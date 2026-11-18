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
	in := func(s object) (object, object) { return nil, object{{caseSchema, s}} }
	return []position{
		{"component", func(s object) (object, object) {
			user := object{{"type", "object"}, {"properties", object{{"p", object{{"$ref", "#/components/schemas/" + caseSchema}}}}}}
			return nil, object{{caseSchema, s}, {userSchema, user}}
		}, nil, "/components/schemas/C", every},
		{"property", func(s object) (object, object) {
			return in(object{{"type", "object"}, {"properties", object{{"p", s}}}})
		}, carryProperties, "/components/schemas/C/properties/p", every},
		{"items", func(s object) (object, object) { return in(object{{"type", "array"}, {"items", s}}) }, nil, "/components/schemas/C/items", every},
		{"additionalProperties", func(s object) (object, object) {
			return in(object{{"type", "object"}, {"additionalProperties", s}})
		}, nil, "/components/schemas/C/additionalProperties", every},
		{"branch", func(s object) (object, object) {
			return in(object{{"oneOf", []any{s, object{{"type", "boolean"}}}}})
		}, nil, "/components/schemas/C/oneOf/0", every},
		{name: "anyOfBranch", place: func(s object) (object, object) {
			return in(object{{"anyOf", []any{s, object{{"type", "boolean"}}}}})
		}, pointer: "/components/schemas/C/anyOf/0"},
		{name: "prefixItem", place: func(s object) (object, object) {
			return in(object{{"type", "array"}, {"prefixItems", []any{s}}})
		}, pointer: "/components/schemas/C/prefixItems/0", span: from31},
		{name: "patternProperty", place: func(s object) (object, object) {
			return in(object{{"type", "object"}, {"patternProperties", object{{"^x", s}}}})
		}, pointer: "/components/schemas/C/patternProperties/^x", span: from31},
		{"parameter", func(s object) (object, object) {
			param := object{{"name", "q"}, {"in", "query"}, {"schema", s}}
			return atCasePath(object{{"get", object{{"parameters", []any{param}}, {"responses", okResponses}}}})
		}, carryParameters, "/paths/~1c/get/parameters/0/schema", every},
		{"requestBody", func(s object) (object, object) {
			body := object{{"content", object{{"application/json", object{{"schema", s}}}}}}
			return atCasePath(object{{"post", object{{"requestBody", body}, {"responses", okResponses}}}})
		}, nil, "/paths/~1c/post/requestBody/content/application~1json/schema", every},
		{"responseBody", func(s object) (object, object) {
			return atCasePath(response(member{"content", object{{"application/json", object{{"schema", s}}}}}))
		}, nil, "/paths/~1c/get/responses/200/content/application~1json/schema", every},
		{"header", func(s object) (object, object) {
			return atCasePath(response(member{"headers", object{{"H", object{{"schema", s}}}}}))
		}, carryHeaders, "/paths/~1c/get/responses/200/headers/H/schema", every},
	}
}

// atCasePath returns the paths holding op at the case's path, and no component.
func atCasePath(op object) (object, object) { return object{{casePath, op}}, nil }

// response returns the operation whose response 200 writes of.
func response(of member) object {
	return object{{"get", object{{"responses", object{{"200", object{{"description", "ok"}, of}}}}}}}
}
