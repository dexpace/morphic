# Services and operation groups

Part of the [IR design](./README.md). The units an API's operations are organized into.

```go
type Service struct {
    ID         ServiceID
    Name       Naming
    Docs       Docs
    Version    string             // per-service version string (Smithy service version); the
                                  // document-level Version remains the API title's version
    Namespace  []string           // source namespace path (TypeSpec/Smithy/proto package)
    Extends    []ServiceID        // client-visible service inheritance (Thrift service extends,
                                  // WSDL 2.0 interface extension, Cap'n Proto interface inheritance);
                                  // inherited operations are walked, never copied
    Groups     []OperationGroup   // hierarchical; a group ≈ TypeSpec interface / Smithy resource / tag
    Auth       []AuthRequirement  // service-level default (OR-of-ANDs, docs/ir/auth.md)
    CommonErrors []ErrorCase      // errors every operation can return (Smithy service-level errors)
    Protocols  []ProtocolDecl     // declared serde/protocol conventions the service speaks
                                  // (Smithy @protocolDefinition traits like aws.protocols#restJson1)
    Renames    map[TypeID]Naming  // per-service shape presentation names (Smithy service `rename`);
                                  // the TypeID — and Naming on the type — are unchanged
    Servers    []ServerID         // the servers serving the service, most preferred first; empty =
                                  // no declared server does (docs/ir/servers.md)
    Unmodeled  Unmodeled
    Provenance Provenance
}

type ProtocolDecl struct {
    Name    string                // e.g. "aws.restJson1", "grpc"
    Options RawConfig             // per-protocol options kept raw (the Channel.Bindings pattern)
}

type OperationGroup struct {
    ID         GroupID            // minted by the rule that formed the group (docs/ir/identity.md); document-wide unique
    Name       Naming
    Docs       Docs
    Groups     []OperationGroup   // nesting: Smithy resources, sub-clients
    Operations []Operation
    Resource   *ResourceInfo      // Smithy resource semantics when declared
    Availability *Availability    // groups (TypeSpec interfaces) are versionable
    Unmodeled  Unmodeled
}

type ResourceInfo struct {
    Identifiers []Property                  // resource identity fields
    Properties  []Property                  // resource state fields (Smithy 2.0 resource properties)
    Lifecycle   map[string]OpID             // "create"|"put"|"read"|"update"|"delete"|"list" → op
                                            // (put = create-or-replace with client-provided identifier)
    NoReplace   bool                        // put may create but not replace (Smithy @noReplace)
    InstanceOps   []OpID                    // declared non-lifecycle instance operations (require identifiers)
    CollectionOps []OpID                    // declared collectionOperations — the split drives
                                            // sub-client shape and is a declared fact, not a heuristic
}
```

OpenAPI compilers build groups from tags (policy-controllable: tag-based vs path-prefix-based);
TypeSpec from interfaces/namespaces; Smithy from resources; GraphQL yields three groups
(query/mutation/subscription); Protobuf one group per `service`; Erlang/OTP one group per module.
How a tag builds a group, and nests it under a declared parent, is the OpenAPI compiler's rule
[`tag-groups`](../compilers/openapi.md#tag-groups).
