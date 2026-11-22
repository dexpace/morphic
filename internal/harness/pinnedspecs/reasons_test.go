// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package pinnedspecs_test

import (
	"maps"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dexpace/morphic/internal/harness"
	"github.com/dexpace/morphic/internal/harness/pinnedspecs"
)

// reasons names each reason a record of a listed spec may give, by key.
var reasons = map[string]string{
	"accept-header": "expected: the spec declares a header parameter named Accept, which OpenAPI says is " +
		"ignored, so the compiler warns of it (docs/compilers/openapi.md#reserved-header-names)",
	"no-sequence": "expected: the spec writes no allOf and no required list, so the sequence-order oracle " +
		"has nothing to reverse",
}

// stands is the form a reason's text opens with: "expected" where the spec
// really says what drew the finding, or the open issue tracking it, then why.
var stands = regexp.MustCompile(`^(expected|#[0-9]+): \S`)

// TestReasons_EachIsExpectedOrCitesAnIssue holds each reason to the form
// stands names, so a record grows no class nobody can account for, and each
// key to some line of a record that gives it, so one a fix leaves unused goes.
// No test reads whether a cited issue is still open.
func TestReasons_EachIsExpectedOrCitesAnIssue(t *testing.T) {
	t.Parallel()
	given := map[string]bool{}
	for _, path := range recordFiles(t) {
		for line := range strings.Lines(readFile(t, path)) {
			fields := strings.Split(strings.TrimSuffix(line, "\n"), "\t")
			if len(fields) == 3 && !strings.HasPrefix(line, "#") {
				given[fields[2]] = true
			}
		}
	}
	for _, key := range slices.Sorted(maps.Keys(reasons)) {
		assert.Regexp(t, stands, reasons[key], "%s: open its text with expected: or #<issue>: ", key)
		assert.True(t, given[key], "no line of a record gives %s: delete it from reasons", key)
	}
}

// TestRecords_OneForEachListedSpec holds the records to the list offline, as
// the gate runs: the list reads, each spec it names has a record and each
// record a spec, and each record reads, every reason it gives one reasons
// names. What a fetched spec draws only make sweep reads.
func TestRecords_OneForEachListedSpec(t *testing.T) {
	t.Parallel()
	specs, err := pinnedspecs.ReadList([]byte(readFile(t, listPath)))
	require.NoError(t, err)

	listed := make([]string, 0, len(specs))
	for _, spec := range specs {
		listed = append(listed, filepath.Join(recordsPath, spec.Name+".txt"))
	}
	paths := recordFiles(t)
	assert.ElementsMatch(t, listed, paths, "a record per listed spec, and no other")
	for _, path := range paths {
		record, err := harness.ReadRecorded([]byte(readFile(t, path)))
		require.NoError(t, err, path)
		_, _, _, unreasoned := record.Judge(nil, reasons)
		assert.Empty(t, unreasoned, "%s: a line whose reason the reasons do not name", path)
	}
}

// recordFiles returns the path of each record of a listed spec.
func recordFiles(t *testing.T) []string {
	t.Helper()
	paths, err := filepath.Glob(filepath.Join(recordsPath, "*"))
	require.NoError(t, err)
	require.NotEmpty(t, paths, "found no record, so this proves nothing")
	return paths
}

// readFile returns the text of the file at path.
func readFile(t *testing.T, path string) string {
	t.Helper()
	data, err := os.ReadFile(path)
	require.NoError(t, err)
	return string(data)
}
