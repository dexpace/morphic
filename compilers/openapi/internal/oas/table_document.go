// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package oas

// documentObjects returns the root object and the objects that describe the
// API as a whole.
func documentObjects() []Object {
	return []Object{
		openAPIObject(), infoObject(), contactObject(), licenseObject(),
		serverObject(), serverVariableObject(), tagObject(), externalDocsObject(),
	}
}

// openAPIObject is the OpenAPI Object, the root of a document.
func openAPIObject() Object {
	c := sectioned("OpenAPI Object")
	return Object{
		Kind: Document, Name: "OpenAPI Object", Extensible: true,
		Fields: []Field{
			field("openapi", String, c(V30, "**REQUIRED**. This string MUST be the"),
				sectioned("Versions")(V30, "The OpenAPI Specification is versioned using a `major`.`minor`.`patch` versioning scheme.")).
				required(Always()).formatted(VersionNumber),
			objectOf("info", Info, c(V30, "**REQUIRED**. Provides metadata about the API.")).required(Always()),
			field("$self", String, c(V32, "This string MUST be in the form of a URI reference as defined by")).
				during(From(V32)).formatted(URI),
			field("jsonSchemaDialect", String, c(V31, "The default value for the `$schema` keyword within")).
				during(From(V31)).formatted(AbsoluteURI),
			listOf("servers", Server, c(V30, "An array of Server Objects, which provide connectivity information")),
			objectOf("paths", Paths, c(V30, "**REQUIRED**. The available paths and operations for the API.")).
				required(Only(V30)),
			mapOf("webhooks", PathItem, c(V31, "The incoming webhooks that MAY be received as part of this API")).
				during(From(V31)),
			objectOf("components", Components, c(V30, "An element to hold various Objects for the OpenAPI Description.")),
			listOf("security", SecurityRequirement,
				c(V30, "A declaration of which security mechanisms can be used across the API.")),
			listOf("tags", Tag, c(V30, "A list of tags used by the OpenAPI Description with additional metadata.")),
			objectOf("externalDocs", ExternalDocs, c(V30, "Additional external documentation.")),
		},
		Rules: []Rule{
			anyOf(Only(V31), fields("paths", "components", "webhooks"),
				sectioned("OpenAPI Description")(V31, "and MUST contain at least one [paths](#paths-object) field")),
			anyOf(From(V32), fields("components", "paths", "webhooks"),
				c(V32, "at least one of the `components`, `paths`, or `webhooks` fields MUST be present.")),
			uniqueNames(Always(), "tags", c(V30, "Each tag name in the list MUST be unique.")),
			code(Always(), "operation-ids-unique", ownerOperation,
				sectioned("Operation Object")(V30, "The id MUST be unique among all operations described in the API.")),
			uniqueNames(From(V32), "servers",
				sectioned("Server Object")(V32, "An optional unique string to refer to the host designated by the URL.")).
				owned(ownerLoad),
			code(From(V32), "tag-parents", "",
				sectioned("Tag Object")(V32, "The named tag MUST exist in the API description")),
			code(Through(V31), "security-requirement-names", ownerAuth,
				sectioned("Security Requirement Object")(V30, "The name used for each property MUST correspond to a security scheme declared in the")),
			code(From(V32), "security-requirement-names", ownerAuth,
				sectioned("Security Requirement Object")(V32, "The name used for each property MUST either correspond to a security scheme declared in the")),
		},
	}
}

// infoObject is the Info Object.
func infoObject() Object {
	c := sectioned("Info Object")
	return Object{
		Kind: Info, Name: "Info Object", Extensible: true,
		Fields: []Field{
			field("title", String, c(V30, "**REQUIRED**. The title of the API.")).required(Always()),
			field("summary", String, c(V31, "A short summary of the API.")).during(From(V31)),
			field("description", String, c(V30, "A description of the API.")),
			field("termsOfService", String, c(V30, "This MUST be in the form of a URL.")).formatted(URI),
			objectOf("contact", Contact, c(V30, "The contact information for the exposed API.")),
			objectOf("license", License, c(V30, "The license information for the exposed API.")),
			field("version", String, c(V30, "**REQUIRED**. The version of the OpenAPI Document")).required(Always()),
		},
	}
}

// contactObject is the Contact Object.
func contactObject() Object {
	c := sectioned("Contact Object")
	return Object{
		Kind: Contact, Name: "Contact Object", Extensible: true,
		Fields: []Field{
			field("name", String, c(V30, "The identifying name of the contact person/organization.")),
			field("url", String, c(V30, "This MUST be in the form of a URL.")).formatted(URI),
			field("email", String, c(V30, "This MUST be in the form of an email address.")).formatted(Email),
		},
	}
}

// licenseObject is the License Object.
func licenseObject() Object {
	c := sectioned("License Object")
	return Object{
		Kind: License, Name: "License Object", Extensible: true,
		Fields: []Field{
			field("name", String, c(V30, "**REQUIRED**. The license name used for the API.")).required(Always()),
			field("identifier", String, c(V31, "An [SPDX](https://spdx.org/licenses/) license expression for the API.")).
				during(From(V31)),
			field("url", String, c(V30, "This MUST be in the form of a URL.")).formatted(URI),
		},
		Rules: []Rule{
			exclusive(From(V31), fields("identifier", "url"),
				c(V31, "The `identifier` field is mutually exclusive of the `url` field.")),
		},
	}
}

// serverObject is the Server Object.
func serverObject() Object {
	c := sectioned("Server Object")
	return Object{
		Kind: Server, Name: "Server Object", Extensible: true,
		Fields: []Field{
			field("url", String, c(V30, "**REQUIRED**. A URL to the target host."),
				c(V30, "This URL supports Server Variables and MAY be relative")).required(Always()).formatted(ServerURL).
				nonEmpty(From(V32), sectioned("Server Variable Object")(V32, "server-url-template  = 1*( literals / server-variable )")),
			field("description", String, c(V30, "An optional string describing the host designated by the URL.")),
			field("name", String, c(V32, "An optional unique string to refer to the host designated by the URL.")).
				during(From(V32)),
			mapOf("variables", ServerVariable, c(V30, "A map between a variable name and its value.")),
		},
		Rules: []Rule{
			code(From(V32), "server-url-template", "",
				sectioned("Server Variable Object")(V32, "Each server variable MUST NOT appear more than once in the URL template.")),
			code(Always(), "server-url-variables", "",
				c(V30, "Variable substitutions will be made when a variable is named in"),
				c(V30, "The value is used for substitution in the server's URL template.")),
			code(From(V31), "server-url-parts", "", c(V31, "Query and fragment MUST NOT be part of this URL.")),
		},
	}
}

// serverVariableObject is the Server Variable Object.
func serverVariableObject() Object {
	c := sectioned("Server Variable Object")
	return Object{
		Kind: ServerVariable, Name: "Server Variable Object", Extensible: true,
		Fields: []Field{
			field("enum", StringList, c(V30, "An enumeration of string values to be used if the substitution options are from a limited set.")),
			field("default", String, c(V30, "**REQUIRED**. The default value to use for substitution, which SHALL be sent")).
				required(Always()),
			field("description", String, c(V30, "An optional description for the server variable.")),
		},
		Rules: []Rule{
			count(Only(V30), Should, "enum", 0, c(V30, "The array SHOULD NOT be empty.")),
			count(From(V31), Must, "enum", 0, c(V31, "The array MUST NOT be empty.")),
			member(Only(V30), Should, "default", "enum", c(V30, "the value SHOULD exist in the enum's values.")),
			member(From(V31), Must, "default", "enum", c(V31, "the value MUST exist in the enum's values.")),
		},
	}
}

// tagObject is the Tag Object.
func tagObject() Object {
	c := sectioned("Tag Object")
	return Object{
		Kind: Tag, Name: "Tag Object", Extensible: true,
		Fields: []Field{
			field("name", String, c(V30, "**REQUIRED**. The name of the tag.")).required(Always()),
			field("summary", String, c(V32, "A short summary of the tag, used for display purposes.")).during(From(V32)),
			field("description", String, c(V30, "A description for the tag.")),
			objectOf("externalDocs", ExternalDocs, c(V30, "Additional external documentation for this tag.")),
			field("parent", String, c(V32, "The `name` of a tag that this tag is nested under.")).during(From(V32)),
			field("kind", String, c(V32, "A machine-readable string to categorize what sort of tag it is.")).
				during(From(V32)),
		},
	}
}

// externalDocsObject is the External Documentation Object.
func externalDocsObject() Object {
	c := sectioned("External Documentation Object")
	return Object{
		Kind: ExternalDocs, Name: "External Documentation Object", Extensible: true,
		Fields: []Field{
			field("description", String, c(V30, "A description of the target documentation.")),
			field("url", String, c(V30, "**REQUIRED**. The URL for the target documentation.")).
				required(Always()).formatted(URI),
		},
	}
}
