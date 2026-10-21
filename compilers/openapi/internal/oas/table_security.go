package oas

// securityObjects returns the objects that describe how a request is authorized.
func securityObjects() []Object {
	return []Object{
		securitySchemeObject(), oauthFlowsObject(), oauthFlowObject(), securityRequirementObject(),
	}
}

// securitySchemeObject is the Security Scheme Object.
func securitySchemeObject() Object {
	return Object{
		Kind: SecurityScheme, Name: "Security Scheme Object", Extensible: true, Referenceable: Always(),
		Fields: securitySchemeFields(), Rules: securitySchemeRules(),
	}
}

// securitySchemeFields are the fields of the Security Scheme Object.
func securitySchemeFields() []Field {
	c := sectioned("Security Scheme Object")
	types := []Value{val("apiKey"), val("http"), valFrom("mutualTLS", V31), val("oauth2"), val("openIdConnect")}
	return []Field{
		field("type", String, c(V30, "**REQUIRED**. The type of the security scheme.")).
			required(Always()).ownedBy(ownerAuth).admits(types...),
		field("description", String, c(V30, "A description for security scheme.")),
		field("name", String, c(V30, "**REQUIRED**. The name of the header, query or cookie parameter to be used.")),
		field("in", String, c(V30, "**REQUIRED**. The location of the API key.")).admits(vals("query", "header", "cookie")...),
		field("scheme", String, c(V30, "**REQUIRED**. The name of the HTTP Authentication scheme to be used in the")),
		field("bearerFormat", String, c(V30, "A hint to the client to identify how the bearer token is formatted.")),
		objectOf("flows", OAuthFlows, c(V30, "**REQUIRED**. An object containing configuration information for the flow types supported.")),
		field("openIdConnectUrl", String, c(V30, "**REQUIRED**. [Well-known URL]")).formatted(URI),
		field("oauth2MetadataUrl", String, c(V32, "URL to the OAuth2 authorization server metadata")).
			during(From(V32)).formatted(URI),
		field("deprecated", Boolean, c(V32, "Declares this security scheme to be deprecated.")).during(From(V32)),
	}
}

// securitySchemeRules are the rules of the Security Scheme Object: the fields
// each type requires.
func securitySchemeRules() []Rule {
	c := sectioned("Security Scheme Object")
	need := func(name, kind, sentence string) Rule {
		return requiredWhen(Always(), name, when(term("type", kind)), c(V30, sentence))
	}
	return []Rule{
		need("name", "apiKey", "**REQUIRED**. The name of the header, query or cookie parameter to be used."),
		need("in", "apiKey", "**REQUIRED**. The location of the API key."),
		need("scheme", "http", "**REQUIRED**. The name of the HTTP Authentication scheme to be used in the").
			owned(ownerAuth),
		need("flows", "oauth2", "**REQUIRED**. An object containing configuration information for the flow types supported."),
		need("openIdConnectUrl", "openIdConnect", "**REQUIRED**. [Well-known URL]"),
	}
}

// oauthFlowsObject is the OAuth Flows Object.
func oauthFlowsObject() Object {
	c := sectioned("OAuth Flows Object")
	return Object{
		Kind: OAuthFlows, Name: "OAuth Flows Object", Extensible: true,
		Fields: []Field{
			objectOf("implicit", OAuthFlow, c(V30, "Configuration for the OAuth Implicit flow")),
			objectOf("password", OAuthFlow, c(V30, "Configuration for the OAuth Resource Owner Password flow")),
			objectOf("clientCredentials", OAuthFlow, c(V30, "Configuration for the OAuth Client Credentials flow.")),
			objectOf("authorizationCode", OAuthFlow, c(V30, "Configuration for the OAuth Authorization Code flow.")),
			objectOf("deviceAuthorization", OAuthFlow, c(V32, "Configuration for the OAuth Device Authorization flow.")).
				during(From(V32)),
		},
	}
}

// oauthFlowObject is the OAuth Flow Object. The URLs a flow requires depend on
// the flow, which is the key its parent holds it under.
func oauthFlowObject() Object {
	c := sectioned("OAuth Flow Object")
	held := func(flows ...string) []Term { return when(term("$key", flows...)) }
	return Object{
		Kind: OAuthFlow, Name: "OAuth Flow Object", Extensible: true,
		Fields: []Field{
			field("authorizationUrl", String, c(V30, "**REQUIRED**. The authorization URL to be used for this flow.")).formatted(URI),
			field("deviceAuthorizationUrl", String, c(V32, "**REQUIRED**. The device authorization URL to be used for this flow.")).
				during(From(V32)).formatted(URI),
			field("tokenUrl", String, c(V30, "**REQUIRED**. The token URL to be used for this flow.")).formatted(URI),
			field("refreshUrl", String, c(V30, "The URL to be used for obtaining refresh tokens.")).formatted(URI),
			field("scopes", StringMap, c(V30, "**REQUIRED**. The available scopes for the OAuth2 security scheme.")).
				required(Always()),
		},
		Rules: []Rule{
			requiredWhen(Always(), "authorizationUrl", held("implicit", "authorizationCode"),
				c(V30, "**REQUIRED**. The authorization URL to be used for this flow.")),
			requiredWhen(Through(V31), "tokenUrl", held("password", "clientCredentials", "authorizationCode"),
				c(V30, "**REQUIRED**. The token URL to be used for this flow.")),
			requiredWhen(From(V32), "tokenUrl",
				held("password", "clientCredentials", "authorizationCode", "deviceAuthorization"),
				c(V32, "**REQUIRED**. The token URL to be used for this flow.")),
			requiredWhen(From(V32), "deviceAuthorizationUrl", held("deviceAuthorization"),
				c(V32, "**REQUIRED**. The device authorization URL to be used for this flow.")),
		},
	}
}

// securityRequirementObject is the Security Requirement Object. It is all
// pattern: each key names a scheme, each value lists scopes.
func securityRequirementObject() Object {
	c := sectioned("Security Requirement Object")
	return Object{
		Kind: SecurityRequirement, Name: "Security Requirement Object",
		Patterned: []Pattern{
			pattern("{name}", StringList, 0, c(V30, "Each name MUST correspond to a security scheme which is declared in the")),
		},
	}
}
