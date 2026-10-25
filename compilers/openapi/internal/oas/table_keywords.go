package oas

import "slices"

// schemaKeywords returns the JSON Schema keywords a Schema Object takes: the
// subset OpenAPI 3.0 names, and the 2020-12 vocabularies 3.1 and 3.2 adopt.
func schemaKeywords() []Field {
	fields := slices.Concat(keywords30(), dialectCore(), dialectApplicators(), dialectValidation(), dialectAnnotations())
	for i, f := range fields {
		switch f.Name {
		case "minimum", "maximum", "multipleOf", "exclusiveMinimum", "exclusiveMaximum":
			// The schema lowering reads a bound off the raw node, a quoted number as the
			// decimal it spells, and reports one that is no decimal a BigVal holds or is
			// written in the other dialect's form, so the table leaves the shape to it,
			// bar a multipleOf written as a number no greater than 0.
			fields[i] = f.shapeOwnedBy(ownerSchema)
		}
	}
	return fields
}

// keyword returns a JSON Schema keyword defined during span s.
func keyword(name string, shape Shape, s Span, cite Cite) Field {
	return field(name, shape, cite).during(s).keyword()
}

// schemaOf returns a keyword holding a schema.
func schemaOf(name string, s Span, cite Cite) Field {
	f := keyword(name, Nested, s, cite)
	f.Of = Schema
	return f
}

// schemasOf returns a keyword holding a list of schemas.
func schemasOf(name string, s Span, cite Cite) Field {
	f := keyword(name, List, s, cite)
	f.Of = Schema
	return f
}

// schemaMapOf returns a keyword holding a map of schemas.
func schemaMapOf(name string, s Span, cite Cite) Field {
	f := keyword(name, Map, s, cite)
	f.Of = Schema
	return f
}

// keywords30 are the keywords OpenAPI 3.0 takes from JSON Schema, a subset of
// its fifth draft with the adjustments the specification lists.
func keywords30() []Field {
	c := sectioned("Schema Object")
	direct := c(V30, "The following keywords are taken directly from the JSON Schema definition and follow the same specifications:")
	adjust := func(sentence string) Cite { return c(V30, sentence) }
	s := Only(V30)
	composed := adjust("Inline or referenced schema MUST be of a [Schema Object](#schema-object) and not a standard JSON Schema.")
	return []Field{
		keyword("$ref", String, s, adjust("Alternatively, any time a Schema Object can be used, a "+
			"[Reference Object](#reference-object) can be used in its place.")).formatted(URI),
		keyword("title", String, s, direct),
		keyword("multipleOf", PositiveNumber, s, direct),
		keyword("maximum", Number, s, direct),
		keyword("exclusiveMaximum", Boolean, s, direct),
		keyword("minimum", Number, s, direct),
		keyword("exclusiveMinimum", Boolean, s, direct),
		keyword("maxLength", Natural, s, direct),
		keyword("minLength", Natural, s, direct),
		keyword("pattern", String, s, direct),
		keyword("maxItems", Natural, s, direct),
		keyword("minItems", Natural, s, direct),
		keyword("uniqueItems", Boolean, s, direct),
		keyword("maxProperties", Natural, s, direct),
		keyword("minProperties", Natural, s, direct),
		keyword("required", StringList, s, direct),
		keyword("enum", RawList, s, direct),
		keyword("type", String, s, adjust("Value MUST be a string. Multiple types via an array are not supported.")).
			admits(vals("array", "boolean", "integer", "number", "object", "string")...),
		schemasOf("allOf", s, composed), schemasOf("oneOf", s, composed), schemasOf("anyOf", s, composed),
		schemaOf("not", s, composed),
		schemaOf("items", s, adjust("Value MUST be an object and not an array.")),
		schemaMapOf("properties", s, adjust("Property definitions MUST be a [Schema Object](#schema-object) and not a standard JSON Schema (inline or referenced).")),
		schemaOf("additionalProperties", s, adjust("Value can be boolean or object.")).also(Boolean, s),
		keyword("description", String, s, adjust("[CommonMark syntax](https://spec.commonmark.org/) MAY be used for rich text representation.")),
		keyword("format", String, s, adjust("See [Data Type Formats](#data-type-format) for further details.")),
		keyword("default", Any, s, adjust("The default value represents what would be assumed by the consumer of the input")),
		keyword("readOnly", Boolean, s, c(V30, "Declares the property as \"read only\".")),
		keyword("writeOnly", Boolean, s, c(V30, "Declares the property as \"write only\".")),
		keyword("deprecated", Boolean, s, c(V30, "Specifies that a schema is deprecated and SHOULD be transitioned out of usage.")),
	}
}

// The citation every 2020-12 keyword rests on: the dialect keeps the meaning
// JSON Schema gives them.
const dialectSentence = "Unless stated otherwise, the keyword definitions follow those of JSON Schema and do not add any additional semantics"

// dialectCore are the core vocabulary's keywords.
func dialectCore() []Field {
	cite := sectioned("Schema Object")(V31, dialectSentence)
	s := From(V31)
	return []Field{
		keyword("$id", String, s, cite).formatted(ResourceURI),
		keyword("$schema", String, s, cite).formatted(AbsoluteURI),
		keyword("$ref", String, s, cite).formatted(URI),
		keyword("$anchor", String, s, cite).formatted(AnchorName).nonEmpty(s),
		keyword("$dynamicRef", String, s, cite).formatted(URI),
		keyword("$dynamicAnchor", String, s, cite).formatted(AnchorName).nonEmpty(s),
		keyword("$vocabulary", RawMap, s, cite),
		keyword("$comment", String, s, cite),
		schemaMapOf("$defs", s, cite),
	}
}

// dialectApplicators are the applicator and unevaluated vocabularies' keywords.
func dialectApplicators() []Field {
	cite := sectioned("Schema Object")(V31, dialectSentence)
	s := From(V31)
	single := []string{
		"items", "contains", "additionalProperties", "propertyNames", "if", "then", "else", "not",
		"unevaluatedItems", "unevaluatedProperties", "contentSchema",
	}
	singles := make([]Field, len(single))
	for i, name := range single {
		singles[i] = schemaOf(name, s, cite)
	}
	return slices.Concat([]Field{
		schemasOf("prefixItems", s, cite),
		schemasOf("allOf", s, cite), schemasOf("anyOf", s, cite), schemasOf("oneOf", s, cite),
		schemaMapOf("properties", s, cite), schemaMapOf("patternProperties", s, cite),
		schemaMapOf("dependentSchemas", s, cite),
	}, singles)
}

// dialectValidation are the validation vocabulary's keywords.
func dialectValidation() []Field {
	cite := sectioned("Schema Object")(V31, dialectSentence)
	s := From(V31)
	types := vals("array", "boolean", "integer", "number", "object", "string")
	types = append(types, valFrom("null", V31))
	return slices.Concat([]Field{
		keyword("type", String, s, cite).admits(types...).also(StringList, s),
		keyword("const", Any, s, cite),
		keyword("enum", RawList, s, cite),
		keyword("multipleOf", PositiveNumber, s, cite),
		keyword("required", StringList, s, cite),
		keyword("dependentRequired", RawMap, s, cite),
		keyword("pattern", String, s, cite),
		keyword("uniqueItems", Boolean, s, cite),
	},
		keywordsOf(Number, s, cite, "maximum", "minimum", "exclusiveMaximum", "exclusiveMinimum"),
		keywordsOf(Natural, s, cite, "maxLength", "minLength", "maxItems", "minItems", "maxContains", "minContains",
			"maxProperties", "minProperties"),
	)
}

// keywordsOf returns the keywords of one shape that share a citation.
func keywordsOf(shape Shape, s Span, cite Cite, names ...string) []Field {
	out := make([]Field, len(names))
	for i, name := range names {
		out[i] = keyword(name, shape, s, cite)
	}
	return out
}

// dialectAnnotations are the format, content and meta-data vocabularies'
// keywords.
func dialectAnnotations() []Field {
	cite := sectioned("Schema Object")(V31, dialectSentence)
	s := From(V31)
	return []Field{
		keyword("format", String, s, cite),
		keyword("contentEncoding", String, s, cite),
		keyword("contentMediaType", String, s, cite),
		keyword("title", String, s, cite),
		keyword("description", String, s, cite),
		keyword("default", Any, s, cite),
		keyword("deprecated", Boolean, s, cite),
		keyword("readOnly", Boolean, s, cite),
		keyword("writeOnly", Boolean, s, cite),
		keyword("examples", RawList, s, cite),
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
