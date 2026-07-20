package ir

// Service is a client-visible service: a coherent group of operations with a
// shared identity, auth default, and protocol conventions (ir-design §7.1).
type Service struct {
	// ID is the service's stable synthetic identity.
	ID ServiceID `json:"id,omitempty"`
	// Name is the service's naming.
	Name Naming `json:"name"`
	// Docs is the service's documentation.
	Docs Docs `json:"docs"`
	// Version is the per-service version string (Smithy service version); the
	// document-level Version remains the API title's version.
	Version string `json:"version,omitempty"`
	// Namespace is the source namespace path (TypeSpec/Smithy/proto package).
	Namespace []string `json:"namespace,omitempty"`
	// Extends lists client-visible service inheritance (Thrift service extends,
	// WSDL 2.0 interface extension, Cap'n Proto interface inheritance); inherited
	// operations are walked, never copied.
	Extends []ServiceID `json:"extends,omitempty"`
	// Groups holds the hierarchical operation groups; a group is a TypeSpec
	// interface / Smithy resource / tag.
	Groups []OperationGroup `json:"groups,omitempty"`
	// Auth is the service-level default requirement (OR-of-ANDs, §9). An empty
	// non-nil slice (explicitly public) differs from nil (no default), so the
	// field carries no omitempty.
	Auth []AuthRequirement `json:"auth"`
	// CommonErrors are errors every operation can return (Smithy service-level
	// errors).
	CommonErrors []ErrorCase `json:"commonErrors,omitempty"`
	// Protocols are the declared serde/protocol conventions the service speaks
	// (Smithy @protocolDefinition traits like aws.protocols#restJson1).
	Protocols []ProtocolDecl `json:"protocols,omitempty"`
	// Renames holds per-service shape presentation names (Smithy service rename);
	// the TypeID — and Naming on the type — are unchanged.
	Renames map[TypeID]Naming `json:"renames,omitempty"`
	// Servers indexes into Document.Servers scoped to this service.
	Servers []int `json:"servers,omitempty"`
	// Extensions carries source metadata without a first-class IR node.
	Extensions Extensions `json:"extensions,omitempty"`
	// Provenance records where the service came from.
	Provenance Provenance `json:"provenance"`
}

// ProtocolDecl is a declared serde/protocol convention a service speaks
// (ir-design §7.1).
type ProtocolDecl struct {
	// Name is the protocol name, e.g. "aws.restJson1" or "grpc".
	Name string `json:"name,omitempty"`
	// Options holds per-protocol options kept raw (the Channel.Bindings pattern).
	Options Extensions `json:"options,omitempty"`
}

// OperationGroup is a hierarchical grouping of operations: a TypeSpec interface,
// Smithy resource, or tag (ir-design §7.1).
type OperationGroup struct {
	// Name is the group's naming.
	Name Naming `json:"name"`
	// Docs is the group's documentation.
	Docs Docs `json:"docs"`
	// Groups holds nested groups: Smithy resources, sub-clients.
	Groups []OperationGroup `json:"groups,omitempty"`
	// Operations holds the group's operations.
	Operations []Operation `json:"operations,omitempty"`
	// Resource carries Smithy resource semantics when declared.
	Resource *ResourceInfo `json:"resource,omitempty"`
	// Availability records the group's versioning timeline (TypeSpec interfaces
	// are versionable).
	Availability *Availability `json:"availability,omitempty"`
	// Extensions carries source metadata without a first-class IR node.
	Extensions Extensions `json:"extensions,omitempty"`
}

// ResourceInfo carries Smithy resource semantics for an OperationGroup
// (ir-design §7.1).
type ResourceInfo struct {
	// Identifiers are the resource identity fields.
	Identifiers []Property `json:"identifiers,omitempty"`
	// Properties are the resource state fields (Smithy 2.0 resource properties).
	Properties []Property `json:"properties,omitempty"`
	// Lifecycle maps lifecycle names ("create"|"put"|"read"|"update"|"delete"|
	// "list") to operations; put = create-or-replace with a client-provided
	// identifier.
	Lifecycle map[string]OpID `json:"lifecycle,omitempty"`
	// NoReplace reports that put may create but not replace (Smithy @noReplace).
	NoReplace bool `json:"noReplace"`
	// InstanceOps are declared non-lifecycle instance operations (require
	// identifiers).
	InstanceOps []OpID `json:"instanceOps,omitempty"`
	// CollectionOps are declared collection operations; the split drives
	// sub-client shape and is a declared fact, not a heuristic.
	CollectionOps []OpID `json:"collectionOps,omitempty"`
}
