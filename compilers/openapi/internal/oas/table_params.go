package oas

// parameterObjects returns the objects that describe how a value is sent.
func parameterObjects() []Object {
	return []Object{
		parameterObject(), headerObject(), requestBodyObject(), mediaTypeObject(), encodingObject(),
	}
}

// styleValues are the serializations a parameter's style names, each with the
// locations that admit it.
func styleValues() []Value {
	return []Value{
		valWhen("matrix", Always(), term("in", "path")),
		valWhen("label", Always(), term("in", "path")),
		valWhen("simple", Always(), term("in", "path", "header")),
		valWhen("form", Always(), term("in", "query", "cookie")),
		valWhen("spaceDelimited", Always(), term("in", "query")),
		valWhen("pipeDelimited", Always(), term("in", "query")),
		valWhen("deepObject", Always(), term("in", "query")),
		valWhen("cookie", From(V32), term("in", "cookie")),
	}
}

// parameterObject is the Parameter Object.
func parameterObject() Object {
	return Object{
		Kind: Parameter, Name: "Parameter Object", Extensible: true, Referenceable: Always(),
		Fields: parameterFields(), Rules: parameterRules(),
	}
}

// parameterFields are the fields of the Parameter Object.
func parameterFields() []Field {
	c := sectioned("Parameter Object")
	where := []Value{val("query"), valFrom("querystring", V32), val("header"), val("path"), val("cookie")}
	return []Field{
		field("name", String, c(V30, "**REQUIRED**. The name of the parameter.")).required(Always()),
		field("in", String, c(V30, "**REQUIRED**. The location of the parameter.")).required(Always()).admits(where...),
		field("description", String, c(V30, "A brief description of the parameter.")),
		field("required", Boolean, c(V30, "Determines whether this parameter is mandatory.")),
		field("deprecated", Boolean, c(V30, "Specifies that a parameter is deprecated and SHOULD be transitioned out of usage.")),
		field("allowEmptyValue", Boolean, c(V30, "If `true`, clients MAY pass a zero-length string value in place of parameters")),
		field("style", String, c(V30, "Describes how the parameter value will be serialized depending on the type of the parameter value.")).
			admits(styleValues()...),
		field("explode", Boolean, c(V30, "When this is true, parameter values of type `array` or `object` generate separate parameters")),
		field("allowReserved", Boolean, c(V30, "When this is true, parameter values are serialized using reserved expansion")),
		objectOf("schema", Schema, c(V30, "The schema defining the type used for the parameter.")),
		field("example", Any, c(V30, "Example of the parameter's potential value; see")),
		mapOf("examples", Example, c(V30, "Examples of the parameter's potential value; see")),
		mapOf("content", MediaType, c(V30, "A map containing the representations for the parameter.")),
	}
}

// parameterRules are the rules of the Parameter Object.
func parameterRules() []Rule {
	c := sectioned("Parameter Object")
	path := when(term("in", "path"))
	return []Rule{
		exactlyOne(Always(), fields("schema", "content"),
			c(V30, "Parameter Objects MUST include either a `content` field or a `schema` field, but not both.")),
		exclusive(Always(), fields("example", "examples"),
			c(V30, "The `example` and `examples` fields are mutually exclusive")),
		count(Always(), Must, "content", 1, c(V30, "The map MUST only contain one entry.")),
		requiredWhen(Always(), "required", path,
			c(V30, "this field is **REQUIRED** and its value MUST be `true`.")),
		mustBe(Always(), "required", "true", path,
			c(V30, "this field is **REQUIRED** and its value MUST be `true`.")),
		syntax(Always(), "name", Token, when(term("in", "header")),
			c(V30, "Note that [RFC7230](https://tools.ietf.org/html/rfc7230#section-3.2) states header names are case insensitive."),
			c(V32, "Note that [RFC9110](https://www.rfc-editor.org/rfc/rfc9110.html#section-5.1) states header names are case-insensitive.")),
		syntax(Always(), "name", Token, when(term("in", "cookie")),
			c(V30, "cookie - Used to pass a specific cookie value to the API."),
			c(V32, "Analogous to `form`, but following [[RFC6265]] `Cookie` syntax rules")),
		forbiddenWhen(Always(), fields("allowEmptyValue"), when(term("in", "path", "header", "cookie", "querystring")),
			c(V30, "This field is valid only for `query` parameters.")),
		inapplicable(Through(V31), "allowReserved", when(term("in", "path", "header", "cookie")), "to in=query alone",
			c(V30, "This field only applies to parameters with an `in` value of `query`.")),
		inapplicable(From(V32), "allowReserved", when(term("in", "header")), "only where the value is percent-encoded",
			c(V32, "This field only applies to `in` and `style` values that automatically percent-encode.")),
		inapplicable(From(V32), "allowReserved", when(term("in", "cookie"), term("style", "cookie")),
			"only where the value is percent-encoded",
			c(V32, "This field only applies to `in` and `style` values that automatically percent-encode.")),
		forbiddenWhen(From(V32), fields("schema"), when(term("in", "querystring")),
			c(V32, "These fields MUST NOT be used with `in: \"querystring\"`.")),
		misplaced(From(V32), fields("style", "explode", "allowReserved"), when(term("in", "querystring")),
			c(V32, "These fields MUST NOT be used with `in: \"querystring\"`.")),
	}
}

// headerObject is the Header Object.
func headerObject() Object {
	c := sectioned("Header Object")
	return Object{
		Kind: Header, Name: "Header Object", Extensible: true, Referenceable: Always(),
		Fields: []Field{
			field("description", String, c(V30, "A brief description of the header.")),
			field("required", Boolean, c(V30, "Determines whether this header is mandatory.")),
			field("deprecated", Boolean, c(V30, "Specifies that the header is deprecated and SHOULD be transitioned out of usage.")),
			field("style", String, c(V30, "Describes how the header value will be serialized.")).admits(val("simple")),
			field("explode", Boolean, c(V30, "When this is true, header values of type `array` or `object` generate a single header")),
			objectOf("schema", Schema, c(V30, "The schema defining the type used for the header.")),
			field("example", Any, c(V30, "Example of the header's potential value; see")),
			mapOf("examples", Example, c(V30, "Examples of the header's potential value; see")),
			mapOf("content", MediaType, c(V30, "A map containing the representations for the header.")),
		},
		Rules: []Rule{
			exclusive(Always(), fields("schema", "content"),
				c(V30, "including determining its serialization strategy based on whether `schema` or `content` is present")),
			exclusive(Always(), fields("example", "examples"),
				c(V30, "The `example` and `examples` fields are mutually exclusive")),
			count(Always(), Must, "content", 1, c(V30, "The map MUST only contain one entry.")),
			// Each is a key the Header Object does not define, which the unknown-key
			// census reports where it is written and keeps verbatim.
			forbiddenWhen(Always(), fields("name", "in"), nil,
				c(V30, "`name` MUST NOT be specified, it is given in the corresponding `headers` map.")).owned(ownerAnnotation),
			forbiddenWhen(Through(V31), fields("allowEmptyValue", "allowReserved"), nil,
				c(V30, "This means that `allowEmptyValue` and `allowReserved` MUST NOT be used")).owned(ownerAnnotation),
			forbiddenWhen(From(V32), fields("allowEmptyValue"), nil,
				c(V32, "This means that `allowEmptyValue` MUST NOT be used")).owned(ownerAnnotation),
		},
	}
}

// requestBodyObject is the Request Body Object.
func requestBodyObject() Object {
	c := sectioned("Request Body Object")
	return Object{
		Kind: RequestBody, Name: "Request Body Object", Extensible: true, Referenceable: Always(),
		Fields: []Field{
			field("description", String, c(V30, "A brief description of the request body.")),
			mapOf("content", MediaType, c(V30, "**REQUIRED**. The content of the request body.")).required(Always()),
			field("required", Boolean, c(V30, "Determines if the request body is required in the request.")),
		},
		Rules: []Rule{
			count(From(V31), Should, "content", 0,
				c(V31, "The map SHOULD have at least one entry; if it does not, the behavior is implementation-defined.")).
				owned(ownerLoad),
		},
	}
}

// mediaTypeObject is the Media Type Object.
func mediaTypeObject() Object {
	c := sectioned("Media Type Object")
	return Object{
		Kind: MediaType, Name: "Media Type Object", Extensible: true, Referenceable: From(V32),
		Fields: []Field{
			// The 3.2.0 text lists no description here; the published 3.2 schema
			// does, and the compiler has always read it from that schema.
			field("description", String, c(V32, "Each Media Type Object describes content structured in accordance with the media type identified by its key.")).
				during(From(V32)),
			objectOf("schema", Schema, c(V30, "The schema defining the content of the request, response, parameter, or header.")),
			objectOf("itemSchema", Schema, c(V32, "A schema describing each item within a [sequential media type]")).during(From(V32)),
			field("example", Any, c(V30, "Example of the media type; see")),
			mapOf("examples", Example, c(V30, "Examples of the media type; see")),
			mapOf("encoding", Encoding, c(V30, "A map between a property name and its encoding information")),
			listOf("prefixEncoding", Encoding, c(V32, "An array of positional encoding information")).during(From(V32)),
			objectOf("itemEncoding", Encoding, c(V32, "A single Encoding Object that provides encoding information for multiple array items")).
				during(From(V32)),
		},
		Rules: append(mediaTypeRules(c), encodingExclusions(c, V32,
			"This field MUST NOT be present if `prefixEncoding` or `itemEncoding` are present.")...),
	}
}

// mediaTypeRules are the rules of the Media Type Object beside its encodings'
// exclusions.
func mediaTypeRules(c func(Minor, string) Cite) []Rule {
	// What an encoding is written for, the specification says it SHALL only
	// apply to, so one written elsewhere has no effect, a warning.
	applies := code(Always(), "encoding-applies", "",
		c(V30, "The `encoding` field SHALL only apply to [Request Body Objects](#request-body-object), and only when the media type is `multipart` or `application/x-www-form-urlencoded`."),
		c(V32, "The `encoding` field SHALL only apply when the media type is `multipart` or `application/x-www-form-urlencoded`."),
		c(V32, "The `prefixEncoding` field SHALL only apply when the media type is `multipart`."))
	applies.Level = Should
	return []Rule{
		exclusive(Always(), fields("example", "examples"),
			c(V30, "The `example` and `examples` fields are mutually exclusive")),
		applies,
	}
}

// encodingExclusions are the rules that keep an object's encoding by name from
// being written beside its encoding by position.
func encodingExclusions(c func(Minor, string) Cite, m Minor, sentence string) []Rule {
	return []Rule{
		exclusive(From(m), fields("encoding", "prefixEncoding"), c(m, sentence)),
		exclusive(From(m), fields("encoding", "itemEncoding"), c(m, sentence)),
	}
}

// encodingObject is the Encoding Object.
func encodingObject() Object {
	c := sectioned("Encoding Object")
	query := []Value{val("form"), val("spaceDelimited"), val("pipeDelimited"), val("deepObject")}
	return Object{
		Kind: Encoding, Name: "Encoding Object", Extensible: true,
		Fields: []Field{
			field("contentType", String, c(V30, "The `Content-Type` for encoding a specific property."),
				c(V30, "The value is a comma-separated list, each element of which is either a specific media type")).
				formatted(MediaRanges).nonEmpty(Always()),
			mapOf("headers", Header, c(V30, "A map allowing additional information to be provided as headers.")),
			mapOf("encoding", Encoding, c(V32, "Applies nested Encoding Objects in the same manner as the")).during(From(V32)),
			listOf("prefixEncoding", Encoding, c(V32, "Applies nested Encoding Objects in the same manner as the")).during(From(V32)),
			objectOf("itemEncoding", Encoding, c(V32, "Applies nested Encoding Objects in the same manner as the")).during(From(V32)),
			field("style", String, c(V30, "Describes how a specific property value will be serialized depending on its type.")).
				admits(query...),
			field("explode", Boolean, c(V30, "When this is true, property values of type `array` or `object` generate separate parameters")),
			field("allowReserved", Boolean, c(V30, "When this is true, parameter values are serialized using reserved expansion")),
		},
		// The nested fields apply "in the same manner" as the Media Type Object's,
		// whose text bars writing them together: a reading by analogy, so a
		// warning.
		Rules: softened(encodingExclusions(c, V32, "Applies nested Encoding Objects in the same manner as the")),
	}
}
