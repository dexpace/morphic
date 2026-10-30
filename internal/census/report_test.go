// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package census_test

import (
	"slices"
	"strings"
	"testing"

	"github.com/google/go-cmp/cmp"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dexpace/morphic/internal/census"
	"github.com/dexpace/morphic/internal/census/testdata/fixture"
)

func TestReport_ListsEveryFactOfTheUniverseWhateverIsCounted(t *testing.T) {
	t.Parallel()
	empty := facts(t, census.New(fixtureUniverse(t)).Report())
	got := counted(t, &fixture.Root{Name: "n", Color: "green"})

	for fact, n := range empty {
		switch fact {
		case "documents", "no-document", "truncated":
			continue
		}
		assert.Equal(t, "0 0", n, fact)
		assert.Contains(t, got, fact, "counting a document removes no fact from the report")
	}
	assert.Contains(t, got, `value Color "green" undeclared`)
}

func TestReport_IsDeterministicAndOrdersValuesByValue(t *testing.T) {
	t.Parallel()
	build := func() string {
		c := census.New(fixtureUniverse(t))
		require.NoError(t, c.AddAny(&fixture.Root{
			Color:  "green",
			Labels: map[string]fixture.Level{"a": 5, "b": -5, "c": 1000},
		}))
		return c.Report()
	}
	first, second := build(), build()
	if diff := cmp.Diff(first, second); diff != "" {
		t.Errorf("two runs differ (-first +second):\n%s", diff)
	}

	var values []string
	for line := range strings.Lines(first) {
		if fields := strings.Fields(line); fields[0] == "value" {
			values = append(values, strings.Join(fields[3:5], " "))
		}
	}
	want := []string{
		`Color "blue"`, `Color "green"`, `Color "red"`,
		`Kind "circle"`, `Kind "hexagon"`, `Kind "square"`,
		`Level -20`, `Level -5`, `Level -1`, `Level 0`, `Level 5`, `Level 10`, `Level 100`, `Level 1000`,
	}
	if diff := cmp.Diff(want, values); diff != "" {
		t.Errorf("values (-want +got):\n%s", diff)
	}
}

func TestReport_ListsMembersByName(t *testing.T) {
	t.Parallel()
	var names []string
	for line := range strings.Lines(irCensus(t).Report()) {
		if fields := strings.Fields(line); fields[0] == "member" {
			names = append(names, fields[3])
		}
	}
	assert.True(t, slices.IsSorted(names))
}
