package protobuf

import "github.com/dexpace/morphic/ir"

// IDs are derived from a descriptor's fully-qualified proto name — the stable
// structural identity protobuf assigns every declaration, independent of any
// display/renaming (ir-design §3.1). No other code constructs IDs.

// namedTypeID returns the stable ID of a message or enum by its fully-qualified
// name, e.g. "t/protobuf/example.v1.User".
func namedTypeID(fullName string) ir.TypeID { return ir.TypeID("t/protobuf/" + fullName) }

// anonTypeID returns the stable ID of a hoisted anonymous type (a container or a
// oneof union) at scope, e.g. "t/anon/protobuf/example.v1.User.tags/list".
func anonTypeID(scope string) ir.TypeID { return ir.TypeID("t/anon/protobuf/" + scope) }

// primTypeID returns the interned ID of primitive kind k.
func primTypeID(k ir.PrimKind) ir.TypeID { return ir.TypeID("t/prim/" + string(k)) }

// anyTypeID is the shared ID of the schemaless Any node the well-known dynamic
// types (Any, Struct, Value, …) resolve to.
const anyTypeID ir.TypeID = "t/protobuf/any"

// propID returns the stable ID of a field by its fully-qualified name,
// e.g. "p/protobuf/example.v1.User.id".
func propID(fullName string) ir.PropID { return ir.PropID("p/protobuf/" + fullName) }

// opID returns the stable ID of an rpc method by its fully-qualified name.
func opID(fullName string) ir.OpID { return ir.OpID("op/protobuf/" + fullName) }

// serviceID returns the stable ID of the document service for scope (the proto
// package, or the source path when the file declares no package).
func serviceID(scope string) ir.ServiceID { return ir.ServiceID("s/protobuf/" + scope) }
