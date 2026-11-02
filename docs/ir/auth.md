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
    //           sasl_plain | sasl_scram_sha256 | sasl_scram_sha512 | sasl_gssapi | custom |
    //           unresolved
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

## Unresolved schemes

A requirement names its schemes, and a name can reach no mechanism: nothing declares it, what
declares it names none, or the compiler cannot read what it names. The requirement still stands,
so the option keeps its place in the list, its other schemes and its scopes, and names a scheme of
kind `unresolved`, one per name. An SDK asks its caller for that credential, and never sends the
service's in its place, or nothing at all unless the list offers the empty option
([auth](../emitters/operations/auth.md#unresolved-schemes)).

The kind says what the compiler holds, which is no mechanism. It does not say the source states
none: an OpenAPI 3.2 requirement can name a scheme by URI, and an entry can write `in` and `name`
under no `type`. So the scheme fills no field saying how to authenticate, and `irverify` reports one
that does (`ir/unresolved-auth-has-mechanism`). What it does hold is what a declaration wrote, if
one did: its `Docs` and `Deprecation`, and the rest verbatim under `Unmodeled`. A compiler reports
an error for each such scheme, so a document holding one reaches an emitter only where its caller
accepted that finding. Its ID is in a namespace of its own
([identity](./identity.md#unresolved-schemes)).

The premise is measured. `cloudflare/api-schemas` at `319dc756`, the golden spec, names three such
schemes from nine operations, and in those nine every option does, so nothing else states what they
need. Of the 1,862 OpenAPI documents the reference corpus lowers to, 57 name one, at 286 options, 50
of them listing scopes, and none ANDs such a name with a declared scheme. In 45 of those documents
the list holds `{}` as well, so dropping the rest would leave it saying no auth is needed.

Rejected, each with the input it fails on:

- **No requirement, with the name kept under `Unmodeled`.** `Auth` absent inherits, so an operation
  naming its own upload token sends the service's account token.
- **`[]`.** That is explicitly public, the opposite of what a demanded credential says (#41).
- **`custom`.** It claims a mechanism the IR does not model, where the compiler holds none (#294).
- **`AuthRequirement.Unresolved`, a name beside empty `Schemes`.** `{nosuch: []}` reads as the empty
  option to a consumer that does not read the new field, and its scopes and any other member of the
  option need a second home.
- **A name on `SchemeUse` with an empty `Scheme`.** A reference stops being an ID, and every emitter
  derives the set of names itself, though refine emits one option per scheme.
- **Refusing the document.** A finding never refuses a document, which lowers as written, and the
  golden spec writes this construct.
- **Declaring the scheme with an overlay, or matching the name to a declared one.** An overlay works
  today, since a name it declares resolves, but it gives a source no default. A match is an
  inference (invariant 6), and only five of the golden spec's nine sites have a near match, so the
  other four need this home regardless.
