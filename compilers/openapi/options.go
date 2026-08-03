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
	// AllowExternalRefs lets reference resolution leave the source document —
	// reading files off disk and fetching http(s) URLs. Off by default, because
	// compilers.Source is the whole input ("the caller loads bytes so compilation
	// stays pure and reentrant") and a spec is untrusted data whose $refs would
	// otherwise name any readable file or reachable host. A $ref leaving the
	// document is reported unresolved instead of followed.
	//
	// Turning it on departs from that contract knowingly, and buys less than it
	// looks: resolution reads relative to the process working directory, so the
	// same bytes compile differently in two directories, and the resolved content
	// still does not reach lowering — Sources records one entry either way
	// (GitHub #74 carries the multi-file work).
	AllowExternalRefs bool `json:"allowExternalRefs"`
}

// withDefaults returns a copy of o with unset fields filled from the defaults.
func (o Options) withDefaults() Options {
	if o.Grouping == "" {
		o.Grouping = GroupByTags
	}
	return o
}
