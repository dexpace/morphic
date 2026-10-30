// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package lowering

import "github.com/dexpace/morphic/ir"

// GroupPathPrefixHeuristic is the name Provenance.Inferred carries on an
// operation GroupByPathPrefix filed. It is a constant for the reason
// StreamingMediaTypeHeuristic is: an auditor greps for it.
const GroupPathPrefixHeuristic ir.Inferred = "group-path-prefix"

// GroupingStrategy selects how operations are grouped into OperationGroups. It
// is the injectable-policy seam (architecture principle 6): grouping is inferred
// policy, not source semantics, and can be switched or disabled.
//
// The vocabulary is declared here, below both walks, and re-exported by the
// compiler's public options rather than restated there. One declaration is what
// keeps the two spellings from drifting apart: a strategy the public type names
// and the lowering does not recognize would silently fall through to the
// default, and nothing would report it.
type GroupingStrategy string

// Grouping strategies.
const (
	// GroupByTags groups operations by their first OpenAPI tag (default).
	GroupByTags GroupingStrategy = "tags"
	// GroupByPathPrefix groups operations by the first path segment.
	GroupByPathPrefix GroupingStrategy = "path-prefix"
)
