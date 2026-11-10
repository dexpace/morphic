// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package oas

// draft06Keywords are the keywords of JSON Schema draft 6: its core text's and
// its validation text's, each defined at every minor that lets a schema
// declare a dialect.
func draft06Keywords() []Field {
	c, v := jsonSchemaText(TextDraft06Core), jsonSchemaText(TextDraft06Validation)
	s := From(V31)
	return []Field{
		keyword("$schema", String, s, c(`7. The "$schema" keyword`, `The value of this keyword MUST be a URI [RFC3986] (containing a scheme) and this URI MUST be normalized.`)).formatted(AbsoluteURI),
		keyword("$id", String, s, c(`9.2. The "$id" keyword`, `If present, the value for this keyword MUST be a string, and MUST represent a valid URI-reference [RFC3986].`)).
			formatted(PlainNameURI, c(`9.2. The "$id" keyword`, `The fragment identifier MUST begin with a letter ([A-Za-z]), followed by any number of letters, digits ([0-9]), hyphens ("-"), underscores ("_"), colons (":"), or periods (".").`)),
		keyword("$ref", String, s, c(`8. Schema references with $ref`, `An object schema with a "$ref" property MUST be interpreted as a "$ref" reference.`)).formatted(URI),
		keyword("multipleOf", PositiveNumber, s, v(`6.1. multipleOf`, `The value of "multipleOf" MUST be a number, strictly greater than 0.`)),
		keyword("maximum", Number, s, v(`6.2. maximum`, `The value of "maximum" MUST be a number`)),
		keyword("exclusiveMaximum", Number, s, v(`6.3. exclusiveMaximum`, `The value of "exclusiveMaximum" MUST be number`)),
		keyword("minimum", Number, s, v(`6.4. minimum`, `The value of "minimum" MUST be a number`)),
		keyword("exclusiveMinimum", Number, s, v(`6.5. exclusiveMinimum`, `The value of "exclusiveMinimum" MUST be number`)),
		keyword("maxLength", Natural, s, v(`6.6. maxLength`, `The value of this keyword MUST be a non-negative integer.`)),
		keyword("minLength", Natural, s, v(`6.7. minLength`, `The value of this keyword MUST be a non-negative integer.`)),
		keyword("pattern", String, s, v(`6.8. pattern`, `The value of this keyword MUST be a string.`)),
		schemaOf("items", s, v(`6.9. items`, `The value of "items" MUST be either a valid JSON Schema or an array of valid JSON Schemas.`)).also(List, s),
		schemaOf("additionalItems", s, v(`6.10. additionalItems`, `The value of "additionalItems" MUST be a valid JSON Schema.`)),
		keyword("maxItems", Natural, s, v(`6.11. maxItems`, `The value of this keyword MUST be a non-negative integer.`)),
		keyword("minItems", Natural, s, v(`6.12. minItems`, `The value of this keyword MUST be a non-negative integer.`)),
		keyword("uniqueItems", Boolean, s, v(`6.13. uniqueItems`, `The value of this keyword MUST be a boolean.`)),
		schemaOf("contains", s, v(`6.14. contains`, `The value of this keyword MUST be a valid JSON Schema.`)),
		keyword("maxProperties", Natural, s, v(`6.15. maxProperties`, `The value of this keyword MUST be a non-negative integer.`)),
		keyword("minProperties", Natural, s, v(`6.16. minProperties`, `The value of this keyword MUST be a non-negative integer.`)),
		keyword("required", StringList, s, v(`6.17. required`, `The value of this keyword MUST be an array.`)),
		schemaMapOf("properties", s, v(`6.18. properties`, `The value of "properties" MUST be an object.`)),
		schemaMapOf("patternProperties", s, v(`6.19. patternProperties`, `The value of "patternProperties" MUST be an object.`)),
		schemaOf("additionalProperties", s, v(`6.20. additionalProperties`, `The value of "additionalProperties" MUST be a valid JSON Schema.`)),
		keyword("dependencies", RawMap, s, v(`6.21. dependencies`, `This keyword's value MUST be an object.`)),
		schemaOf("propertyNames", s, v(`6.22. propertyNames`, `The value of "propertyNames" MUST be a valid JSON Schema.`)),
		keyword("enum", RawList, s, v(`6.23. enum`, `The value of this keyword MUST be an array.`)),
		keyword("const", Any, s, v(`6.24. const`, `The value of this keyword MAY be of any type, including null.`)),
		keyword("type", String, s, v(`6.25. type`, `The value of this keyword MUST be either a string or an array.`)).
			admits(vals("array", "boolean", "integer", "null", "number", "object", "string")...).also(StringList, s),
		schemasOf("allOf", s, v(`6.26. allOf`, `This keyword's value MUST be a non-empty array.`)),
		schemasOf("anyOf", s, v(`6.27. anyOf`, `This keyword's value MUST be a non-empty array.`)),
		schemasOf("oneOf", s, v(`6.28. oneOf`, `This keyword's value MUST be a non-empty array.`)),
		schemaOf("not", s, v(`6.29. not`, `This keyword's value MUST be a valid JSON Schema.`)),
		schemaMapOf("definitions", s, v(`7.1. definitions`, `This keyword's value MUST be an object.`)),
		keyword("title", String, s, v(`7.2. "title" and "description"`, `The value of both of these keywords MUST be a string.`)),
		keyword("description", String, s, v(`7.2. "title" and "description"`, `The value of both of these keywords MUST be a string.`)),
		keyword("default", Any, s, v(`7.3. "default"`, `There are no restrictions placed on the value of this keyword.`)),
		keyword("examples", RawList, s, v(`7.4. "examples"`, `The value of this keyword MUST be an array.`)),
		keyword("format", String, s, v(`8.1. Foreword`, `It MUST be a string.`)),
	}
}

// draft07Keywords are the keywords of JSON Schema draft 7: its core text's and
// its validation text's, each defined at every minor that lets a schema
// declare a dialect.
func draft07Keywords() []Field {
	c, v := jsonSchemaText(TextDraft07Core), jsonSchemaText(TextDraft07Validation)
	s := From(V31)
	return []Field{
		keyword("$schema", String, s, c(`7. The "$schema" Keyword`, `The value of this keyword MUST be a URI [RFC3986] (containing a scheme) and this URI MUST be normalized.`)).formatted(AbsoluteURI),
		keyword("$id", String, s, c(`8.2. The "$id" Keyword`, `If present, the value for this keyword MUST be a string, and MUST represent a valid URI-reference [RFC3986].`)).
			formatted(PlainNameURI, c(`8.2.3. Location-independent identifiers`, `This value MUST begin with the number sign that specifies a fragment ("#"), then a letter ([A-Za-z]), followed by any number of letters, digits ([0-9]), hyphens ("-"), underscores ("_"), colons (":"), or periods (".").`)),
		keyword("$ref", String, s, c(`8.3. Schema References With "$ref"`, `The value of the "$ref" property MUST be a URI Reference.`)).formatted(URI),
		keyword("$comment", String, s, c(`9. Comments With "$comment"`, `The value of this keyword MUST be a string.`)),
		keyword("type", String, s, v(`6.1.1. type`, `The value of this keyword MUST be either a string or an array.`)).
			admits(vals("array", "boolean", "integer", "null", "number", "object", "string")...).also(StringList, s),
		keyword("enum", RawList, s, v(`6.1.2. enum`, `The value of this keyword MUST be an array.`)),
		keyword("const", Any, s, v(`6.1.3. const`, `The value of this keyword MAY be of any type, including null.`)),
		keyword("multipleOf", PositiveNumber, s, v(`6.2.1. multipleOf`, `The value of "multipleOf" MUST be a number, strictly greater than 0.`)),
		keyword("maximum", Number, s, v(`6.2.2. maximum`, `The value of "maximum" MUST be a number`)),
		keyword("exclusiveMaximum", Number, s, v(`6.2.3. exclusiveMaximum`, `The value of "exclusiveMaximum" MUST be number`)),
		keyword("minimum", Number, s, v(`6.2.4. minimum`, `The value of "minimum" MUST be a number`)),
		keyword("exclusiveMinimum", Number, s, v(`6.2.5. exclusiveMinimum`, `The value of "exclusiveMinimum" MUST be number`)),
		keyword("maxLength", Natural, s, v(`6.3.1. maxLength`, `The value of this keyword MUST be a non-negative integer.`)),
		keyword("minLength", Natural, s, v(`6.3.2. minLength`, `The value of this keyword MUST be a non-negative integer.`)),
		keyword("pattern", String, s, v(`6.3.3. pattern`, `The value of this keyword MUST be a string.`)),
		schemaOf("items", s, v(`6.4.1. items`, `The value of "items" MUST be either a valid JSON Schema or an array of valid JSON Schemas.`)).also(List, s),
		schemaOf("additionalItems", s, v(`6.4.2. additionalItems`, `The value of "additionalItems" MUST be a valid JSON Schema.`)),
		keyword("maxItems", Natural, s, v(`6.4.3. maxItems`, `The value of this keyword MUST be a non-negative integer.`)),
		keyword("minItems", Natural, s, v(`6.4.4. minItems`, `The value of this keyword MUST be a non-negative integer.`)),
		keyword("uniqueItems", Boolean, s, v(`6.4.5. uniqueItems`, `The value of this keyword MUST be a boolean.`)),
		schemaOf("contains", s, v(`6.4.6. contains`, `The value of this keyword MUST be a valid JSON Schema.`)),
		keyword("maxProperties", Natural, s, v(`6.5.1. maxProperties`, `The value of this keyword MUST be a non-negative integer.`)),
		keyword("minProperties", Natural, s, v(`6.5.2. minProperties`, `The value of this keyword MUST be a non-negative integer.`)),
		keyword("required", StringList, s, v(`6.5.3. required`, `The value of this keyword MUST be an array.`)),
		schemaMapOf("properties", s, v(`6.5.4. properties`, `The value of "properties" MUST be an object.`)),
		schemaMapOf("patternProperties", s, v(`6.5.5. patternProperties`, `The value of "patternProperties" MUST be an object.`)),
		schemaOf("additionalProperties", s, v(`6.5.6. additionalProperties`, `The value of "additionalProperties" MUST be a valid JSON Schema.`)),
		keyword("dependencies", RawMap, s, v(`6.5.7. dependencies`, `This keyword's value MUST be an object.`)),
		schemaOf("propertyNames", s, v(`6.5.8. propertyNames`, `The value of "propertyNames" MUST be a valid JSON Schema.`)),
		schemaOf("if", s, v(`6.6.1. if`, `This keyword's value MUST be a valid JSON Schema.`)),
		schemaOf("then", s, v(`6.6.2. then`, `This keyword's value MUST be a valid JSON Schema.`)),
		schemaOf("else", s, v(`6.6.3. else`, `This keyword's value MUST be a valid JSON Schema.`)),
		schemasOf("allOf", s, v(`6.7.1. allOf`, `This keyword's value MUST be a non-empty array.`)),
		schemasOf("anyOf", s, v(`6.7.2. anyOf`, `This keyword's value MUST be a non-empty array.`)),
		schemasOf("oneOf", s, v(`6.7.3. oneOf`, `This keyword's value MUST be a non-empty array.`)),
		schemaOf("not", s, v(`6.7.4. not`, `This keyword's value MUST be a valid JSON Schema.`)),
		keyword("format", String, s, v(`7.1. Foreword`, `It MUST be a string.`)),
		keyword("contentEncoding", String, s, v(`8.3. contentEncoding`, `The value of this property MUST be a string.`)),
		keyword("contentMediaType", String, s, v(`8.4. contentMediaType`, `The value of this property MUST be a string.`)),
		schemaMapOf("definitions", s, v(`9. Schema Re-Use With "definitions"`, `This keyword's value MUST be an object.`)),
		keyword("title", String, s, v(`10.1. "title" and "description"`, `The value of both of these keywords MUST be a string.`)),
		keyword("description", String, s, v(`10.1. "title" and "description"`, `The value of both of these keywords MUST be a string.`)),
		keyword("default", Any, s, v(`10.2. "default"`, `There are no restrictions placed on the value of this keyword.`)),
		keyword("readOnly", Boolean, s, v(`10.3. "readOnly" and "writeOnly"`, `The value of these keywords MUST be a boolean.`)),
		keyword("writeOnly", Boolean, s, v(`10.3. "readOnly" and "writeOnly"`, `The value of these keywords MUST be a boolean.`)),
		keyword("examples", RawList, s, v(`10.4. "examples"`, `The value of this keyword MUST be an array.`)),
	}
}

// draft2019Keywords are the keywords of JSON Schema 2019-09: its core text's and
// its validation text's, each defined at every minor that lets a schema
// declare a dialect.
func draft2019Keywords() []Field {
	c, v := jsonSchemaText(TextDraft2019Core), jsonSchemaText(TextDraft2019Validation)
	s := From(V31)
	return []Field{
		keyword("$schema", String, s, c(`8.1.1. The "$schema" Keyword`, `The value of this keyword MUST be a URI [RFC3986] (containing a scheme) and this URI MUST be normalized.`)).formatted(AbsoluteURI),
		keyword("$vocabulary", RawMap, s, c(`8.1.2. The "$vocabulary" Keyword`, `The value of this keyword MUST be an object.`)),
		keyword("$id", String, s, c(`8.2.2. The "$id" Keyword`, `Therefore, "$id" MUST NOT contain a non-empty fragment, and SHOULD NOT contain an empty fragment.`)).formatted(ResourceURI),
		keyword("$anchor", String, s, c(`8.2.3. Defining location-independent identifiers with "$anchor"`, `If present, the value of this keyword MUST be a string, which MUST start with a letter ([A-Za-z]), followed by any number of letters, digits ([0-9]), hyphens ("-"), underscores ("_"), colons (":"), or periods (".").`)).formatted(PlainName),
		keyword("$ref", String, s, c(`8.2.4.1. Direct References with "$ref"`, `The value of the "$ref" property MUST be a string which is a URI-Reference.`)).formatted(URI),
		keyword("$recursiveRef", String, s, c(`8.2.4.2.1. Dynamically recursive references with "$recursiveRef"`, `The value of the "$recursiveRef" property MUST be a string which is a URI-reference.`)).formatted(URI),
		keyword("$recursiveAnchor", Boolean, s, c(`8.2.4.2.2. Enabling Recursion with "$recursiveAnchor"`, `The value of the "$recursiveAnchor" property MUST be a boolean.`)),
		schemaMapOf("$defs", s, c(`8.2.5. Schema Re-Use With "$defs"`, `This keyword's value MUST be an object.`)),
		keyword("$comment", String, s, c(`8.3. Comments With "$comment"`, `The value of this keyword MUST be a string.`)),
		schemasOf("allOf", s, c(`9.2.1.1. allOf`, `This keyword's value MUST be a non-empty array.`)),
		schemasOf("anyOf", s, c(`9.2.1.2. anyOf`, `This keyword's value MUST be a non-empty array.`)),
		schemasOf("oneOf", s, c(`9.2.1.3. oneOf`, `This keyword's value MUST be a non-empty array.`)),
		schemaOf("not", s, c(`9.2.1.4. not`, `This keyword's value MUST be a valid JSON Schema.`)),
		schemaOf("if", s, c(`9.2.2.1. if`, `This keyword's value MUST be a valid JSON Schema.`)),
		schemaOf("then", s, c(`9.2.2.2. then`, `This keyword's value MUST be a valid JSON Schema.`)),
		schemaOf("else", s, c(`9.2.2.3. else`, `This keyword's value MUST be a valid JSON Schema.`)),
		schemaMapOf("dependentSchemas", s, c(`9.2.2.4. dependentSchemas`, `This keyword's value MUST be an object.`)),
		schemaOf("items", s, c(`9.3.1.1. items`, `The value of "items" MUST be either a valid JSON Schema or an array of valid JSON Schemas.`)).also(List, s),
		schemaOf("additionalItems", s, c(`9.3.1.2. additionalItems`, `The value of "additionalItems" MUST be a valid JSON Schema.`)),
		schemaOf("unevaluatedItems", s, c(`9.3.1.3. unevaluatedItems`, `The value of "unevaluatedItems" MUST be a valid JSON Schema.`)),
		schemaOf("contains", s, c(`9.3.1.4. contains`, `The value of this keyword MUST be a valid JSON Schema.`)),
		schemaMapOf("properties", s, c(`9.3.2.1. properties`, `The value of "properties" MUST be an object.`)),
		schemaMapOf("patternProperties", s, c(`9.3.2.2. patternProperties`, `The value of "patternProperties" MUST be an object.`)),
		schemaOf("additionalProperties", s, c(`9.3.2.3. additionalProperties`, `The value of "additionalProperties" MUST be a valid JSON Schema.`)),
		schemaOf("unevaluatedProperties", s, c(`9.3.2.4. unevaluatedProperties`, `The value of "unevaluatedProperties" MUST be a valid JSON Schema.`)),
		schemaOf("propertyNames", s, c(`9.3.2.5. propertyNames`, `The value of "propertyNames" MUST be a valid JSON Schema.`)),
		keyword("type", String, s, v(`6.1.1. type`, `The value of this keyword MUST be either a string or an array.`)).
			admits(vals("array", "boolean", "integer", "null", "number", "object", "string")...).also(StringList, s),
		keyword("enum", RawList, s, v(`6.1.2. enum`, `The value of this keyword MUST be an array.`)),
		keyword("const", Any, s, v(`6.1.3. const`, `The value of this keyword MAY be of any type, including null.`)),
		keyword("multipleOf", PositiveNumber, s, v(`6.2.1. multipleOf`, `The value of "multipleOf" MUST be a number, strictly greater than 0.`)),
		keyword("maximum", Number, s, v(`6.2.2. maximum`, `The value of "maximum" MUST be a number`)),
		keyword("exclusiveMaximum", Number, s, v(`6.2.3. exclusiveMaximum`, `The value of "exclusiveMaximum" MUST be number`)),
		keyword("minimum", Number, s, v(`6.2.4. minimum`, `The value of "minimum" MUST be a number`)),
		keyword("exclusiveMinimum", Number, s, v(`6.2.5. exclusiveMinimum`, `The value of "exclusiveMinimum" MUST be number`)),
		keyword("maxLength", Natural, s, v(`6.3.1. maxLength`, `The value of this keyword MUST be a non-negative integer.`)),
		keyword("minLength", Natural, s, v(`6.3.2. minLength`, `The value of this keyword MUST be a non-negative integer.`)),
		keyword("pattern", String, s, v(`6.3.3. pattern`, `The value of this keyword MUST be a string.`)),
		keyword("maxItems", Natural, s, v(`6.4.1. maxItems`, `The value of this keyword MUST be a non-negative integer.`)),
		keyword("minItems", Natural, s, v(`6.4.2. minItems`, `The value of this keyword MUST be a non-negative integer.`)),
		keyword("uniqueItems", Boolean, s, v(`6.4.3. uniqueItems`, `The value of this keyword MUST be a boolean.`)),
		keyword("maxContains", Natural, s, v(`6.4.4. maxContains`, `The value of this keyword MUST be a non-negative integer.`)),
		keyword("minContains", Natural, s, v(`6.4.5. minContains`, `The value of this keyword MUST be a non-negative integer.`)),
		keyword("maxProperties", Natural, s, v(`6.5.1. maxProperties`, `The value of this keyword MUST be a non-negative integer.`)),
		keyword("minProperties", Natural, s, v(`6.5.2. minProperties`, `The value of this keyword MUST be a non-negative integer.`)),
		keyword("required", StringList, s, v(`6.5.3. required`, `The value of this keyword MUST be an array.`)),
		keyword("dependentRequired", RawMap, s, v(`6.5.4. dependentRequired`, `The value of this keyword MUST be an object.`)),
		keyword("format", String, s, v(`7.1. Foreword`, `It MUST be a string.`)),
		keyword("contentEncoding", String, s, v(`8.3. contentEncoding`, `The value of this property MUST be a string.`)),
		keyword("contentMediaType", String, s, v(`8.4. contentMediaType`, `The value of this property MUST be a string`)),
		schemaOf("contentSchema", s, v(`8.5. contentSchema`, `this property contains a schema which describes the structure of the string.`)),
		keyword("title", String, s, v(`9.1. "title" and "description"`, `The value of both of these keywords MUST be a string.`)),
		keyword("description", String, s, v(`9.1. "title" and "description"`, `The value of both of these keywords MUST be a string.`)),
		keyword("default", Any, s, v(`9.2. "default"`, `There are no restrictions placed on the value of this keyword.`)),
		keyword("deprecated", Boolean, s, v(`9.3. "deprecated"`, `The value of this keyword MUST be a boolean.`)),
		keyword("readOnly", Boolean, s, v(`9.4. "readOnly" and "writeOnly"`, `The value of these keywords MUST be a boolean.`)),
		keyword("writeOnly", Boolean, s, v(`9.4. "readOnly" and "writeOnly"`, `The value of these keywords MUST be a boolean.`)),
		keyword("examples", RawList, s, v(`9.5. "examples"`, `The value of this keyword MUST be an array.`)),
	}
}
