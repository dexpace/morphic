package ir_test

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/dexpace/morphic/ir"
)

// TestJoinInferred_ListsEachNameOnceInFirstWrittenOrder pins the one form an
// inferred-marker list takes: every name once, in the order first written,
// never sorted, with nothing empty and nothing trimmed. Each argument is itself
// a list, so one heuristic's name and a stored list go through the same call.
func TestJoinInferred_ListsEachNameOnceInFirstWrittenOrder(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name  string
		lists []string
		want  string
	}{
		{"no argument", nil, ""},
		{"one empty list", []string{""}, ""},
		{"one name", []string{"a"}, "a"},
		{"an empty list first", []string{"", "a"}, "a"},
		{"an empty list last", []string{"a", ""}, "a"},
		{"a name twice", []string{"a", "a"}, "a"},
		{"the first written leads, nothing is sorted", []string{"b", "a"}, "b,a"},
		{"two lists sharing a name", []string{"a,b", "b,c"}, "a,b,c"},
		{"an empty name inside a list", []string{"a,,b"}, "a,b"},
		{"a list with a leading and a trailing separator", []string{",a,"}, "a"},
		{"a name repeated inside one list", []string{"a,b,a"}, "a,b"},
		{"names compare exactly and nothing is trimmed", []string{" a", "a"}, " a,a"},
		{"the heuristics the compiler writes", []string{
			"group-path-prefix", "streaming-media-type", "extension-promotion",
		}, "group-path-prefix,streaming-media-type,extension-promotion"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, tc.want, ir.JoinInferred(tc.lists...))
		})
	}
}
