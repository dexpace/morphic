package merge

import (
	"slices"
	"strings"

	"github.com/dexpace/morphic/ir"
)

// intersectNull returns dst admitting null only where both references do, the
// rule foldNullVerdicts states for a single schema. The result's null rests on
// a heuristic wherever either side's did, since the declared reading admits it
// only where both declare it, so it names every marker either side carries,
// dst's first and none twice.
func intersectNull(dst, src ir.TypeRef) ir.TypeRef {
	dst.Nullable = dst.Nullable && src.Nullable
	if !dst.Nullable {
		dst.Inferred = ""
		return dst
	}
	dst.Inferred = joinMarkers(dst.Inferred, src.Inferred)
	return dst
}

// joinMarkers adds each marker of the comma-separated list more to list, in
// order, unless it is empty or already there (joinMarker).
func joinMarkers(list, more string) string {
	for marker := range strings.SplitSeq(more, ",") {
		list = joinMarker(list, marker)
	}
	return list
}

// joinMarker adds marker to the comma-separated list of heuristic names in
// list, unless it is empty or already there.
func joinMarker(list, marker string) string {
	switch {
	case marker == "" || slices.Contains(strings.Split(list, ","), marker):
		return list
	case list == "":
		return marker
	default:
		return list + "," + marker
	}
}
