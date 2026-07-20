package ir

// Naming carries the identity of a named entity as words, never as a cased
// identifier: emitters apply casing, acronym policy, and reserved-word escaping
// (ir-design §3.2). Anonymous (hoisted) types have an empty Source and a Hint.
type Naming struct {
	// Source is the name exactly as written in the spec ("user_id", a $ref
	// name, a GraphQL field).
	Source string `json:"source,omitempty"`
	// Canonical is the IR-normalized identifier in neutral form: lower_snake
	// words with no casing opinions.
	Canonical string `json:"canonical,omitempty"`
	// Hint is a context-derived suggestion for anonymous types only
	// (e.g. "connection_domain").
	Hint string `json:"hint,omitempty"`
	// Aliases are alternate names for schema-resolution matching (Avro
	// aliases). Versionless — rename history tied to version labels lives in
	// Availability.RenamedFrom.
	Aliases []string `json:"aliases,omitempty"`
}
