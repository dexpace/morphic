// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package openapicases

// position is a place a document writes a case's schema: its name, place,
// which returns the paths and the component schemas holding schema there, and
// the pointer of the schema place writes.
type position struct {
	name    string
	place   func(schema object) (paths, schemas object)
	pointer string
}

// caseSchema is the component, and casePath the path, a position writes the
// case's schema in; userSchema is the component naming the case's own.
const (
	caseSchema = "C"
	userSchema = "U"
	casePath   = "/c"
)

// ok is the responses of an operation whose response writes no schema.
const ok = raw(`{"200": {"description": "ok"}}`)

// positions returns the positions, in the order the position axis lists them.
// The component position also writes a property of a second component naming
// the case's by $ref, as a fact lowered onto a use is seen at the use alone.
func positions() []position {
	in := func(s object) (object, object) { return nil, object{{caseSchema, s}} }
	at := func(op object) (object, object) { return object{{casePath, op}}, nil }
	response := func(of member) object {
		return object{{"get", object{{"responses", object{{"200", object{{"description", "ok"}, of}}}}}}}
	}
	return []position{
		{"component", func(s object) (object, object) {
			user := object{{"type", "object"}, {"properties", object{{"p", object{{"$ref", "#/components/schemas/" + caseSchema}}}}}}
			return nil, object{{caseSchema, s}, {userSchema, user}}
		}, "/components/schemas/C"},
		{"property", func(s object) (object, object) {
			return in(object{{"type", "object"}, {"properties", object{{"p", s}}}})
		}, "/components/schemas/C/properties/p"},
		{"items", func(s object) (object, object) { return in(object{{"type", "array"}, {"items", s}}) }, "/components/schemas/C/items"},
		{"additionalProperties", func(s object) (object, object) {
			return in(object{{"type", "object"}, {"additionalProperties", s}})
		}, "/components/schemas/C/additionalProperties"},
		{"branch", func(s object) (object, object) {
			return in(object{{"oneOf", []any{s, object{{"type", "boolean"}}}}})
		}, "/components/schemas/C/oneOf/0"},
		{"parameter", func(s object) (object, object) {
			param := object{{"name", "q"}, {"in", "query"}, {"schema", s}}
			return at(object{{"get", object{{"parameters", []any{param}}, {"responses", ok}}}})
		}, "/paths/~1c/get/parameters/0/schema"},
		{"requestBody", func(s object) (object, object) {
			body := object{{"content", object{{"application/json", object{{"schema", s}}}}}}
			return at(object{{"post", object{{"requestBody", body}, {"responses", ok}}}})
		}, "/paths/~1c/post/requestBody/content/application~1json/schema"},
		{"responseBody", func(s object) (object, object) {
			return at(response(member{"content", object{{"application/json", object{{"schema", s}}}}}))
		}, "/paths/~1c/get/responses/200/content/application~1json/schema"},
		{"header", func(s object) (object, object) {
			return at(response(member{"headers", object{{"H", object{{"schema", s}}}}}))
		}, "/paths/~1c/get/responses/200/headers/H/schema"},
	}
}
