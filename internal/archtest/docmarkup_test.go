// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package archtest_test

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"testing"

	"github.com/google/go-cmp/cmp"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dexpace/morphic/internal/keydoc"
	"github.com/dexpace/morphic/ir/irtest"
)

// plusMarker is a line GitHub renders as a list item, even inside a paragraph.
// No document writes a list with it, so it is a sum a reflow left at the start
// of a line, which turns the rest of the paragraph into a list. Every other
// marker has real lists to tell it from.
var plusMarker = regexp.MustCompile(`^ {0,3}\+ `)

// TestDocuments_OpenNoLineWithAPlus fails on a Markdown document with a line
// that renders as a list item by accident. A comparison with the text before a
// reflow cannot see one, since the words are the same.
func TestDocuments_OpenNoLineWithAPlus(t *testing.T) {
	t.Parallel()
	root := irtest.RepoRoot(t)
	var documents int
	err := keydoc.Walk(root, ".", func(path string) error {
		rel := keydoc.Rel(root, path)
		if filepath.Ext(path) != ".md" || slices.Contains(strings.Split(rel, "/"), "testdata") {
			return nil
		}
		documents++
		text, err := os.ReadFile(path)
		if err != nil {
			return fmt.Errorf("read %s: %w", rel, err)
		}
		for _, line := range plusLines(string(text)) {
			t.Errorf("%s:%d opens with a list marker, which a reflowed sum renders as a list", rel, line)
		}
		return nil
	})
	require.NoError(t, err)
	require.NotZero(t, documents, "found no document, so this proves nothing")
}

// plusLines returns the number of each line of a Markdown document that opens
// with the plus marker outside a fenced block.
func plusLines(text string) []int {
	var lines []int
	for i, line := range proseLines(text) {
		if plusMarker.MatchString(line) {
			lines = append(lines, i+1)
		}
	}
	return lines
}

func TestPlusLines_ReportsAMarkerOutsideAFence(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		text string
		want []int
	}{
		{"a sum reflowed to a line's start", "node set\n+ printer\n", []int{2}},
		{"indented as far as a marker may be", "a\n   + b\n", []int{2}},
		{"indented into a code block", "a\n\n    + b\n", nil},
		{"a sign on a number", "a\n+1 more\n", nil},
		{"a plus inside a line", "a + b\n", nil},
		{"a diff in a fence", "```diff\n+ added\n```\n", nil},
		{"past a fence", "```\nx\n```\n+ b\n", []int{4}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			assert.Empty(t, cmp.Diff(tt.want, plusLines(tt.text)), "mismatch (-want +got)")
		})
	}
}
