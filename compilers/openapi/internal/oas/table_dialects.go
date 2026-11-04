// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package oas

// dialect is a JSON Schema dialect a 3.1 or 3.2 Schema Object can declare,
// through its `$schema` or its document's jsonSchemaDialect: the URI naming
// it, and the keywords it reads in place of the minor's, or none where they
// are the minor's own.
type dialect struct {
	id       string
	keywords []Field
	cites    []Cite
}

// schemaDialects are the dialects the table has rows for: the OAS dialect and
// JSON Schema 2020-12, whose keywords are the minor's, and JSON Schema draft
// 4. Each is named by the id its meta-schema declares.
func schemaDialects() []dialect {
	c := sectioned("Schema Object")
	defined := c(V31, "in addition to the vocabularies as specified in the JSON Schema Specification Draft 2020-12")
	others := c(V31, "This allows use of Schema Objects which comply with other drafts of JSON Schema than the default Draft 2020-12 support.")
	return []dialect{
		{id: "https://spec.openapis.org/oas/3.1/dialect/base", cites: []Cite{c(V31, "The OpenAPI Schema Object dialect for this "+
			"version of the specification is identified by the URI `https://spec.openapis.org/oas/3.1/dialect/base`")}},
		{id: "https://json-schema.org/draft/2020-12/schema", cites: []Cite{defined}},
		{id: "http://json-schema.org/draft-04/schema#", keywords: draft04Keywords(), cites: []Cite{others}},
	}
}

// draft04 returns a constructor of citations of the JSON Schema draft 4 text
// named, under one of its headings.
func draft04(text string) func(section, sentence string) Cite {
	return func(section, sentence string) Cite {
		return Cite{Text: text, Section: section, Sentence: sentence}
	}
}

// draft04Keywords are the keywords of JSON Schema draft 4: its core text's and
// its validation text's. Each is defined at every minor that lets a schema
// declare a dialect.
func draft04Keywords() []Field {
	core := draft04("draft-zyp-json-schema-04")
	ref := draft04("draft-pbryan-zyp-json-ref-03")("3. Syntax", "A JSON Reference is a JSON object, which contains a member named")
	v := draft04("draft-fge-json-schema-validation-00")
	s := From(V31)
	integer := "The value of this keyword MUST be an integer."
	return []Field{
		keyword("$schema", String, s, core(`6.1. Purpose`, `this URI MUST be both absolute and normalized.`)).formatted(AbsoluteURI),
		keyword("id", String, s, core(`7.2.1. Valid values`, `The value for this keyword MUST be a string, and MUST be a valid URI.`)),
		keyword("$ref", String, s, ref).formatted(URI),
		keyword("multipleOf", PositiveNumber, s, v(`5.1.1.1. Valid values`, `The value of "multipleOf" MUST be a JSON number.`)),
		keyword("maximum", Number, s, v(`5.1.2.1. Valid values`, `The value of "maximum" MUST be a JSON number.`)),
		keyword("exclusiveMaximum", Boolean, s, v(`5.1.2.1. Valid values`, `"exclusiveMaximum" MUST be a boolean.`)),
		keyword("minimum", Number, s, v(`5.1.3.1. Valid values`, `The value of "minimum" MUST be a JSON number.`)),
		keyword("exclusiveMinimum", Boolean, s, v(`5.1.3.1. Valid values`, `"exclusiveMinimum" MUST be a boolean.`)),
		keyword("maxLength", Natural, s, v(`5.2.1.1. Valid values`, integer)),
		keyword("minLength", Natural, s, v(`5.2.2.1. Valid values`, integer)),
		keyword("pattern", String, s, v(`5.2.3.1. Valid values`, `The value of this keyword MUST be a string.`)),
		schemaOf("additionalItems", s, v(`5.3.1.1. Valid values`, `The value of "additionalItems" MUST be either a boolean or an object.`)).
			also(Boolean, s),
		schemaOf("items", s, v(`5.3.1.1. Valid values`, `The value of "items" MUST be either an object or an array.`)).also(List, s),
		keyword("maxItems", Natural, s, v(`5.3.2.1. Valid values`, integer)),
		keyword("minItems", Natural, s, v(`5.3.3.1. Valid values`, integer)),
		keyword("uniqueItems", Boolean, s, v(`5.3.4.1. Valid values`, `The value of this keyword MUST be a boolean.`)),
		keyword("maxProperties", Natural, s, v(`5.4.1.1. Valid values`, integer)),
		keyword("minProperties", Natural, s, v(`5.4.2.1. Valid values`, integer)),
		keyword("required", StringList, s, v(`5.4.3.1. Valid values`, `The value of this keyword MUST be an array.`)),
		schemaOf("additionalProperties", s, v(`5.4.4.1. Valid values`, `The value of "additionalProperties" MUST be a boolean or an object.`)).
			also(Boolean, s),
		schemaMapOf("properties", s, v(`5.4.4.1. Valid values`, `The value of "properties" MUST be an object.`)),
		schemaMapOf("patternProperties", s, v(`5.4.4.1. Valid values`, `The value of "patternProperties" MUST be an object.`)),
		keyword("dependencies", RawMap, s, v(`5.4.5.1. Valid values`, `This keyword's value MUST be an object.`)),
		keyword("enum", RawList, s, v(`5.5.1.1. Valid values`, `The value of this keyword MUST be an array.`)),
		keyword("type", String, s, v(`5.5.2.1. Valid values`, `The value of this keyword MUST be either a string or an array.`)).
			admits(vals("array", "boolean", "integer", "null", "number", "object", "string")...).also(StringList, s),
		schemasOf("allOf", s, v(`5.5.3.1. Valid values`, `This keyword's value MUST be an array.`)),
		schemasOf("anyOf", s, v(`5.5.4.1. Valid values`, `This keyword's value MUST be an array.`)),
		schemasOf("oneOf", s, v(`5.5.5.1. Valid values`, `This keyword's value MUST be an array.`)),
		schemaOf("not", s, v(`5.5.6.1. Valid values`, `This keyword's value MUST be an object.`)),
		schemaMapOf("definitions", s, v(`5.5.7.1. Valid values`, `This keyword's value MUST be an object.`)),
		keyword("title", String, s, v(`6.1.1. Valid values`, `The value of both of these keywords MUST be a string.`)),
		keyword("description", String, s, v(`6.1.1. Valid values`, `The value of both of these keywords MUST be a string.`)),
		keyword("default", Any, s, v(`6.2.1. Valid values`, `There are no restrictions placed on the value of this keyword.`)),
		keyword("format", String, s, v(`7.1. Foreword`, `It MUST be a string.`)),
	}
}
