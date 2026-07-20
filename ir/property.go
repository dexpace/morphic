package ir

// PresenceKind is the wire-presence discipline of a property where the format
// distinguishes more than required/optional (protobuf) (ir-design §5.1).
type PresenceKind string

// Presence disciplines.
const (
	// PresenceDefault is the format default (Required/Nullable say everything in
	// the JSON world).
	PresenceDefault PresenceKind = ""
	// PresenceImplicit means absence equals the default value; unset is
	// unobservable and zero values are not serialized (proto3 no-label, editions
	// IMPLICIT).
	PresenceImplicit PresenceKind = "implicit"
	// PresenceExplicit means unset is distinguishable from default-valued
	// (hazzers/pointers; proto2 optional, proto3 optional, editions EXPLICIT).
	PresenceExplicit PresenceKind = "explicit"
	// PresenceRequired means the property must be present on the wire (proto2
	// required, editions LEGACY_REQUIRED).
	PresenceRequired PresenceKind = "required"
)

// Lifecycle names a visibility lifecycle class. It is an OPEN set; canonical
// values are "create", "read", "update", "delete", and "query". TypeSpec custom
// visibility classes lower as "<class>:<member>" strings so nothing is dropped
// (ir-design §5.2).
type Lifecycle = string

// Canonical lifecycle values.
const (
	// LifecycleCreate is the create lifecycle.
	LifecycleCreate Lifecycle = "create"
	// LifecycleRead is the read lifecycle.
	LifecycleRead Lifecycle = "read"
	// LifecycleUpdate is the update lifecycle.
	LifecycleUpdate Lifecycle = "update"
	// LifecycleDelete is the delete lifecycle.
	LifecycleDelete Lifecycle = "delete"
	// LifecycleQuery is the query lifecycle.
	LifecycleQuery Lifecycle = "query"
)

// Visibility is the set of lifecycles in which a property is visible
// (ir-design §5.2). The zero value is visible in all lifecycles.
type Visibility struct {
	// Only lists the lifecycles the property is visible in; empty = visible in
	// all (unless None).
	Only []Lifecycle `json:"only,omitempty"`
	// None marks a property visible in NO lifecycle, excluded from every
	// projection (TypeSpec @invisible); distinct from the zero value.
	None bool `json:"none"`
}

// Property is one member of a Model (ir-design §5.1). Required (wire presence) is
// orthogonal to Type.Nullable (this usage admits null).
type Property struct {
	// ID is the property's stable synthetic identity.
	ID PropID `json:"id"`
	// Name is the property's naming.
	Name Naming `json:"name"`
	// WireName is the serialized name; defaults to Name.Source.
	WireName string `json:"wireName,omitempty"`
	// WireNameByFormat carries per-media-type overrides (TypeSpec @encodedName
	// json/xml).
	WireNameByFormat map[string]string `json:"wireNameByFormat,omitempty"`
	// WireID is the protobuf field number / thrift id / tuple element index
	// (1-based when Model.Positional); nil = none (pointer because 0 is a legal
	// ordinal).
	WireID *int `json:"wireID,omitempty"`
	// ExtensionOf is "" for the model's own field, else the fully-qualified
	// declaring scope of a third-party extension field (protobuf extend).
	ExtensionOf string `json:"extensionOf,omitempty"`
	// Type is the property's type.
	Type TypeRef `json:"type"`
	// Required reports wire presence; orthogonal to Type.Nullable.
	Required bool `json:"required"`
	// Presence is the wire-presence discipline where the format distinguishes more
	// than required/optional (protobuf).
	Presence PresenceKind `json:"presence,omitempty"`
	// ClientOptional marks a wire-required property that clients MUST treat as
	// optional (Smithy @clientOptional).
	ClientOptional bool `json:"clientOptional"`
	// DefaultAdded marks a default added post-publication; generators may ignore
	// it for backward compatibility (Smithy @addedDefault).
	DefaultAdded bool `json:"defaultAdded"`
	// Visibility is the lifecycle set; zero value = visible in all.
	Visibility Visibility `json:"visibility"`
	// Default is the property's default value.
	Default *Value `json:"default,omitempty"`
	// Constraints restricts the property's admissible values.
	Constraints *Constraints `json:"constraints,omitempty"`
	// Encoding overrides the property's wire encoding.
	Encoding *Encoding `json:"encoding,omitempty"`
	// Args are field arguments for parameterized fields: GraphQL field arguments
	// on any property at any depth; empty elsewhere.
	Args []Parameter `json:"args,omitempty"`
	// Flatten hoists the property's fields into the parent on the wire (Smithy/TCGC
	// flatten; also set on the synthetic property wrapping a hoisted protobuf
	// oneof).
	Flatten bool `json:"flatten"`
	// EventHeader marks an event-stream member that travels in the frame header,
	// not the payload (Smithy @eventHeader).
	EventHeader bool `json:"eventHeader"`
	// EventPayload marks the raw frame payload member (Smithy @eventPayload);
	// mutually exclusive with EventHeader, at most one per model.
	EventPayload bool `json:"eventPayload"`
	// Secret requests redaction in logs/docs (TypeSpec @secret, format:password).
	Secret bool `json:"secret"`
	// XML is the XML wire shape when it diverges from the JSON-implied shape.
	XML *XMLHints `json:"xml,omitempty"`
	// Examples are property-level example values.
	Examples []Example `json:"examples,omitempty"`
	// Docs is the property's documentation.
	Docs Docs `json:"docs"`
	// Deprecation marks the property as deprecated.
	Deprecation *Deprecation `json:"deprecation,omitempty"`
	// Availability records the property's versioning timeline.
	Availability *Availability `json:"availability,omitempty"`
	// Extensions carries source metadata without a first-class IR node.
	Extensions Extensions `json:"extensions,omitempty"`
	// Provenance records where the property came from.
	Provenance Provenance `json:"provenance"`
}
