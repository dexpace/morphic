package protobuf

// WrapperPolicy selects how the google.protobuf wrapper types (Int32Value,
// StringValue, …) lower into the IR. It is the injectable-policy seam
// (architecture principle 6): the mapping is a modeling choice, not a source
// fact, and can be switched.
type WrapperPolicy string

// Wrapper policies.
const (
	// WrapperNullablePrimitive lowers each wrapper to its underlying primitive
	// with a nullable reference (the default). This matches the wrappers' purpose
	// — giving a scalar explicit presence — and their proto-JSON form, where a
	// wrapper serializes as the bare value or null.
	WrapperNullablePrimitive WrapperPolicy = "nullable-primitive"
	// WrapperExternal lowers each wrapper to an External referencing the
	// well-known runtime type, preserving the box as a distinct type.
	WrapperExternal WrapperPolicy = "external"
)

// Options configures the protobuf compiler. It is the concrete type this
// compiler expects in compilers.Options.FormatOptions; the zero value is valid
// and normalized by withDefaults.
type Options struct {
	// Wrappers selects how google.protobuf wrapper types lower.
	Wrappers WrapperPolicy `json:"wrappers,omitempty"`
}

// withDefaults returns a copy of o with unset fields filled from the defaults.
func (o Options) withDefaults() Options {
	if o.Wrappers == "" {
		o.Wrappers = WrapperNullablePrimitive
	}
	return o
}
