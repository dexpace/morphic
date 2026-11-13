// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package lowering

import (
	"encoding/json/jsontext"
	"encoding/json/v2"
	"slices"
	"strings"

	"github.com/dexpace/morphic/ir"
)

// GroupPathPrefixHeuristic is the name Provenance.Inferred carries on an
// operation GroupByPathPrefix filed. It is a constant for the reason
// StreamingMediaTypeHeuristic is: an auditor greps for it.
const GroupPathPrefixHeuristic ir.Inferred = "group-path-prefix"

// GroupExtensionHeuristic is the name Provenance.Inferred carries on an
// operation GroupByExtension filed by the path its extension states. OpenAPI
// gives an x-* key no meaning, so reading one as a group is a convention's
// reading, not the document's.
const GroupExtensionHeuristic ir.Inferred = "group-extension"

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
	// GroupByExtension groups operations by the group path a vendor extension
	// states, nesting a group for each segment, and an operation stating none by
	// GroupingExtension.Fallback.
	GroupByExtension GroupingStrategy = "extension"
)

// GroupingExtension is what GroupByExtension reads: which extension states an
// operation's group path, what a list there means, which extension lists the
// further paths it is exposed under, and how an operation stating none is
// grouped. Conventions disagree on a list, so its reading is the key's and is
// never assumed from another key's.
type GroupingExtension struct {
	// Key is the extension read, x- prefix included. Empty reads none.
	Key string `json:"key,omitempty"`
	// Lists is what a list value means under Key. Any value but ListsAreGroups
	// reads one as ListsAreSegments; the compiler's options refuse a Key that
	// names none.
	Lists ExtensionLists `json:"lists,omitempty"`
	// Aliases is the extension listing an operation's aliases, each an object
	// stating a further group path under Key (AliasPaths). A group is minted for
	// each, for a remount to name, and no operation is filed in it. Empty reads
	// none.
	Aliases string `json:"aliases,omitempty"`
	// Fallback groups an operation Key states no path for: GroupByTags or
	// GroupByPathPrefix, any other value, empty included, grouping by tags.
	Fallback GroupingStrategy `json:"fallback,omitempty"`
}

// ExtensionLists is what a list value of a grouping extension means. A string
// is one path either way, its segments separated by dots.
type ExtensionLists string

// The readings of a list value.
const (
	// ListsAreSegments reads a list as one path, each entry adding its
	// segments, as x-fern-sdk-group-name writes one.
	ListsAreSegments ExtensionLists = "segments"
	// ListsAreGroups reads a list as several paths, each a group the operation
	// belongs to, as x-speakeasy-group writes them. The operation is filed under
	// the first, and a group is minted for each of the rest, for the remount a
	// consumer reads from the extension, which stays kept.
	ListsAreGroups ExtensionLists = "groups"
)

// DefaultGroupingExtension returns the extension GroupByExtension reads where
// the caller names none: x-fern-sdk-group-name, whose list is one path, beside
// x-forge-aliases, whose entries restate it for each alias. It is a default and
// not a standard, as DefaultExtensionPromotions is.
func DefaultGroupingExtension() GroupingExtension {
	return GroupingExtension{Key: "x-fern-sdk-group-name", Lists: ListsAreSegments, Aliases: "x-forge-aliases"}
}

// groupPathSeparator separates the segments of a group path a string writes.
const groupPathSeparator = "."

// MaxGroupPathDepth bounds the segments of a group path, so the groups it nests:
// 16, a 2.3x margin over the 7 the golden spec's deepest writes (spec
// @e411be7e). Each group's ID spells the path to it, so the IDs of a path's
// groups weigh half its depth times its length, and the bound is that factor.
const MaxGroupPathDepth = 16

// Paths reads the group paths raw states, an extension's value as Unmodeled
// keeps it, each outermost segment first: one under ListsAreSegments, each
// entry's under ListsAreGroups, the first being the one the operation is filed
// under. Empty text, an empty list and null state none. It returns false for a
// misfit: any other value, or a path holding an empty segment, which no group
// is named by, or one deeper than MaxGroupPathDepth. A list is a misfit if any
// entry is, since the value is the document's whole statement.
func (g GroupingExtension) Paths(raw ir.RawValue) ([][]string, bool) {
	entries, ok := listedText(raw)
	if !ok {
		return nil, false
	}
	paths := make([][]string, 0, len(entries))
	depth := 0
	for _, entry := range entries {
		path, ok := splitGroupPath(entry)
		if !ok {
			return nil, false
		}
		paths = append(paths, path)
		depth += len(path)
	}
	switch {
	case len(paths) == 0:
		return nil, true
	case g.Lists == ListsAreGroups:
		return paths, true
	case depth > MaxGroupPathDepth:
		return nil, false
	default:
		return [][]string{slices.Concat(paths...)}, true
	}
}

// AliasPaths reads the group paths raw, the Aliases extension's value as
// Unmodeled keeps it, names: a list of objects, each stating paths under Key as
// Key's own value does (Paths). An object without Key, an empty list and null
// state none. It returns false for a misfit: any other value, an entry that is
// no object, or one whose paths are a misfit, the list judged whole as Paths
// judges one.
func (g GroupingExtension) AliasPaths(raw ir.RawValue) ([][]string, bool) {
	var entries []jsontext.Value
	if err := json.Unmarshal(raw, &entries); err != nil {
		return nil, false
	}
	var paths [][]string
	for _, entry := range entries {
		var fields map[string]jsontext.Value
		if entry.Kind() != '{' || json.Unmarshal(entry, &fields) != nil {
			return nil, false
		}
		stated, written := fields[g.Key]
		if !written {
			continue
		}
		more, ok := g.Paths(stated)
		if !ok {
			return nil, false
		}
		paths = append(paths, more...)
	}
	return paths, true
}

// splitGroupPath splits text, which is not empty, into the segments of a group
// path, false where one is empty or there are more than MaxGroupPathDepth,
// which it counts before splitting anything.
func splitGroupPath(text string) ([]string, bool) {
	if strings.Count(text, groupPathSeparator) >= MaxGroupPathDepth {
		return nil, false
	}
	segments := strings.Split(text, groupPathSeparator)
	if slices.Contains(segments, "") {
		return nil, false
	}
	return segments, true
}
