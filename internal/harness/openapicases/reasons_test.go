// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package openapicases_test

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
	"github.com/dexpace/morphic/internal/harness/openapicases"
)

// cites is the form a reason's text opens with: the issue tracking the
// finding, then what it says.
var cites = regexp.MustCompile(`^#[0-9]+: \S`)

// uncited names each reason whose text cites no issue, and why it need not:
// the key leaves with the fix that is under way.
var uncited = map[string]string{
	"unfiled-disjoint-value-keywords": "a fix on this branch lowers a value keyword contradicting another " +
		"alike in every spelling, and its lines leave with it",
}

// TestReasons_EachCitesAnIssue holds each reason a record may give to the
// issue tracking it, so a record grows no class nobody tracks: its text opens
// "#<issue>: ", but for a key uncited names, which must still be a key and cite
// none. And each key is given by some line, so one a fix leaves unused goes.
func TestReasons_EachCitesAnIssue(t *testing.T) {
	t.Parallel()
	reasons := openapicases.Reasons()
	for key, text := range reasons {
		if _, excused := uncited[key]; excused {
			assert.NotRegexp(t, cites, text, "%s cites an issue: drop it from uncited", key)
			continue
		}
		assert.Regexp(t, cites, text, "%s: open its text with the issue tracking it, as #<n>: ", key)
	}
	for key, why := range uncited {
		assert.NotEmpty(t, why, key)
		assert.Contains(t, reasons, key, "uncited names %s, which is no reason: drop it", key)
	}

	given := givenReasons(t)
	for _, key := range slices.Sorted(maps.Keys(reasons)) {
		assert.True(t, given[key], "no line of a record gives %s: delete it from Reasons", key)
	}
}

// givenReasons returns the reasons the lines of every record give, read from
// the third field of each line that is no comment.
func givenReasons(t *testing.T) map[string]bool {
	t.Helper()
	core, err := filepath.Glob("testdata/*.txt")
	require.NoError(t, err)
	full, err := filepath.Glob("testdata/full/*.txt")
	require.NoError(t, err)
	paths := slices.Concat(core, full)
	require.Len(t, paths, 2*len(harness.CaseOracles()), "a core and a full record per case oracle")
	given := map[string]bool{}
	for _, path := range paths {
		data, err := os.ReadFile(path)
		require.NoError(t, err)
		for line := range strings.Lines(string(data)) {
			fields := strings.Split(strings.TrimSuffix(line, "\n"), "\t")
			if len(fields) == 3 && !strings.HasPrefix(line, "#") {
				given[fields[2]] = true
			}
		}
	}
	return given
}
