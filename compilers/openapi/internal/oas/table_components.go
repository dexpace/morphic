// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package oas

// componentObjects returns the objects that hold and refer to reusable ones.
func componentObjects() []Object {
	return []Object{componentsObject(), referenceObject()}
}

// componentsObject is the Components Object.
func componentsObject() Object {
	c := sectioned("Components Object")
	names := c(V30, "All the fixed fields declared above are objects that MUST use keys that match the regular expression")
	keyed := func(name string, k Kind, sentence string) Field {
		return mapOf(name, k, c(V30, sentence)).keyed(ComponentName, names)
	}
	added := func(name string, k Kind, m Minor, sentence string) Field {
		return mapOf(name, k, c(m, sentence)).during(From(m)).keyed(ComponentName, names)
	}
	return Object{
		Kind: Components, Name: "Components Object", Extensible: true,
		Fields: []Field{
			keyed("schemas", Schema, "An object to hold reusable [Schema Objects](#schema-object)."),
			keyed("responses", Response, "An object to hold reusable [Response Objects](#response-object)."),
			keyed("parameters", Parameter, "An object to hold reusable [Parameter Objects](#parameter-object)."),
			keyed("examples", Example, "An object to hold reusable [Example Objects](#example-object)."),
			keyed("requestBodies", RequestBody, "An object to hold reusable [Request Body Objects](#request-body-object)."),
			keyed("headers", Header, "An object to hold reusable [Header Objects](#header-object)."),
			keyed("securitySchemes", SecurityScheme, "An object to hold reusable [Security Scheme Objects](#security-scheme-object)."),
			keyed("links", Link, "An object to hold reusable [Link Objects](#link-object)."),
			keyed("callbacks", Callback, "An object to hold reusable [Callback Objects](#callback-object)."),
			added("pathItems", PathItem, V31, "An object to hold reusable [Path Item Objects](#path-item-object)."),
			added("mediaTypes", MediaType, V32, "An object to hold reusable [Media Type Objects](#media-type-object)."),
		},
	}
}

// referenceObject is the Reference Object. It admits nothing beside its
// fields: any property added is ignored.
func referenceObject() Object {
	c := sectioned("Reference Object")
	return Object{
		Kind: Reference, Name: "Reference Object",
		Fields: []Field{
			field("$ref", String, c(V30, "**REQUIRED**. The reference string."), c(V31, "**REQUIRED**. The reference identifier. This MUST be in the form of a URI.")).
				required(Always()).formatted(URI),
			field("summary", String, c(V31, "A short summary which by default SHOULD override that of the referenced component.")).
				during(From(V31)),
			field("description", String, c(V31, "A description which by default SHOULD override that of the referenced component.")).
				during(From(V31)),
		},
	}
}
