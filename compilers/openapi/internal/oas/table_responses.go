// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package oas

// responseObjects returns the objects that describe what an operation returns.
func responseObjects() []Object {
	return []Object{responsesObject(), responseObject(), exampleObject(), linkObject()}
}

// responsesObject is the Responses Object.
func responsesObject() Object {
	c := sectioned("Responses Object")
	return Object{
		Kind: Responses, Name: "Responses Object", Extensible: true,
		Fields: []Field{
			objectOf("default", Response, c(V30, "The documentation of responses other than the ones declared for specific HTTP response codes.")),
		},
		Patterned: []Pattern{
			pattern("[HTTP Status Code]", Nested, Response,
				c(V30, "but only one property per code, to describe the expected response for that HTTP status code.")),
		},
		Rules: []Rule{
			count(Always(), Must, "", 0, c(V30, "The Responses Object MUST contain at least one response code")),
			code(Always(), "status-keys", ownerOperation,
				c(V30, "Only the following range definitions are allowed: `1XX`, `2XX`, `3XX`, `4XX`, and `5XX`.")),
		},
	}
}

// responseObject is the Response Object.
func responseObject() Object {
	c := sectioned("Response Object")
	return Object{
		Kind: Response, Name: "Response Object", Extensible: true, Referenceable: Always(),
		Fields: []Field{
			field("summary", String, c(V32, "A short summary of the meaning of the response.")).during(From(V32)),
			field("description", String, c(V30, "**REQUIRED**. A description of the response.")).required(Through(V31)),
			mapOf("headers", Header, c(V30, "Maps a header name to its definition.")),
			mapOf("content", MediaType, c(V30, "A map containing descriptions of potential response payloads.")),
			mapOf("links", Link, c(V30, "A map of operations links that can be followed from the response.")).
				keyed(ComponentName, c(V30, "following the naming constraints of the names for")),
		},
	}
}

// exampleObject is the Example Object.
func exampleObject() Object {
	c := sectioned("Example Object")
	return Object{
		Kind: Example, Name: "Example Object", Extensible: true, Referenceable: Always(),
		Fields: []Field{
			field("summary", String, c(V30, "Short description for the example.")),
			field("description", String, c(V30, "Long description for the example.")),
			field("dataValue", Any, c(V32, "An example of the data structure that MUST be valid according to the relevant")).
				during(From(V32)),
			field("serializedValue", String, c(V32, "An example of the serialized form of the value")).during(From(V32)),
			field("externalValue", String, c(V30, "A URL that points to the literal example.")).formatted(URI),
			field("value", Any, c(V30, "Embedded literal example.")),
		},
		Rules: exampleRules(c),
	}
}

// exampleRules keep the Example Object from stating its value twice.
func exampleRules(c func(Minor, string) Cite) []Rule {
	return []Rule{
		exclusive(Always(), fields("value", "externalValue"),
			c(V30, "The `value` field and `externalValue` field are mutually exclusive.")),
		exclusive(From(V32), fields("dataValue", "value"), c(V32, "If this field is present, `value` MUST be absent.")),
		exclusive(From(V32), fields("serializedValue", "value"),
			c(V32, "If this field is present, `value`, and `externalValue` MUST be absent.")),
		exclusive(From(V32), fields("serializedValue", "externalValue"),
			c(V32, "If this field is present, `value`, and `externalValue` MUST be absent.")),
	}
}

// linkObject is the Link Object.
func linkObject() Object {
	c := sectioned("Link Object")
	return Object{
		Kind: Link, Name: "Link Object", Extensible: true, Referenceable: Always(),
		Fields: []Field{
			field("operationRef", String, c(V30, "A URI reference to an OAS operation.")).formatted(URI),
			field("operationId", String, c(V30, "The name of an _existing_, resolvable OAS operation")),
			field("parameters", RawMap, c(V30, "A map representing parameters to pass to an operation")),
			field("requestBody", Any, c(V30, "A literal value or [{expression}](#runtime-expressions) to use as a request body")),
			field("description", String, c(V30, "A description of the link.")),
			objectOf("server", Server, c(V30, "A server object to be used by the target operation.")),
		},
		Rules: []Rule{
			exactlyOne(Always(), fields("operationRef", "operationId"),
				c(V30, "A linked operation MUST be identified using either an `operationRef` or `operationId`.")),
		},
	}
}
