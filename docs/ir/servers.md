# Servers

Part of the [IR design](./README.md). The server registry, and the scopes that name its entries.

```go
type ServerID string                    // server/<space>/<pointer of its Server Object>, or a
                                        // minted one: server/default

type Server struct {
    ID          ServerID
    Name        Naming                  // servers are named entities (AsyncAPI name-keyed servers,
                                        // OpenAPI 3.2 Server.name)
    URLTemplate string                  // may contain {variables}
    Description Docs
    Variables   []ServerVariable        // {Name, Default, Enum []string, Docs, Unmodeled}
    Protocol    string                  // "https" default; "kafka", "wss", … for messaging servers
    ProtocolVersion string              // e.g. Kafka "3.5", AMQP "0-9-1" (AsyncAPI)
    Tags        []string
    Auth        []AuthRequirement       // server-scoped security — AsyncAPI's primary auth placement
                                        // (broker connections authenticate per server; different
                                        // servers of one service may require different schemes)
    Bindings    map[string]RawConfig    // server-level protocol bindings kept raw
    Unmodeled   Unmodeled
    Provenance  Provenance              // where it is declared; no pointer for one the format
                                        // supplies where the document declares none
}
```

**A registry, and scopes that name it.** `Document.Servers` holds the servers the source declares,
keyed by ID, whichever object declares them: a document's own list, an OpenAPI path item's or
operation's, an AsyncAPI document's name-keyed map. Where a compiler keeps an object whole rather
than lowering it, as the OpenAPI compiler keeps a component no reference reaches, its servers stay
inside it. The scopes a server serves name it: `Service.Servers`, `Channel.Servers` and
`Operation.Servers` are each a list of IDs, most preferred first, since a consumer that needs one
server, an SDK client's default among them, takes the first it can use ([the
client](../emitters/operations/client.md)). An operation's non-empty list replaces its service's for
that operation, as its `Auth` replaces its service's; an empty one leaves its service's. An empty
service or channel list names no server. It does not mean every server, because the registry holds
servers that serve one operation alone; a format that reads a scope naming none as served by all of
them, as AsyncAPI reads a channel, lists them all. A server no scope names is still a declaration
and stays: an OpenAPI path item's list that every one of its operations overrides is registered and
named by none.

**Identity.** A server is identified by where it is declared, the pointer of its Server Object in
the document declaring it (`server/openapi/paths/~1a/servers/0`), so a path item two paths mount
declares one server that both mounts name, and the ID's path is the pointer its provenance records,
which `irverify` holds. A server the format supplies where the document declares none is minted
([identity](./identity.md)): OpenAPI's `/`, for a `servers` field absent or empty, is
`server/default`, in a space no pointer reaches, and records the whole document as its position. Its
missing pointer is what tells it from a declared `/`.

**Why IDs and a map rather than indices.** Until 0.22.0 the scopes indexed an ordered
`Document.Servers`, and an empty list meant every server, which held while the only servers were the
document's own. Servers an operation declares break both halves: an index stops naming its server
once a pass removes or reorders one (invariant 3 exists for this), and "every server" would send a
service to servers only one of its operations uses. Servers are named entities, and every other
reference between named entities is by ID.

**What an operation's servers are not.** `HTTPBinding.HostPrefix` stays where it is: Smithy's
`@endpoint` prefixes whichever server is chosen with a host label, so it qualifies a server rather
than naming one. And `Operation.Servers` sits on the neutral core beside `Auth`, which an operation
overrides the same way, rather than on a binding: it says where the operation is sent, whichever
binding sends it. An operation bound to a channel leaves it empty, since its channel names its
servers.
