// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package specrules

import "slices"

// schemaKeywords returns the JSON Schema keywords a Schema Object takes: the
// subset OpenAPI 3.0 names, and the 2020-12 vocabularies 3.1 and 3.2 adopt.
func schemaKeywords() []Field {
	return readBounds(slices.Concat(keywords30(), dialectCore(), dialectApplicators(), dialectValidation(),
		dialectAnnotations()))
}

// readBounds marks each numeric bound of fields, a JSON Schema dialect's
// keywords, read as the decimal it spells (Field.Decimal): the schema lowering
// reads a bound off the raw node, a quoted number as the decimal it spells, so
// the check judges one as it reads it, bar a multipleOf written as a number no
// greater than 0, which its shape refuses.
func readBounds(fields []Field) []Field {
	for i, field := range fields {
		switch field.Name {
		case "minimum", "maximum", "multipleOf", "exclusiveMinimum", "exclusiveMaximum":
			fields[i] = field.decimal()
		}
	}
	return fields
}

// keyword returns a JSON Schema keyword defined during span minorVersionRange.
func keyword(name string, shape Shape, minorVersionRange MinorVersionRange, cite Cite) Field {
	return field(name, shape, cite).during(minorVersionRange).keyword()
}

// schemaOf returns a keyword holding a schema.
func schemaOf(name string, minorVersionRange MinorVersionRange, cite Cite) Field {
	field := keyword(name, Nested, minorVersionRange, cite)
	field.Of = Schema
	return field
}

// schemasOf returns a keyword holding a list of schemas.
func schemasOf(name string, minorVersionRange MinorVersionRange, cite Cite) Field {
	field := keyword(name, List, minorVersionRange, cite)
	field.Of = Schema
	return field
}

// schemaMapOf returns a keyword holding a map of schemas.
func schemaMapOf(name string, minorVersionRange MinorVersionRange, cite Cite) Field {
	field := keyword(name, Map, minorVersionRange, cite)
	field.Of = Schema
	return field
}

// keywords30 are the keywords OpenAPI 3.0 takes from JSON Schema, a subset of
// its fifth draft with the adjustments the specification lists.
func keywords30() []Field {
	c := sectioned("Schema Object")
	direct := c(V30, "The following keywords are taken directly from the JSON Schema definition and follow the same specifications:")
	adjust := func(sentence string) Cite { return c(V30, sentence) }
	minorVersionRange := Only(V30)
	composed := adjust("Inline or referenced schema MUST be of a [Schema Object](#schema-object) and not a standard JSON Schema.")
	return []Field{
		keyword("$ref", String, minorVersionRange, adjust("Alternatively, any time a Schema Object can be used, a "+
			"[Reference Object](#reference-object) can be used in its place.")).formatted(URI),
		keyword("title", String, minorVersionRange, direct),
		keyword("multipleOf", PositiveNumber, minorVersionRange, direct),
		keyword("maximum", Number, minorVersionRange, direct),
		keyword("exclusiveMaximum", Boolean, minorVersionRange, direct),
		keyword("minimum", Number, minorVersionRange, direct),
		keyword("exclusiveMinimum", Boolean, minorVersionRange, direct),
		keyword("maxLength", Natural, minorVersionRange, direct),
		keyword("minLength", Natural, minorVersionRange, direct),
		keyword("pattern", String, minorVersionRange, direct),
		keyword("maxItems", Natural, minorVersionRange, direct),
		keyword("minItems", Natural, minorVersionRange, direct),
		keyword("uniqueItems", Boolean, minorVersionRange, direct),
		keyword("maxProperties", Natural, minorVersionRange, direct),
		keyword("minProperties", Natural, minorVersionRange, direct),
		keyword("required", StringList, minorVersionRange, direct),
		keyword("enum", RawList, minorVersionRange, direct),
		keyword("type", String, minorVersionRange, adjust("Value MUST be a string. Multiple types via an array are not supported.")).
			admits(vals("array", "boolean", "integer", "number", "object", "string")...),
		schemasOf("allOf", minorVersionRange, composed), schemasOf("oneOf", minorVersionRange, composed), schemasOf("anyOf", minorVersionRange, composed),
		schemaOf("not", minorVersionRange, composed),
		schemaOf("items", minorVersionRange, adjust("Value MUST be an object and not an array.")),
		schemaMapOf("properties", minorVersionRange, adjust("Property definitions MUST be a [Schema Object](#schema-object) and not a standard JSON Schema (inline or referenced).")),
		schemaOf("additionalProperties", minorVersionRange, adjust("Value can be boolean or object.")).also(Boolean, minorVersionRange),
		keyword("description", String, minorVersionRange, adjust("[CommonMark syntax](https://spec.commonmark.org/) MAY be used for rich text representation.")),
		keyword("format", String, minorVersionRange, adjust("See [Data Type Formats](#data-type-format) for further details.")),
		keyword("default", AnyShapeUnchecked, minorVersionRange, adjust("The default value represents what would be assumed by the consumer of the input")),
		keyword("readOnly", Boolean, minorVersionRange, c(V30, "Declares the property as \"read only\".")),
		keyword("writeOnly", Boolean, minorVersionRange, c(V30, "Declares the property as \"write only\".")),
		keyword("deprecated", Boolean, minorVersionRange, c(V30, "Specifies that a schema is deprecated and SHOULD be transitioned out of usage.")),
	}
}

// The citation every 2020-12 keyword rests on: the dialect keeps the meaning
// JSON Schema gives them.
const dialectSentence = "Unless stated otherwise, the keyword definitions follow those of JSON Schema and do not add any additional semantics"

// dialectCore are the core vocabulary's keywords.
func dialectCore() []Field {
	cite := sectioned("Schema Object")(V31, dialectSentence)
	minorVersionRange := From(V31)
	return []Field{
		keyword("$id", String, minorVersionRange, cite).formatted(ResourceURI),
		keyword("$schema", String, minorVersionRange, cite).formatted(AbsoluteURI),
		keyword("$ref", String, minorVersionRange, cite).formatted(URI),
		keyword("$anchor", String, minorVersionRange, cite).formatted(AnchorName).nonEmpty(minorVersionRange),
		keyword("$dynamicRef", String, minorVersionRange, cite).formatted(URI),
		keyword("$dynamicAnchor", String, minorVersionRange, cite).formatted(AnchorName).nonEmpty(minorVersionRange),
		keyword("$vocabulary", RawMap, minorVersionRange, cite),
		keyword("$comment", String, minorVersionRange, cite),
		schemaMapOf("$defs", minorVersionRange, cite),
	}
}

// dialectApplicators are the applicator and unevaluated vocabularies' keywords.
func dialectApplicators() []Field {
	cite := sectioned("Schema Object")(V31, dialectSentence)
	minorVersionRange := From(V31)
	single := []string{
		"items", "contains", "additionalProperties", "propertyNames", "if", "then", "else", "not",
		"unevaluatedItems", "unevaluatedProperties", "contentSchema",
	}
	singles := make([]Field, len(single))
	for i, name := range single {
		singles[i] = schemaOf(name, minorVersionRange, cite)
	}
	return slices.Concat([]Field{
		schemasOf("prefixItems", minorVersionRange, cite),
		schemasOf("allOf", minorVersionRange, cite), schemasOf("anyOf", minorVersionRange, cite), schemasOf("oneOf", minorVersionRange, cite),
		schemaMapOf("properties", minorVersionRange, cite), schemaMapOf("patternProperties", minorVersionRange, cite),
		schemaMapOf("dependentSchemas", minorVersionRange, cite),
	}, singles)
}

// dialectValidation are the validation vocabulary's keywords.
func dialectValidation() []Field {
	cite := sectioned("Schema Object")(V31, dialectSentence)
	minorVersionRange := From(V31)
	types := vals("array", "boolean", "integer", "number", "object", "string")
	types = append(types, valFrom("null", V31))
	return slices.Concat([]Field{
		keyword("type", String, minorVersionRange, cite).admits(types...).also(StringList, minorVersionRange),
		keyword("const", AnyShapeUnchecked, minorVersionRange, cite),
		keyword("enum", RawList, minorVersionRange, cite),
		keyword("multipleOf", PositiveNumber, minorVersionRange, cite),
		keyword("required", StringList, minorVersionRange, cite),
		keyword("dependentRequired", RawMap, minorVersionRange, cite),
		keyword("pattern", String, minorVersionRange, cite),
		keyword("uniqueItems", Boolean, minorVersionRange, cite),
	},
		keywordsOf(Number, minorVersionRange, cite, "maximum", "minimum", "exclusiveMaximum", "exclusiveMinimum"),
		keywordsOf(Natural, minorVersionRange, cite, "maxLength", "minLength", "maxItems", "minItems", "maxContains", "minContains",
			"maxProperties", "minProperties"),
	)
}

// keywordsOf returns the keywords of one shape that share a citation.
func keywordsOf(shape Shape, minorVersionRange MinorVersionRange, cite Cite, names ...string) []Field {
	out := make([]Field, len(names))
	for i, name := range names {
		out[i] = keyword(name, shape, minorVersionRange, cite)
	}
	return out
}

// dialectAnnotations are the format, content and meta-data vocabularies'
// keywords.
func dialectAnnotations() []Field {
	cite := sectioned("Schema Object")(V31, dialectSentence)
	minorVersionRange := From(V31)
	return []Field{
		keyword("format", String, minorVersionRange, cite),
		keyword("contentEncoding", String, minorVersionRange, cite),
		keyword("contentMediaType", String, minorVersionRange, cite),
		keyword("title", String, minorVersionRange, cite),
		keyword("description", String, minorVersionRange, cite),
		keyword("default", AnyShapeUnchecked, minorVersionRange, cite),
		keyword("deprecated", Boolean, minorVersionRange, cite),
		keyword("readOnly", Boolean, minorVersionRange, cite),
		keyword("writeOnly", Boolean, minorVersionRange, cite),
		keyword("examples", RawList, minorVersionRange, cite),
	}
}

// keywordRules are what JSON Schema requires of keywords' values beyond their
// shapes: a list of schemas, and from 3.1 prefixItems and a list of types,
// holds an entry; the names required lists, and from 3.1 the types a list
// gives, differ; and from 3.1 dependentRequired and $vocabulary hold what
// their keywords define.
func keywordRules() []Rule {
	c := sectioned("Schema Object")
	composed := c(V30, "Inline or referenced schema MUST be of a [Schema Object](#schema-object) and not a standard JSON Schema.")
	direct := c(V30, "The following keywords are taken directly from the JSON Schema definition and follow the same specifications:")
	dialect := c(V31, dialectSentence)
	var rules []Rule
	for _, name := range []string{"allOf", "anyOf", "oneOf"} {
		rules = append(rules, count(Always(), Must, name, 0, composed, dialect))
	}
	for _, name := range []string{"prefixItems", "type"} {
		rules = append(rules, count(From(V31), Must, name, 0, dialect))
	}
	return append(rules,
		distinct(Always(), "required", direct, dialect),
		distinct(From(V31), "type", dialect),
		code(From(V31), "dependent-required", "", dialect),
		code(From(V31), "vocabulary", "", dialect),
	)
}
