package ir

import "encoding/json"

// RawValue is source JSON preserved verbatim.
type RawValue = json.RawMessage

// Extensions is the lossless escape hatch: source metadata without a
// first-class IR node survives here, keys namespaced by origin so two formats'
// extensions never collide: "openapi:x-rate-limit", "smithy:aws.api#arn",
// "graphql:@key", "erlang:opaque" (ir-design §12).
type Extensions map[string]RawValue
