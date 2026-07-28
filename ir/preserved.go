package ir

import "encoding/json"

// RawValue is source JSON preserved verbatim.
type RawValue = json.RawMessage

// Preserved is the lossless escape hatch: a source construct the IR
// deliberately does not model survives here untouched, keys namespaced by
// origin so two formats never collide: "openapi:x-rate-limit",
// "smithy:aws.api#arn", "graphql:@key", "erlang:opaque" (ir-design §12).
//
// The name states the guarantee rather than any one format's word for the
// concept — OpenAPI calls these extensions, Protobuf options, GraphQL
// directives, Smithy traits, TypeSpec decorators — and Protobuf's own
// "extensions" means something else entirely (reserved field-number ranges).
type Preserved map[string]RawValue

// RawConfig is declared protocol configuration the IR models a field for but
// deliberately does not constrain the shape of: AsyncAPI protocol bindings,
// Smithy protocol options. It is not an escape hatch — the entries are there
// because the source declared them where the IR expects them, so unlike
// Preserved they carry no reason for being unmodeled.
type RawConfig map[string]RawValue
