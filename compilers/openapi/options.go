package openapi

import "github.com/dexpace/morphic/compilers/openapi/internal/lowering"

// GroupingStrategy selects how operations are grouped into OperationGroups. It
// is the injectable-policy seam (architecture principle 6): grouping is inferred
// policy, not source semantics, and can be switched or disabled.
//
// The vocabulary is declared once, beneath both walks, and named here rather
// than restated: two declarations of one strategy set can drift, and a strategy
// only the public half knew would fall through to the default unreported.
type GroupingStrategy = lowering.GroupingStrategy

// Grouping strategies.
const (
	// GroupByTags groups operations by their first OpenAPI tag (default).
	GroupByTags = lowering.GroupByTags
	// GroupByPathPrefix groups operations by the first path segment.
	GroupByPathPrefix = lowering.GroupByPathPrefix
)

// Options configures the OpenAPI compiler. It is the concrete type this
// compiler expects in compilers.Options.FormatOptions; the zero value is valid
// and normalized by withDefaults.
//
// It stays whole here while each phase below takes its own input projected from
// it — load.Options, and the strategy the lowering context carries. Its shape is
// a published contract (ir-design §10), and most of it describes work no single
// phase does.
type Options struct {
	// Grouping selects the operation-grouping strategy.
	Grouping GroupingStrategy `json:"grouping,omitempty"`
	// DisableExternalRefs prevents resolution of $refs into other documents.
	DisableExternalRefs bool `json:"disableExternalRefs"`
}

// withDefaults returns a copy of o with unset fields filled from the defaults.
func (o Options) withDefaults() Options {
	if o.Grouping == "" {
		o.Grouping = GroupByTags
	}
	return o
}
