// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package oas

import "slices"

// pathObjects returns the objects that describe the paths of the API.
func pathObjects() []Object {
	return []Object{pathsObject(), pathItemObject(), operationObject(), callbackObject()}
}

// pathsObject is the Paths Object.
func pathsObject() Object {
	c := sectioned("Paths Object")
	t := sectioned("Path Templating")
	return Object{
		Kind: Paths, Name: "Paths Object", Extensible: true,
		Patterned: []Pattern{
			pattern("/{path}", Nested, PathItem, c(V30, "A relative path to an individual endpoint.")),
		},
		Rules: []Rule{
			code(Always(), "path-keys", ownerOperation, c(V30, "The field name MUST begin with a forward slash (`/`).")),
			code(Always(), "path-templates-distinct", "",
				c(V30, "Templated paths with the same hierarchy but different templated names MUST NOT exist as they are identical.")),
			code(Always(), "path-template-parameters", "",
				t(V30, "Each template expression in the path MUST correspond to a path parameter that is included in the")),
		},
	}
}

// pathItemObject is the Path Item Object.
func pathItemObject() Object {
	c := sectioned("Path Item Object")
	head := []Field{
		field("$ref", String, c(V30, "Allows for a referenced definition of this path item.")).formatted(URI),
		field("summary", String, c(V30, "An optional string summary, intended to apply to all operations in this path.")),
		field("description", String, c(V30, "An optional string description, intended to apply to all operations in this path.")),
	}
	methods := []string{"get", "put", "post", "delete", "options", "head", "patch", "trace"}
	ops := make([]Field, len(methods))
	for i, m := range methods {
		ops[i] = objectOf(m, Operation, c(V30, "A definition of a "+upper(m)+" operation on this path."))
	}
	tail := []Field{
		objectOf("query", Operation, c(V32, "A definition of a QUERY operation, as defined in the most recent IETF draft")).
			during(From(V32)),
		mapOf("additionalOperations", Operation, c(V32, "A map of additional operations on this path.")).during(From(V32)),
		listOf("servers", Server, c(V30, "An alternative `servers` array to service all operations in this path.")),
		listOf("parameters", Parameter, c(V30, "A list of parameters that are applicable for all the operations described under this path.")),
	}
	return Object{
		Kind: PathItem, Name: "Path Item Object", Extensible: true, Fields: slices.Concat(head, ops, tail),
		Rules: []Rule{
			code(Always(), "parameters-unique", ownerLoad, c(V30, "The list MUST NOT include duplicated parameters.")),
			uniqueNames(From(V32), "servers",
				sectioned("Server Object")(V32, "An optional unique string to refer to the host designated by the URL.")).
				owned(ownerLoad),
			code(From(V32), "additional-operations-methods", "",
				c(V32, "This map MUST NOT contain any entry for the methods that can be defined by other fixed fields")),
		},
	}
}

// upper spells an HTTP method in capitals.
func upper(method string) string {
	b := []byte(method)
	for i, ch := range b {
		if ch >= 'a' && ch <= 'z' {
			b[i] = ch - 'a' + 'A'
		}
	}
	return string(b)
}

// operationObject is the Operation Object.
func operationObject() Object {
	c := sectioned("Operation Object")
	return Object{
		Kind: Operation, Name: "Operation Object", Extensible: true,
		Fields: []Field{
			field("tags", StringList, c(V30, "A list of tags for API documentation control.")),
			field("summary", String, c(V30, "A short summary of what the operation does.")),
			field("description", String, c(V30, "A verbose explanation of the operation behavior.")),
			objectOf("externalDocs", ExternalDocs, c(V30, "Additional external documentation for this operation.")),
			field("operationId", String, c(V30, "Unique string used to identify the operation.")),
			listOf("parameters", Parameter, c(V30, "A list of parameters that are applicable for this operation.")),
			objectOf("requestBody", RequestBody, c(V30, "The request body applicable for this operation.")),
			objectOf("responses", Responses, c(V30, "**REQUIRED**. The list of possible responses as they are returned from executing this operation.")).
				required(Only(V30)),
			mapOf("callbacks", Callback, c(V30, "A map of possible out-of band callbacks related to the parent operation.")),
			field("deprecated", Boolean, c(V30, "Declares this operation to be deprecated.")),
			listOf("security", SecurityRequirement,
				c(V30, "A declaration of which security mechanisms can be used for this operation.")),
			listOf("servers", Server, c(V30, "An alternative `servers` array to service this operation.")),
		},
		Rules: []Rule{
			code(Always(), "parameters-unique", ownerLoad, c(V30, "The list MUST NOT include duplicated parameters.")),
			uniqueNames(From(V32), "servers",
				sectioned("Server Object")(V32, "An optional unique string to refer to the host designated by the URL.")).
				owned(ownerLoad),
		},
	}
}

// callbackObject is the Callback Object.
func callbackObject() Object {
	c := sectioned("Callback Object")
	return Object{
		Kind: Callback, Name: "Callback Object", Extensible: true, Referenceable: Always(),
		Patterned: []Pattern{
			pattern("{expression}", Nested, PathItem, c(V30, "A Path Item Object used to define a callback request and expected responses."),
				// 3.0 closes the Runtime Expressions examples with this, under a heading of their own.
				sectioned("Examples")(V30, "Expressions can be embedded into string values by surrounding the expression with `{}` curly braces."),
				sectioned("Key Expression")(V30, "The key that identifies the [Path Item Object](#path-item-object) is a [runtime expression](#runtime-expressions)")).
				keyed(RuntimeExpression),
		},
	}
}
