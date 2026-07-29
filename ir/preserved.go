package ir

import "encoding/json"

// RawValue is source JSON preserved verbatim.
type RawValue = json.RawMessage

// PreserveReason says why a construct was kept verbatim instead of modeled. It
// is a property of the construct itself, never of the diagnostic a compiler
// emitted beside it: one diagnostic code routinely spans several of these.
type PreserveReason string

// Why a construct is preserved rather than modeled.
const (
	// ReasonVendorExtension marks an annotation the source format itself assigns
	// no semantics to (OpenAPI x-*, and every format's equivalent). The key set
	// is unbounded and nothing can be inferred from the value.
	ReasonVendorExtension PreserveReason = "vendor_extension"
	// ReasonValidationOnly marks validation logic rather than data shape: the
	// IR's structural picture is complete without it, and neither a target type
	// system nor a sibling source format has an equivalent (ir-design §4.7).
	ReasonValidationOnly PreserveReason = "validation_only"
	// ReasonDegradedLowering marks a construct with no faithful target as
	// written — the IR has no combinator for the shape, or no target type system
	// holds it. It was lowered to a documented weaker shape with the original
	// kept beside it, so the degradation stays recoverable (ir-design §4.8).
	ReasonDegradedLowering PreserveReason = "degraded_lowering"
	// ReasonNoIRHome marks a construct target languages can express and the IR
	// draws no deliberate boundary against: the position simply has no field for
	// it yet. A gap expected to close, not a boundary.
	ReasonNoIRHome PreserveReason = "no_ir_home"
	// ReasonOutOfScope marks a construct the IR excludes on purpose — runtime
	// machinery or per-target SDK policy rather than API shape (ir-design §15:
	// Smithy waiters and @endpointRuleSet, TCGC client-shaping decorators). It
	// is kept so nothing is lost, but no IR node is coming; the consumer meant
	// to read it is an emitter policy layer, not a promotion pass.
	ReasonOutOfScope PreserveReason = "out_of_scope"
)

// Valid reports whether r is one of the reasons declared above. PreserveReason
// is a bare string enum, so nothing rejects a typo'd or stale value on the
// wire; irverify calls this so an entry no consumer's switch can route is
// reported as the compiler bug it is rather than silently dropped.
func (r PreserveReason) Valid() bool {
	switch r {
	case ReasonVendorExtension, ReasonValidationOnly, ReasonDegradedLowering,
		ReasonNoIRHome, ReasonOutOfScope:
		return true
	default:
		return false
	}
}

// PreservedEntry is one preserved construct.
type PreservedEntry struct {
	// Reason says why the construct is here rather than modeled, so a consumer
	// can take the subset it cares about — a validation emitter wants §4.7
	// entries and not vendor noise; a linter wants the degradations.
	Reason PreserveReason `json:"reason"`
	// Value is the source construct, preserved whole rather than byte-for-byte:
	// nothing here discards or reshapes it, but two re-encodings sit between the
	// source bytes and this field. json.Marshal compacts a RawValue and escapes
	// <, >, & as \uXXXX, and a compiler that rebuilds the value from its parsed
	// tree (the OpenAPI path does, via a decode/re-marshal) also normalizes
	// object key order and number spelling and rewrites ill-formed UTF-8 to
	// U+FFFD. What survives is the construct's meaning, not its spelling.
	Value RawValue `json:"value"`
	// Provenance locates the construct itself, which the owning node's own
	// provenance cannot: a validation emitter reporting on a `not` must point at
	// the keyword, not at the schema that carried it. It falls back to the
	// declaring position only for an entry synthesized from several keywords, no
	// one of which addresses the whole value.
	Provenance Provenance `json:"provenance"`
}

// Preserved is the lossless escape hatch: a source construct the IR
// deliberately does not model survives here untouched, keys namespaced by
// origin so two formats never collide: "openapi:x-rate-limit",
// "smithy:aws.api#arn", "graphql:@key", "erlang:opaque" (ir-design §12).
//
// The name states the guarantee rather than any one format's word for the
// concept — OpenAPI calls these extensions, Protobuf options, GraphQL
// directives, Smithy traits, TypeSpec decorators — and Protobuf's own
// "extensions" means something else entirely (reserved field-number ranges).
type Preserved map[string]PreservedEntry

// RawConfig is declared protocol configuration the IR models a field for but
// deliberately does not constrain the shape of: AsyncAPI protocol bindings,
// Smithy protocol options. It is not an escape hatch — the entries are there
// because the source declared them where the IR expects them, so unlike
// Preserved they carry no reason for being unmodeled.
type RawConfig map[string]RawValue
