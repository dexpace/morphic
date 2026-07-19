package ir

// TypeKind names one variant of the sealed TypeDef sum (ir-design §4).
type TypeKind string

// Type kinds: the closed set of type-graph node variants.
const (
	// KindPrimitive is a built-in scalar leaf (Primitive).
	KindPrimitive TypeKind = "primitive"
	// KindScalar is a named restricted/extended primitive (Scalar).
	KindScalar TypeKind = "scalar"
	// KindModel is a struct/object/message shape (Model).
	KindModel TypeKind = "model"
	// KindUnion is a sum over variant types (Union).
	KindUnion TypeKind = "union"
	// KindEnum is a closed or open set of named values (Enum).
	KindEnum TypeKind = "enum"
	// KindList is an ordered collection (List).
	KindList TypeKind = "list"
	// KindMap is a keyed collection (MapT).
	KindMap TypeKind = "map"
	// KindTuple is a positional fixed-arity sequence (Tuple).
	KindTuple TypeKind = "tuple"
	// KindLiteral is a single constant value as a type (Literal).
	KindLiteral TypeKind = "literal"
	// KindExternal is a well-known library type resolved by the backend (External).
	KindExternal TypeKind = "external"
	// KindAny is a schemaless type (Any).
	KindAny TypeKind = "any"
)

// TypeDef is the sealed sum of all type-graph nodes (ir-design §4). Concrete
// kinds: Primitive, Scalar, Model, Union, Enum, List, MapT, Tuple, Literal,
// External, Any. JSON encodes the sum with an adjacent "kind" tag.
type TypeDef interface {
	typeDef() // sealed: only this package's types implement TypeDef
	Kind() TypeKind
	Common() *TypeCommon
}

// UsageFlags is a bitset recording how a type is used across the API surface. It
// is computed by a pass and JSON-encoded as a number.
type UsageFlags uint32

// Usage bits.
const (
	// UsageInput marks a type reachable from a request payload.
	UsageInput UsageFlags = 1 << iota
	// UsageOutput marks a type reachable from a response payload.
	UsageOutput
	// UsageError marks a type reachable from an error payload.
	UsageError
	// UsageMultipart marks a type used in a multipart body.
	UsageMultipart
)

// TypeCommon carries the identity, documentation, and cross-cutting metadata
// shared by every type-graph node (ir-design §4). It is embedded in each
// concrete kind and inlined by encoding/json.
type TypeCommon struct {
	// ID is the type's stable synthetic identity in Document.Types.
	ID TypeID `json:"id"`
	// Name is the source/canonical naming of the type.
	Name Naming `json:"name"`
	// Namespace is the type's declared logical namespace (proto package, Avro
	// namespace, Thrift/XSD/Cap'n Proto scopes); independent of Service.Namespace.
	Namespace []string `json:"namespace,omitempty"`
	// Anonymous reports that this is a hoisted inline type.
	Anonymous bool `json:"anonymous"`
	// Docs is the human-readable documentation for the type.
	Docs Docs `json:"docs"`
	// Tags are free-form labels (Smithy @tags, OpenAPI tag membership via policy);
	// tag metadata lives once in Document.TagDefs.
	Tags []string `json:"tags,omitempty"`
	// Sensitive requests whole-type redaction (Smithy @sensitive on shapes);
	// Property.Secret is the per-use form.
	Sensitive bool `json:"sensitive"`
	// Access is "" for public or "internal" for a type outside the exported SDK
	// surface (protobuf editions export/local, TCGC @access(internal)).
	Access string `json:"access,omitempty"`
	// Deprecation marks the type as deprecated with optional migration guidance.
	Deprecation *Deprecation `json:"deprecation,omitempty"`
	// Availability records the type's versioning timeline.
	Availability *Availability `json:"availability,omitempty"`
	// Usage is the computed input/output/error/multipart usage bitset.
	Usage UsageFlags `json:"usage,omitempty"`
	// WireNameByFormat carries type-level serialized-name overrides per media type
	// (TypeSpec @encodedName on models/enums/scalars).
	WireNameByFormat map[string]string `json:"wireNameByFormat,omitempty"`
	// MediaTypeHint is the declared default content type when the type is a body
	// (TypeSpec @mediaTypeHint, Smithy @mediaType on string/blob shapes).
	MediaTypeHint string `json:"mediaTypeHint,omitempty"`
	// XML is the type-level XML wire shape: root element name/namespace.
	XML *XMLHints `json:"xml,omitempty"`
	// Examples are typed example values attached to the type.
	Examples []Example `json:"examples,omitempty"`
	// Instantiation records provenance for monomorphized generics (TypeSpec
	// templates).
	Instantiation *TemplateInstantiation `json:"instantiation,omitempty"`
	// Extensions carries source metadata without a first-class IR node.
	Extensions Extensions `json:"extensions,omitempty"`
	// Provenance records where the type came from.
	Provenance Provenance `json:"provenance"`
}

// TemplateInstantiation records the template and arguments a monomorphized
// generic type was produced from (ir-design §4).
type TemplateInstantiation struct {
	// Template names the source template.
	Template string `json:"template,omitempty"`
	// Args are the type and value arguments; instances are identified by both.
	Args []TemplateArg `json:"args,omitempty"`
}

// TemplateArg is one argument of a TemplateInstantiation; exactly one of Type or
// Value is set (TypeSpec valueof template parameters).
type TemplateArg struct {
	// Type is the type argument, set when this is a type parameter.
	Type *TypeRef `json:"type,omitempty"`
	// Value is the value argument, set when this is a valueof parameter.
	Value *Value `json:"value,omitempty"`
}

// newTypeDefByKind maps every TypeKind to a constructor of its concrete type.
// It is the single source of truth for kind dispatch: JSON decoding and the
// switch-completeness test both consume it.
var newTypeDefByKind = map[TypeKind]func() TypeDef{
	KindPrimitive: func() TypeDef { return &Primitive{} },
	KindScalar:    func() TypeDef { return &Scalar{} },
	KindModel:     func() TypeDef { return &Model{} },
	KindUnion:     func() TypeDef { return &Union{} },
	KindEnum:      func() TypeDef { return &Enum{} },
	KindList:      func() TypeDef { return &List{} },
	KindMap:       func() TypeDef { return &MapT{} },
	KindTuple:     func() TypeDef { return &Tuple{} },
	KindLiteral:   func() TypeDef { return &Literal{} },
	KindExternal:  func() TypeDef { return &External{} },
	KindAny:       func() TypeDef { return &Any{} },
}

// NewTypeDef returns a new zero-valued concrete TypeDef for kind k, or false
// when k is not a registered kind.
func NewTypeDef(k TypeKind) (TypeDef, bool) {
	ctor, ok := newTypeDefByKind[k]
	if !ok {
		return nil, false
	}
	return ctor(), true
}
