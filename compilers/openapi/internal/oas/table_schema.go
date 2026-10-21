package oas

import "slices"

// schemaObjects returns the Schema Object and the objects only it holds.
func schemaObjects() []Object {
	return []Object{schemaObject(), discriminatorObject(), xmlObject()}
}

// schemaObject is the Schema Object: the fields OpenAPI defines on it and the
// JSON Schema keywords of the dialect it declares (schemaKeywords). A $ref is a
// Reference Object in 3.0 and a keyword from 3.1, and from 3.1 true and false
// are schemas.
func schemaObject() Object {
	c := sectioned("Schema Object")
	own := []Field{
		field("nullable", Boolean, c(V30, "This keyword only takes effect if `type` is explicitly defined within the same Schema Object.")).
			during(Only(V30)),
		objectOf("discriminator", Discriminator, c(V30, "Adds support for polymorphism.")),
		objectOf("xml", XML, c(V30, "This MAY be used only on property schemas.")),
		objectOf("externalDocs", ExternalDocs, c(V30, "Additional external documentation for this schema.")),
		field("example", Any, c(V30, "A free-form field to include an example of an instance for this schema.")),
	}
	return Object{
		Kind: Schema, Name: "Schema Object", Extensible: true,
		Referenceable: Only(V30), Boolean: From(V31),
		Fields: slices.Concat(own, schemaKeywords()),
		Rules: []Rule{
			mustBe(Only(V30), "writeOnly", "false", when(term("readOnly", "true")),
				c(V30, "A property MUST NOT be marked as both `readOnly` and `writeOnly` being `true`.")),
			requiredWhen(Only(V30), "items", when(term("type", "array")),
				c(V30, "`items` MUST be present if `type` is `\"array\"`.")).owned("schema"),
		},
	}
}

// discriminatorObject is the Discriminator Object.
func discriminatorObject() Object {
	c := sectioned("Discriminator Object")
	return Object{
		Kind: Discriminator, Name: "Discriminator Object", Extensible: true,
		Fields: []Field{
			field("propertyName", String, c(V30, "**REQUIRED**. The name of the property in the payload that will hold the discriminating value."),
				c(V32, "**REQUIRED**. The name of the discriminating property in the payload that will hold the discriminating value.")).
				required(Always()),
			field("mapping", StringMap, c(V30, "An object to hold mappings between payload values and schema names or URI references.")),
			field("defaultMapping", String, c(V32, "The schema name or URI reference to a schema that is expected to validate the structure of the model")).
				during(From(V32)),
		},
	}
}

// xmlObject is the XML Object.
func xmlObject() Object {
	c := sectioned("XML Object")
	nodeTypes := vals("element", "attribute", "text", "cdata", "none")
	present := "If `nodeType` is present, this field MUST NOT be present."
	return Object{
		Kind: XML, Name: "XML Object", Extensible: true,
		Fields: []Field{
			field("nodeType", String, c(V32, "One of `element`, `attribute`, `text`, `cdata`, or `none`")).
				during(From(V32)).admits(nodeTypes...),
			field("name", String, c(V30, "Replaces the name of the element/attribute used for the described schema property."),
				c(V32, "Sets the name of the element/attribute corresponding to the schema")),
			field("namespace", String, c(V30, "Value MUST be in the form of a non-relative URI.")).formatted(AbsoluteURI),
			field("prefix", String, c(V30, "The prefix to be used for the [name](#xml-name).")),
			field("attribute", Boolean, c(V30, "Declares whether the property definition translates to an attribute instead of an element.")),
			field("wrapped", Boolean, c(V30, "MAY be used only for an array definition.")),
		},
		Rules: []Rule{
			exclusive(From(V32), fields("nodeType", "attribute"), c(V32, present)),
			exclusive(From(V32), fields("nodeType", "wrapped"), c(V32, present)),
		},
	}
}
