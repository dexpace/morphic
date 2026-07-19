package openapi

// GroupingStrategy selects how operations are grouped into OperationGroups. It
// is the injectable-policy seam (architecture principle 6): grouping is inferred
// policy, not source semantics, and can be switched or disabled.
type GroupingStrategy string

// Grouping strategies.
const (
	// GroupByTags groups operations by their first OpenAPI tag (default).
	GroupByTags GroupingStrategy = "tags"
	// GroupByPathPrefix groups operations by the first path segment.
	GroupByPathPrefix GroupingStrategy = "path-prefix"
)

// Options configures the OpenAPI frontend. It is the concrete type this
// frontend expects in frontend.Options.FormatOptions; the zero value is valid
// and normalized by withDefaults.
type Options struct {
	// Grouping selects the operation-grouping strategy.
	Grouping GroupingStrategy `json:"grouping,omitempty"`
	// DisableExternalRefs prevents resolution of $refs into other documents.
	DisableExternalRefs bool `json:"disableExternalRefs"`
}

// withDefaults returns a copy of o with unset fields filled from the defaults.
//
//nolint:unused // applied by the frontend entrypoint in a later task
func (o Options) withDefaults() Options {
	if o.Grouping == "" {
		o.Grouping = GroupByTags
	}
	return o
}
