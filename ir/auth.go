package ir

// AuthKind names one authentication mechanism (ir-design §9). X509 is distinct
// from mutual_tls (certificate as credential vs mutual verification); frontends
// must not conflate them.
type AuthKind string

// Authentication mechanisms.
const (
	// AuthKindAPIKey is an API key in a header, query, cookie, or transport slot.
	AuthKindAPIKey AuthKind = "apiKey"
	// AuthKindHTTPBasic is HTTP Basic authentication.
	AuthKindHTTPBasic AuthKind = "http_basic"
	// AuthKindHTTPBearer is HTTP Bearer-token authentication.
	AuthKindHTTPBearer AuthKind = "http_bearer"
	// AuthKindOAuth2 is OAuth 2.0 with one or more flows.
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
)

// AuthScheme is a named authentication scheme in Document.Auth (ir-design §9).
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
	Deprecation *Deprecation `json:"deprecation,omitempty"`
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
	// Extensions carries source metadata without a first-class IR node.
	Extensions Extensions `json:"extensions,omitempty"`
	// Provenance records where the scheme came from.
	Provenance Provenance `json:"provenance"`
}

// OAuthFlow is one OAuth 2.0 flow of an AuthScheme (ir-design §9).
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
	// Scopes maps scope names to their descriptions.
	Scopes map[string]string `json:"scopes,omitempty"`
	// Extensions carries source metadata without a first-class IR node.
	Extensions Extensions `json:"extensions,omitempty"`
}

// AuthRequirement is one authentication option: all its SchemeUses must be
// satisfied together (ir-design §9). A slice of AuthRequirement is an OR across
// options in priority order; an empty option means "no auth is one acceptable
// choice".
type AuthRequirement struct {
	// Schemes must all be satisfied together to fulfill this option.
	Schemes []SchemeUse `json:"schemes,omitempty"`
}

// SchemeUse names one scheme and the scopes required of it within an
// AuthRequirement (ir-design §9).
type SchemeUse struct {
	// Scheme is the referenced auth scheme.
	Scheme AuthID `json:"scheme,omitempty"`
	// Scopes are the scopes required of the scheme (OAuth2/OpenID).
	Scopes []string `json:"scopes,omitempty"`
}
