# Auth

Part of the [IR design](./README.md). Authentication schemes, and the requirements services,
operations and servers state over them.

```go
type AuthScheme struct {
    ID   AuthID
    Name Naming
    Kind AuthKind
    // AuthKind: apiKey | http_basic | http_bearer | oauth2 | openid_connect | mutual_tls |
    //           user_password | x509 | symmetric_encryption | asymmetric_encryption |
    //           sasl_plain | sasl_scram_sha256 | sasl_scram_sha512 | sasl_gssapi | custom
    // The SASL family, user_password, and x509 are how Kafka/AMQP clients authenticate
    // (AsyncAPI securitySchemes). AsyncAPI `httpApiKey` lowers to apiKey; AsyncAPI `apiKey`
    // (transport user/password slot) lowers to apiKey with In: user|password. X509 is distinct
    // from mutual_tls (certificate as credential vs mutual verification) — compilers must not
    // conflate them.
    Docs Docs
    Deprecation *Deprecation
    // apiKey
    In       string      // header | query | cookie | user | password
    KeyName  string
    // http
    Scheme       string  // bearer, basic, digest…; also legal for apiKey (Smithy @httpApiKeyAuth scheme)
    BearerFormat string
    // oauth2 (Flows may be empty: OAuth2MetadataURL can name the authorization server
    // instead, and a source can state neither)
    Flows []OAuthFlow
    OAuth2MetadataURL string // RFC 8414 authorization-server metadata (OpenAPI 3.2)
    // openid
    OpenIDConnectURL string
    Credentials *TypeRef // the credential a client supplies, where the source declares its shape
    Unmodeled  Unmodeled
    Provenance Provenance
}

type OAuthFlow struct {
    Kind                     string       // authorization_code | client_credentials | implicit | password | device
    AuthorizationURL         string       // the device flow's deviceAuthorizationUrl rides it
    TokenURL                 string
    RefreshURL               string
    Scopes                   []OAuthScope // source order, each name once
    TokenEndpointAuthMethods []string     // IANA "OAuth Token Endpoint Authentication Methods" values
    Unmodeled                Unmodeled
    Inferred                 Inferred     // the heuristics that read a fact out of Unmodeled
                                          // ([promotion](./unmodeled.md#promotion))
}

type OAuthScope struct { Name, Description string }

type AuthRequirement struct {          // one *option*
    Schemes []SchemeUse                // ALL must be satisfied together
}
type SchemeUse struct { Scheme AuthID; Scopes []string }
// []AuthRequirement on a service/operation/server = OR across options (TypeSpec/OpenAPI security
// semantics). Option order is PRIORITY order — clients pick the first supported option (Smithy
// @auth is priority-ordered; the Smithy compiler materializes the alphabetical default when
// @auth is absent). Compilers whose source has no ordering semantics emit source order.
// An empty option (AuthRequirement{Schemes: []}) inside a non-empty list means "no auth is one
// acceptable choice" (TypeSpec NoAuth in a union, Smithy @optionalAuth, OpenAPI's empty security
// requirement) — distinct from Operation.Auth = [] (explicitly public).
```

**Scopes keep their order.** A flow's scopes are a list of name and description, in the order the
source writes them, as a `Value.Object`'s members are ([values](./values.md)): documentation and
consent screens show them as the author did, and a map would sort them. A scope with no description
has an empty one; TypeSpec writes scope names alone. A name appears once per flow, as the map made
it: two entries would give one scope two descriptions, so a compiler folds a source that repeats
one, and `irverify` reports a repeat (`ir/oauth-scope-duplicate`).

**How a client authenticates to the token endpoint** is `TokenEndpointAuthMethods`, spelled as the
IANA registry RFC 8414's `token_endpoint_auth_methods_supported` draws on spells it
(`client_secret_basic`, `client_secret_post`, `private_key_jwt`, …). It is a set: the methods the
endpoint accepts, none blank and none repeated. The registry is open, so a value outside it is kept
as written, and an empty list means the source does not say, leaving the choice to the SDK's
[runtime policy](../emitters/contract/runtime-policy.md). No format the matrix covers states it as a
field; an OpenAPI document that does so writes a vendor extension, which stays under `Unmodeled`
until a promotion target reads it.

**A declared credential shape** is `Credentials`, a reference to a registered type, for a scheme —
typically `custom` — whose credential is several fields combined into a request rather than the
one value its kind implies. Nil means the source declares no shape. No compiler fills it yet:
building that type out of an extension would mint a node ([identity](./identity.md#the-id-grammar)),
which is a design of its own.
