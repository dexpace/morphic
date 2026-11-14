// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package irfacts_test

import (
	"fmt"
	"maps"
	"slices"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// staleEntries returns one message for each key of a hand-kept table that no
// position of the IR read, in key order, each naming the table and the key.
func staleEntries(table string, listed []string, read map[string]bool) []string {
	var stale []string
	for _, key := range slices.Sorted(slices.Values(listed)) {
		if !read[key] {
			stale = append(stale, fmt.Sprintf("%s lists %s, but the IR has no such position", table, key))
		}
	}
	return stale
}

// walkEveryKind reads every kind's parts by roles and notFields, and returns the
// keys of the two the walks read.
func walkEveryKind(t *testing.T, roles, notFields map[string]string) (*readKeys, []partWalk) {
	t.Helper()
	var read readKeys
	kinds := declaredKinds(t)
	walks := make([]partWalk, 0, len(kinds))
	for _, k := range kinds {
		p := walkParts(t, k, roles, notFields)
		read.add(p)
		walks = append(walks, p)
	}
	return &read, walks
}

// TestStaleEntries_AreNamedByTheirTable plants, one at a time, a key no
// position reads in each hand-kept table of the type graph and of usage: the
// walk of the real IR leaves it unread, and the check names the table and the
// key. The tables as they stand, beside it, report nothing.
func TestStaleEntries_AreNamedByTheirTable(t *testing.T) {
	t.Parallel()
	t.Run("partRoles", func(t *testing.T) {
		t.Parallel()
		planted := maps.Clone(partRoles)
		planted["Model.NoSuchField"] = "held"
		read, _ := walkEveryKind(t, planted, notPartFields)
		assert.Equal(t, []string{"partRoles lists Model.NoSuchField, but the IR has no such position"},
			staleEntries("partRoles", slices.Collect(maps.Keys(planted)), read.roles))
		assert.Empty(t, staleEntries("partRoles", slices.Collect(maps.Keys(partRoles)), read.roles))
	})
	t.Run("notPartFields", func(t *testing.T) {
		t.Parallel()
		planted := maps.Clone(notPartFields)
		planted["Encoding.NoSuchField"] = "stale"
		read, _ := walkEveryKind(t, partRoles, planted)
		assert.Equal(t, []string{"notPartFields lists Encoding.NoSuchField, but the IR has no such position"},
			staleEntries("notPartFields", slices.Collect(maps.Keys(planted)), read.steps))
		assert.Empty(t, staleEntries("notPartFields", slices.Collect(maps.Keys(notPartFields)), read.steps))
	})
	t.Run("usageNotRootFields", func(t *testing.T) {
		t.Parallel()
		planted := maps.Clone(usageNotRootFields)
		planted["Encoding.NoSuchField"] = "stale"
		_, read := usagePositions(usageFilledDocument(t), planted, usageSkippedSteps)
		assert.Equal(t, []string{"usageNotRootFields lists Encoding.NoSuchField, but the IR has no such position"},
			staleEntries("usageNotRootFields", slices.Collect(maps.Keys(planted)), read))
		assert.Empty(t, staleEntries("usageNotRootFields", slices.Collect(maps.Keys(usageNotRootFields)), read))
	})
	t.Run("usageSkippedSteps", func(t *testing.T) {
		t.Parallel()
		planted := maps.Clone(usageSkippedSteps)
		planted["Service.NoSuchField"] = true
		_, read := usagePositions(usageFilledDocument(t), usageNotRootFields, planted)
		assert.Equal(t, []string{"usageSkippedSteps lists Service.NoSuchField, but the IR has no such position"},
			staleEntries("usageSkippedSteps", slices.Collect(maps.Keys(planted)), read))
		assert.Empty(t, staleEntries("usageSkippedSteps", slices.Collect(maps.Keys(usageSkippedSteps)), read))
	})
}

// TestStaleEntries_ARealEntryIsStillNeeded removes one real entry from each
// table and finds the position it decided undecided: the removal fails a test
// as it did before an unread entry could, so the staleness check does not
// loosen what a table must say.
func TestStaleEntries_ARealEntryIsStillNeeded(t *testing.T) {
	t.Parallel()
	t.Run("partRoles", func(t *testing.T) {
		t.Parallel()
		without := maps.Clone(partRoles)
		delete(without, "List.Elem")
		_, walks := walkEveryKind(t, without, notPartFields)
		assert.Contains(t, undecidedOf(walks), "List.Elem")
	})
	t.Run("notPartFields", func(t *testing.T) {
		t.Parallel()
		without := maps.Clone(notPartFields)
		delete(without, "Encoding.WireType")
		_, walks := walkEveryKind(t, partRoles, without)
		assert.True(t, slices.ContainsFunc(undecidedOf(walks), func(path string) bool {
			return strings.HasSuffix(path, "Encoding.WireType")
		}), "Encoding.WireType is read as a part once notPartFields stops excluding it")
	})
	t.Run("usageNotRootFields", func(t *testing.T) {
		t.Parallel()
		without := maps.Clone(usageNotRootFields)
		delete(without, "Encoding.WireType")
		positions, _ := usagePositions(usageFilledDocument(t), without, usageSkippedSteps)
		roots := usageRootPositions()
		var unclassified []string
		for _, at := range positions {
			if _, classified := roots[at.path]; at.excluded == "" && !classified {
				unclassified = append(unclassified, at.path)
			}
		}
		require.NotEmpty(t, unclassified)
		assert.True(t, slices.ContainsFunc(unclassified, func(path string) bool {
			return strings.HasSuffix(path, "Encoding.WireType")
		}), "Encoding.WireType is a root no table lists once usageNotRootFields stops excluding it")
	})
}

// undecidedOf returns the positions naming a type that no table decided in walks.
func undecidedOf(walks []partWalk) []string {
	var out []string
	for _, w := range walks {
		out = append(out, w.undecided...)
	}
	return out
}
