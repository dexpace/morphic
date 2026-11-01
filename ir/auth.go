// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package ir

// AuthKind names one authentication mechanism (docs/ir/auth.md). X509 is
// distinct from mutual_tls (certificate as credential vs mutual verification);
// compilers must not conflate them.
type AuthKind string

// Authentication mechanisms.
const (
	// AuthKindAPIKey is an API key in a header, query, cookie, or transport slot.
	AuthKindAPIKey AuthKind = "apiKey"
	// AuthKindHTTPBasic is HTTP Basic authentication.
	AuthKindHTTPBasic AuthKind = "http_basic"
	// AuthKindHTTPBearer is HTTP Bearer-token authentication.
	AuthKindHTTPBearer AuthKind = "http_bearer"
	// AuthKindOAuth2 is OAuth 2.0. Its Flows may be empty: OAuth2MetadataURL can
	// name the authorization server instead, and a source can state neither.
	AuthKindOAuth2 AuthKind = "oauth2"
	// AuthKindOpenIDConnect is OpenID Connect discovery.
	AuthKindOpenIDConnect AuthKind = "openid_connect"
	// AuthKindMutualTLS is mutual TLS verification.
	AuthKindMutualTLS AuthKind = "mutual_tls"
	// AuthKindUserPassword is a transport user/password credential.
	AuthKindUserPassword AuthKind = "user_password"
	// AuthKindX509 is an X.509 certificate used as a credential.
	AuthKindX509 AuthKind = "x509"
	// AuthKindSymmetricEncryption is symmetric-key encryption.
	AuthKindSymmetricEncryption AuthKind = "symmetric_encryption"
	// AuthKindAsymmetricEncryption is asymmetric-key encryption.
	AuthKindAsymmetricEncryption AuthKind = "asymmetric_encryption"
	// AuthKindSASLPlain is SASL PLAIN.
	AuthKindSASLPlain AuthKind = "sasl_plain"
	// AuthKindSASLSCRAMSHA256 is SASL SCRAM-SHA-256.
	AuthKindSASLSCRAMSHA256 AuthKind = "sasl_scram_sha256"
	// AuthKindSASLSCRAMSHA512 is SASL SCRAM-SHA-512.
	AuthKindSASLSCRAMSHA512 AuthKind = "sasl_scram_sha512"
	// AuthKindSASLGSSAPI is SASL GSSAPI (Kerberos).
	AuthKindSASLGSSAPI AuthKind = "sasl_gssapi"
	// AuthKindCustom is a custom/unmodeled scheme.
	AuthKindCustom AuthKind = "custom"
	// AuthKindUnresolved is a scheme the source names and the compiler resolved
	// no mechanism for. It records what the compiler holds, not that the source
	// states none (docs/ir/auth.md).
	AuthKindUnresolved AuthKind = "unresolved"
)

// Valid reports whether k is one of the mechanisms declared above. AuthKind is a
// bare string enum, so nothing rejects an empty, misspelled or stale value on
// the wire, and a scheme naming no mechanism is indistinguishable from one
// naming oauth2 to every structural check that reads only its key and its ID.
// irverify calls this so such a scheme is reported as the compiler bug it is.
func (k AuthKind) Valid() bool {
	switch k {
	case AuthKindAPIKey, AuthKindHTTPBasic, AuthKindHTTPBearer, AuthKindOAuth2,
		AuthKindOpenIDConnect, AuthKindMutualTLS, AuthKindUserPassword, AuthKindX509,
		AuthKindSymmetricEncryption, AuthKindAsymmetricEncryption,
		AuthKindSASLPlain, AuthKindSASLSCRAMSHA256, AuthKindSASLSCRAMSHA512,
		AuthKindSASLGSSAPI, AuthKindCustom, AuthKindUnresolved:
		return true
	default:
		return false
	}
}

// AuthScheme is a named authentication scheme in Document.Auth
// (docs/ir/auth.md).
type AuthScheme struct {
	// ID is the scheme's stable synthetic identity.
	ID AuthID `json:"id,omitempty"`
	// Name is the scheme's naming.
	Name Naming `json:"name"`
	// Kind is the authentication mechanism.
	Kind AuthKind `json:"kind,omitempty"`
	// Docs is the scheme's documentation.
	Docs Docs `json:"docs"`
	// Deprecation marks the scheme as deprecated.
	Deprecation *Deprecation `json:"deprecation,omitzero"`
	// In is the apiKey location: header | query | cookie | user | password.
	In string `json:"in,omitempty"`
	// KeyName is the apiKey name.
	KeyName string `json:"keyName,omitempty"`
	// Scheme is the HTTP scheme (bearer, basic, digest…); also legal for apiKey
	// (Smithy @httpApiKeyAuth scheme).
	Scheme string `json:"scheme,omitempty"`
	// BearerFormat is the bearer-token format hint.
	BearerFormat string `json:"bearerFormat,omitempty"`
	// Flows are the OAuth2 flows; device flow's deviceAuthorizationUrl rides
	// OAuthFlow.AuthorizationURL.
	Flows []OAuthFlow `json:"flows,omitempty"`
	// OAuth2MetadataURL is the RFC 8414 authorization-server metadata URL
	// (OpenAPI 3.2).
	OAuth2MetadataURL string `json:"oauth2MetadataURL,omitempty"`
	// OpenIDConnectURL is the OpenID Connect discovery URL.
	OpenIDConnectURL string `json:"openIDConnectURL,omitempty"`
	// Credentials is the type of the credential a client supplies to the
	// scheme, when the source declares one: the fields a custom scheme combines
	// into a request. Nil when it declares none, as for every kind whose
	// credential the kind itself implies.
	Credentials *TypeRef `json:"credentials,omitzero"`
	// Unmodeled holds source constructs the IR does not model, kept verbatim.
	Unmodeled Unmodeled `json:"unmodeled,omitempty"`
	// Provenance records where the scheme came from.
	Provenance Provenance `json:"provenance"`
}

// OAuthFlow is one OAuth 2.0 flow of an AuthScheme (docs/ir/auth.md).
type OAuthFlow struct {
	// Kind is authorization_code | client_credentials | implicit | password |
	// device.
	Kind string `json:"kind,omitempty"`
	// AuthorizationURL is the authorization endpoint; also carries device flow's
	// deviceAuthorizationUrl.
	AuthorizationURL string `json:"authorizationURL,omitempty"`
	// TokenURL is the token endpoint.
	TokenURL string `json:"tokenURL,omitempty"`
	// RefreshURL is the token-refresh endpoint.
	RefreshURL string `json:"refreshURL,omitempty"`
	// Scopes are the flow's scopes in source order, which documentation and
	// consent screens show as the author wrote them, each name once.
	Scopes []OAuthScope `json:"scopes,omitempty"`
	// TokenEndpointAuthMethods are the ways a client may authenticate to the
	// token endpoint, spelled as the IANA "OAuth Token Endpoint Authentication
	// Methods" registry spells them (client_secret_basic, client_secret_post,
	// private_key_jwt, …; RFC 8414 token_endpoint_auth_methods_supported). The
	// registry is open, so a value outside it is kept as written. It is a set,
	// none blank and none repeated; empty means the source does not say.
	TokenEndpointAuthMethods []string `json:"tokenEndpointAuthMethods,omitempty"`
	// Unmodeled holds source constructs the IR does not model, kept verbatim.
	Unmodeled Unmodeled `json:"unmodeled,omitempty"`
}

// OAuthScope is one scope an OAuthFlow offers (docs/ir/auth.md).
type OAuthScope struct {
	// Name is the scope as a token request spells it.
	Name string `json:"name"`
	// Description is what the scope grants, or empty when the source says
	// nothing.
	Description string `json:"description,omitempty"`
}

// AuthRequirement is one authentication option: all its SchemeUses must be
// satisfied together (docs/ir/auth.md). A slice of AuthRequirement is an OR
// across options in priority order; an empty option means "no auth is one
// acceptable choice".
type AuthRequirement struct {
	// Schemes must all be satisfied together to fulfill this option.
	Schemes []SchemeUse `json:"schemes,omitempty"`
}

// SchemeUse names one scheme and the scopes required of it within an
// AuthRequirement (docs/ir/auth.md).
type SchemeUse struct {
	// Scheme is the referenced auth scheme; never empty.
	Scheme AuthID `json:"scheme,omitempty"`
	// Scopes are the scopes required of the scheme (OAuth2/OpenID).
	Scopes []string `json:"scopes,omitempty"`
}
