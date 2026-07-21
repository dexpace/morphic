package graphql

// Options configures the GraphQL compiler. It is the concrete type this compiler
// expects in compilers.Options.FormatOptions; the zero value is valid and
// normalized by withDefaults.
//
// GraphQL's operation grouping is fixed by the language (query, mutation,
// subscription), so there is no grouping policy here. The single knob is whether
// to emit the document-level directive-definition inventory, which is verbose
// and only some emitters consume.
type Options struct {
	// OmitDirectiveDefinitions suppresses the verbatim directive-definition
	// inventory otherwise stored under Extensions["graphql:directive-definitions"].
	OmitDirectiveDefinitions bool `json:"omitDirectiveDefinitions,omitempty"`
}

// withDefaults returns a copy of o with unset fields filled from the defaults.
// The zero value is already the intended default, so it is returned unchanged;
// the method exists to mirror the compiler-options contract and to give future
// defaults one home.
func (o Options) withDefaults() Options {
	return o
}
