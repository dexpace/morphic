# The document root

Part of the [IR design](./README.md). `Document` is what a compiler returns and an emitter reads:
the registries every node lives in, and what is said of the API as a whole.

```go
type Document struct {
    IRVersion   string                 // version of the IR schema itself, semver
    IDSpaces    map[string][]string    // the namespaces the document's IDs live in, by kind prefix (docs/ir/identity.md);
                                       // a producer states it as it stamps IRVersion
    Name        string                 // API title, as declared; "" when the source declares none
    Version     string                 // API version string (source-declared)
    Docs        Docs
    Contact     *Contact               // {Name, URL, Email} (OpenAPI/AsyncAPI info.contact)
    License     *License               // {Name, Identifier, URL} (OpenAPI/AsyncAPI info.license)
    TermsOfService string
    Services    []Service              // ≥1 (irverify); multi-service documents are normal (TypeSpec,
                                       // stitching)
    Types       map[TypeID]TypeDef     // the type registry — the only owner of TypeDefs
    Channels    map[ChannelID]Channel  // event/messaging layer (AsyncAPI, webhooks, subscriptions, OTP processes)
    Messages    map[MessageID]Message  // message registry — messages are reused across channels and
                                       // referenced by identity from operations and replies (AsyncAPI 3)
    Auth        map[AuthID]AuthScheme  // auth scheme registry
    Servers     map[ServerID]Server    // server registry: the servers the source declares,
                                       // whichever object declares them; scopes name them by ID
                                       // (docs/ir/servers.md)
    TagDefs     []TagDef               // tag metadata registry: {Name, Docs, Parent, Kind}; tag
                                       // *membership* stays []string on the tagged nodes
                                       // (OpenAPI/AsyncAPI tag objects)
    Versions    []string               // ordered version labels when availability metadata is used
    Unmodeled   Unmodeled
    Diagnostics []Diagnostic           // accumulated by compiler + passes; not part of API meaning
    Sources     []SourceInfo           // ≥1 (irverify), one entry per input file
}

type Contact struct { Name, URL, Email string }
type License struct { Name, Identifier, URL string }
type TagDef  struct {
    Name   string
    Docs   Docs
    Parent string                 // nested-tag parent (OpenAPI 3.2 tag parent), as declared;
                                  // "" = top-level
    Kind   string                 // tag role recorded verbatim (OpenAPI 3.2 tag kind);
                                  // interpretation is grouping policy (docs/ir/services.md), never read here
}
type SourceInfo struct {
    Format  string   // format and version the file was read as, name@version: "openapi@3.2",
                     // "overlay@1.0.0"
    Path    string   // the path the file was handed to the compiler under
    Hash    string   // lowercase hex SHA-256 of the file's bytes (ir.SourceHash)
    SelfURI string   // the URI the document declared for itself (OpenAPI `$self`), as written;
                     // "" = none declared
}
```

A `Document` is self-contained: no node references anything outside it.
