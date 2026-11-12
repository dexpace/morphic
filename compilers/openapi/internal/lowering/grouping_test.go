// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package lowering_test

import (
	"slices"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/dexpace/morphic/compilers/openapi/internal/lowering"
	"github.com/dexpace/morphic/ir"
)

// deep is a path of MaxGroupPathDepth segments, each "a".
var deep = slices.Repeat([]string{"a"}, lowering.MaxGroupPathDepth)

// deepPath is a dotted path of depth segments, as JSON text.
func deepPath(depth int) string {
	return `"` + strings.Repeat("a.", depth-1) + `a"`
}

// TestGroupingExtension_PathReadsEachSpelling pins what a grouping extension's
// value states under each reading of a list. A string is one path either way,
// split at its dots; a list is one path or several by the key's convention, and
// a value with an empty segment anywhere, or deeper than MaxGroupPathDepth,
// names no group.
func TestGroupingExtension_PathReadsEachSpelling(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name     string
		raw      string
		segments []string // what ListsAreSegments reads
		groups   []string // what ListsAreGroups reads
		misfit   bool
		// misfitAsSegments is a value only ListsAreSegments refuses.
		misfitAsSegments bool
	}{
		{name: "one segment", raw: `"accounts"`, segments: []string{"accounts"}, groups: []string{"accounts"}},
		{
			name: "a dotted path", raw: `"zero-trust.devices.policies"`,
			segments: []string{"zero-trust", "devices", "policies"}, groups: []string{"zero-trust", "devices", "policies"},
		},
		{
			name: "a list", raw: `["users","notifications"]`,
			segments: []string{"users", "notifications"}, groups: []string{"users"},
		},
		{
			name: "a list of dotted paths", raw: `["a.b","c"]`,
			segments: []string{"a", "b", "c"}, groups: []string{"a", "b"},
		},
		{name: "a list of one", raw: `["users"]`, segments: []string{"users"}, groups: []string{"users"}},
		{name: "empty text", raw: `""`},
		{name: "an empty list", raw: `[]`},
		{name: "null", raw: `null`},
		{name: "a leading dot", raw: `".a"`, misfit: true},
		{name: "a trailing dot", raw: `"a."`, misfit: true},
		{name: "two dots in a row", raw: `"a..b"`, misfit: true},
		{name: "a lone dot", raw: `"."`, misfit: true},
		{name: "an empty entry", raw: `["a",""]`, misfit: true},
		{name: "an empty entry first", raw: `["","a"]`, misfit: true},
		{name: "an entry with an empty segment", raw: `["a","b..c"]`, misfit: true},
		{name: "an entry that is null", raw: `["a",null]`, misfit: true},
		{name: "an entry that is not text", raw: `["a",1]`, misfit: true},
		{name: "a number", raw: `7`, misfit: true},
		{name: "a boolean", raw: `true`, misfit: true},
		{name: "an object", raw: `{"group":"a"}`, misfit: true},
		{name: "a path as deep as the bound", raw: deepPath(lowering.MaxGroupPathDepth), segments: deep, groups: deep},
		{name: "a path past the bound", raw: deepPath(lowering.MaxGroupPathDepth + 1), misfit: true},
		{
			name: "a list as deep as the bound", raw: `["` + strings.Repeat("a.", lowering.MaxGroupPathDepth-2) + `a","a"]`,
			segments: deep, groups: deep[1:],
		},
		{
			name: "a list past the bound", raw: `["` + strings.Repeat("a.", lowering.MaxGroupPathDepth-1) + `a","a"]`,
			groups: deep,
			// Only the entries' sum is past it, and ListsAreGroups files under the first.
			misfitAsSegments: true,
		},
		{name: "a list holding an entry past the bound", raw: `["a",` + deepPath(lowering.MaxGroupPathDepth+1) + `]`, misfit: true},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			for _, read := range []struct {
				lists  lowering.ExtensionLists
				want   []string
				misfit bool
			}{
				{lowering.ListsAreSegments, tc.segments, tc.misfit || tc.misfitAsSegments},
				{lowering.ListsAreGroups, tc.groups, tc.misfit},
			} {
				got, ok := lowering.GroupingExtension{Key: "x-group", Lists: read.lists}.Path(ir.RawValue(tc.raw))
				assert.Equal(t, !read.misfit, ok, "%s: whether the value is a group path", read.lists)
				assert.Equal(t, read.want, got, "%s: the path read", read.lists)
			}
		})
	}
}

// TestDefaultGroupingExtension_ReadsAListAsOnePath pins the default to the
// convention its key follows: x-fern-sdk-group-name writes a list as the
// segments of one nested path, so reading it as several groups would file
// every such operation one level too high.
func TestDefaultGroupingExtension_ReadsAListAsOnePath(t *testing.T) {
	t.Parallel()
	ext := lowering.DefaultGroupingExtension()
	assert.Equal(t, "x-fern-sdk-group-name", ext.Key)
	assert.Equal(t, lowering.ListsAreSegments, ext.Lists)
	assert.Empty(t, ext.Fallback, "the fallback is the caller's, defaulted by the compiler's options")
}
