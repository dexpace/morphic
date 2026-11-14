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

// TestGroupingExtension_PathsReadEachSpelling pins what a grouping
// extension's value states under each reading of a list. A string is one path
// either way, split at its dots; a list is one path or one per entry by the
// key's convention, and a value with an empty segment anywhere, or deeper than
// MaxGroupPathDepth, names no group.
func TestGroupingExtension_PathsReadEachSpelling(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name     string
		raw      string
		segments []string   // the one path ListsAreSegments reads
		groups   [][]string // the paths ListsAreGroups reads
		misfit   bool
		// misfitAsSegments is a value only ListsAreSegments refuses.
		misfitAsSegments bool
	}{
		{name: "one segment", raw: `"accounts"`, segments: []string{"accounts"}, groups: [][]string{{"accounts"}}},
		{
			name: "a dotted path", raw: `"zero-trust.devices.policies"`,
			segments: []string{"zero-trust", "devices", "policies"},
			groups:   [][]string{{"zero-trust", "devices", "policies"}},
		},
		{
			name: "a list", raw: `["users","notifications"]`,
			segments: []string{"users", "notifications"}, groups: [][]string{{"users"}, {"notifications"}},
		},
		{
			name: "a list of dotted paths", raw: `["a.b","c"]`,
			segments: []string{"a", "b", "c"}, groups: [][]string{{"a", "b"}, {"c"}},
		},
		{name: "a list of one", raw: `["users"]`, segments: []string{"users"}, groups: [][]string{{"users"}}},
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
		{
			name: "a path as deep as the bound", raw: deepPath(lowering.MaxGroupPathDepth),
			segments: deep, groups: [][]string{deep},
		},
		{name: "a path past the bound", raw: deepPath(lowering.MaxGroupPathDepth + 1), misfit: true},
		{
			name: "a list as deep as the bound", raw: `["` + strings.Repeat("a.", lowering.MaxGroupPathDepth-2) + `a","a"]`,
			segments: deep, groups: [][]string{deep[1:], {"a"}},
		},
		{
			name: "a list past the bound", raw: `["` + strings.Repeat("a.", lowering.MaxGroupPathDepth-1) + `a","a"]`,
			groups: [][]string{deep, {"a"}},
			// Only the entries' sum is past it, and ListsAreGroups reads each alone.
			misfitAsSegments: true,
		},
		{name: "a list holding an entry past the bound", raw: `["a",` + deepPath(lowering.MaxGroupPathDepth+1) + `]`, misfit: true},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			var segments [][]string
			if tc.segments != nil {
				segments = [][]string{tc.segments}
			}
			for _, read := range []struct {
				lists  lowering.ExtensionLists
				want   [][]string
				misfit bool
			}{
				{lowering.ListsAreSegments, segments, tc.misfit || tc.misfitAsSegments},
				{lowering.ListsAreGroups, tc.groups, tc.misfit},
			} {
				got, ok := lowering.GroupingExtension{Key: "x-group", Lists: read.lists}.Paths(ir.RawValue(tc.raw))
				assert.Equal(t, !read.misfit, ok, "%s: whether the value states group paths", read.lists)
				assert.Equal(t, read.want, got, "%s: the paths read", read.lists)
			}
		})
	}
}

// TestGroupingExtension_AliasPathsReadEachSpelling pins what an aliases
// extension's value states: a list of objects, each stating paths under the
// grouping key as that key's own value does, read under the key's list reading.
// An object without the key states none, and anything else names no group, the
// list judged whole.
func TestGroupingExtension_AliasPathsReadEachSpelling(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name     string
		raw      string
		segments [][]string // what ListsAreSegments reads
		groups   [][]string // what ListsAreGroups reads
		misfit   bool
	}{
		{
			name: "two aliases", raw: `[{"x-group":"o11y.issues","x-name":"list"},{"x-group":"o11y"}]`,
			segments: [][]string{{"o11y", "issues"}, {"o11y"}}, groups: [][]string{{"o11y", "issues"}, {"o11y"}},
		},
		{
			name: "an alias stating a list", raw: `[{"x-group":["a","b"]}]`,
			segments: [][]string{{"a", "b"}}, groups: [][]string{{"a"}, {"b"}},
		},
		{name: "an alias stating no key", raw: `[{"x-name":"list"},{"x-group":"a"}]`,
			segments: [][]string{{"a"}}, groups: [][]string{{"a"}}},
		{name: "an alias stating empty text", raw: `[{"x-group":""}]`},
		{name: "an empty list", raw: `[]`},
		{name: "null", raw: `null`},
		{name: "an empty object", raw: `[{}]`},
		{name: "an alias whose path misfits", raw: `[{"x-group":"a"},{"x-group":"a..b"}]`, misfit: true},
		{name: "an alias that is text", raw: `["a"]`, misfit: true},
		{name: "an alias that is null", raw: `[null]`, misfit: true},
		{name: "an alias that is a list", raw: `[["a"]]`, misfit: true},
		{name: "text", raw: `"a"`, misfit: true},
		{name: "an object", raw: `{"x-group":"a"}`, misfit: true},
		{name: "a number", raw: `1`, misfit: true},
		{name: "a key written twice", raw: `[{"x-group":"a","x-group":"b"}]`, misfit: true},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			for _, read := range []struct {
				lists lowering.ExtensionLists
				want  [][]string
			}{
				{lowering.ListsAreSegments, tc.segments},
				{lowering.ListsAreGroups, tc.groups},
			} {
				ext := lowering.GroupingExtension{Key: "x-group", Lists: read.lists, Aliases: "x-aliases"}
				got, ok := ext.AliasPaths(ir.RawValue(tc.raw))
				assert.Equal(t, !tc.misfit, ok, "%s: whether the value lists aliases", read.lists)
				assert.Equal(t, read.want, got, "%s: the paths read", read.lists)
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
	assert.Equal(t, "x-forge-aliases", ext.Aliases, "the aliases its key's convention restates it in")
	assert.Empty(t, ext.Fallback, "the fallback is the caller's, defaulted by the compiler's options")
}

// TestGroupingExtension_KeysAreWhatGroupingReads pins the keys grouping reads
// to the strategy: its key and its aliases key by extension, either alone where
// the other is unnamed, and none under any other strategy, whatever the
// extension names.
func TestGroupingExtension_KeysAreWhatGroupingReads(t *testing.T) {
	t.Parallel()
	both := lowering.DefaultGroupingExtension()
	keyAlone := lowering.GroupingExtension{Key: "x-k", Lists: lowering.ListsAreGroups}
	aliasesAlone := lowering.GroupingExtension{Aliases: "x-a"}
	for _, tc := range []struct {
		name     string
		ext      lowering.GroupingExtension
		strategy lowering.GroupingStrategy
		want     []string
	}{
		{"the key and its aliases", both, lowering.GroupByExtension, []string{"x-fern-sdk-group-name", "x-forge-aliases"}},
		{"a key with no aliases", keyAlone, lowering.GroupByExtension, []string{"x-k"}},
		{"aliases with no key", aliasesAlone, lowering.GroupByExtension, []string{"x-a"}},
		{"nothing named", lowering.GroupingExtension{}, lowering.GroupByExtension, nil},
		{"by tags", both, lowering.GroupByTags, nil},
		{"by path prefix", both, lowering.GroupByPathPrefix, nil},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, tc.want, tc.ext.Keys(tc.strategy))
		})
	}
}
