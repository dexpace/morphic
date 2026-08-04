package ir

import "encoding/json"

// RawValue is source JSON preserved verbatim.
type RawValue = json.RawMessage

// UnmodeledReason says why a construct was kept verbatim instead of modeled. It
// is a property of the construct itself, never of the diagnostic a compiler
// emitted beside it: one diagnostic code routinely spans several of these.
type UnmodeledReason string

// Why a construct is unmodeled rather than lowered.
const (
	// ReasonVendorExtension marks an annotation the source format itself assigns
	// no semantics to (OpenAPI x-*, and every format's equivalent). The key set
	// is unbounded and nothing can be inferred from the value.
	ReasonVendorExtension UnmodeledReason = "vendor_extension"
	// ReasonValidationOnly marks validation logic rather than data shape: the
	// IR's structural picture is complete without it, and neither a target type
	// system nor a sibling source format has an equivalent (ir-design §4.7).
	ReasonValidationOnly UnmodeledReason = "validation_only"
	// ReasonDegradedLowering marks a construct with no faithful target as
	// written — the IR has no combinator for the shape, or no target type system
	// holds it. It was lowered to a documented weaker shape with the original
	// kept beside it, so the degradation stays recoverable (ir-design §4.8).
	ReasonDegradedLowering UnmodeledReason = "degraded_lowering"
	// ReasonNoIRHome marks a construct target languages can express and the IR
	// draws no deliberate boundary against: the position simply has no field for
	// it yet. A gap expected to close, not a boundary.
	ReasonNoIRHome UnmodeledReason = "no_ir_home"
	// ReasonOutOfScope marks a construct the IR excludes on purpose — runtime
	// machinery or per-target SDK policy rather than API shape (ir-design §15:
	// Smithy waiters and @endpointRuleSet, TCGC client-shaping decorators). It
	// is kept so nothing is lost, but no IR node is coming; the consumer meant
	// to read it is an emitter policy layer, not a promotion pass.
	ReasonOutOfScope UnmodeledReason = "out_of_scope"
)

// Valid reports whether r is one of the reasons declared above. UnmodeledReason
// is a bare string enum, so nothing rejects a typo'd or stale value on the
// wire; irverify calls this so an entry no consumer's switch can route is
// reported as the compiler bug it is rather than silently dropped.
func (r UnmodeledReason) Valid() bool {
	switch r {
	case ReasonVendorExtension, ReasonValidationOnly, ReasonDegradedLowering,
		ReasonNoIRHome, ReasonOutOfScope:
		return true
	default:
		return false
	}
}

// UnmodeledEntry is one unmodeled construct, kept verbatim.
type UnmodeledEntry struct {
	// Reason says why the construct is here rather than modeled, so a consumer
	// can take the subset it cares about — a validation emitter wants §4.7
	// entries and not vendor noise; a linter wants the degradations.
	Reason UnmodeledReason `json:"reason"`
	// Value is the source construct, preserved whole rather than byte-for-byte:
	// nothing here discards or reshapes it, but re-encodings sit between the
	// source bytes and this field. json.Marshal compacts a RawValue and escapes
	// <, >, & as \uXXXX, and a compiler that rebuilds the value from its parsed
	// tree (the OpenAPI path does) also sorts object keys.
	//
	// A number's value survives exactly. Its spelling is canonicalized only
	// where JSON and YAML disagree about how to write one — .5 becomes 0.5,
	// 0o17 becomes 15 — while every significant digit stays (GitHub #32).
	//
	// A scalar the source format types and JSON does not is kept as the text
	// the source wrote, as a JSON string: a YAML timestamp stays `2021-1-1`
	// rather than becoming the RFC 3339 instant it resolves to, and a
	// `!!binary` keeps its base64 spelling rather than the bytes it names
	// (GitHub #242). Reading such a value means resolving the tag the way its
	// source format would; what this field promises is that the text is still
	// there to resolve.
	Value RawValue `json:"value"`
	// Provenance locates the construct itself, which the owning node's own
	// provenance cannot: a validation emitter reporting on a `not` must point at
	// the keyword, not at the schema that carried it. It falls back to the
	// declaring position only for an entry synthesized from several keywords, no
	// one of which addresses the whole value.
	Provenance Provenance `json:"provenance"`
}

// Unmodeled is the lossless escape hatch: a source construct the IR
// deliberately does not model survives here untouched, keys namespaced by
// origin so two formats never collide: "openapi:x-rate-limit",
// "smithy:aws.api#arn", "graphql:@key", "erlang:opaque" (ir-design §12).
//
// The name states the one property every member shares — the IR does not model
// it — rather than any one format's word for the concept. OpenAPI calls these
// extensions, Protobuf options, GraphQL directives, Smithy traits, TypeSpec
// decorators, and Protobuf's own "extensions" means something else entirely
// (reserved field-number ranges), so a spec-agnostic IR can adopt none of those
// names. Being unmodeled is the reason an entry is here; surviving verbatim is
// what the field guarantees about it, and UnmodeledReason records which flavour
// of unmodeled each entry is.
type Unmodeled map[string]UnmodeledEntry

// RawConfig is declared protocol configuration the IR models a field for but
// deliberately does not constrain the shape of: AsyncAPI protocol bindings,
// Smithy protocol options. It is not an escape hatch — the entries are there
// because the source declared them where the IR expects them, so unlike an
// Unmodeled entry they carry no UnmodeledReason.
type RawConfig map[string]RawValue
