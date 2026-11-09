// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package irfacts_test

import (
	"crypto/sha256"
	"encoding/hex"
	"maps"
	"os"
	"slices"
	"strings"
	"testing"

	"github.com/google/go-cmp/cmp"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dexpace/morphic/ir"
	"github.com/dexpace/morphic/ir/irfacts"
	"github.com/dexpace/morphic/ir/irtest"
)

// derivations holds, for each irfacts.CorrelationVersion, the hash of the keys
// correlationFixtures derive, as derivationGolden lists them. A change to how
// a key is derived moves the hash, and fails here until the version moves with
// it and a row for the new version is appended. -update rewrites the golden,
// never this table.
var derivations = map[int]string{
	1: "f0fa7169f5c829a4dfdcb15118cad84987f0dd4c102868a4fcd4a3d989a2558c",
}

// derivationGolden lists every key correlationFixtures derive, so a change to
// the derivation reviews as a diff of it.
const derivationGolden = "testdata/correlation.golden.txt"

// TestCorrelationVersion_PinsTheDerivation holds irfacts.CorrelationVersion to
// the keys the derivation gives every entity of correlationFixtures: a key that
// changes at an unmoved version would match a key a consumer stored under the
// same version to another entity (docs/ir/derived-facts.md#correlation-keys).
func TestCorrelationVersion_PinsTheDerivation(t *testing.T) {
	t.Parallel()
	desc := describeDerivation()
	if irtest.Update() {
		require.NoError(t, os.WriteFile(derivationGolden, []byte(desc), 0o644))
	}
	want, err := os.ReadFile(derivationGolden)
	require.NoError(t, err, "go test ./ir/irfacts -run TestCorrelationVersion -update writes it")
	assert.Empty(t, cmp.Diff(string(want), desc), "%s is stale: -update rewrites it, then review its diff", derivationGolden)

	sum := sha256.Sum256([]byte(desc))
	assert.Equal(t, derivations[irfacts.CorrelationVersion], hex.EncodeToString(sum[:]),
		"the derivation changed: move irfacts.CorrelationVersion and give derivations a row for it")
	assert.Len(t, derivations, irfacts.CorrelationVersion, "derivations holds a row for every version from 1")
}

// describeDerivation lists, for each fixture by name, each ID it declares and
// the key derived for it, "-" for none, one per line in sorted order.
func describeDerivation() string {
	var b strings.Builder
	for _, name := range slices.Sorted(maps.Keys(correlationFixtures)) {
		doc := correlationFixtures[name]()
		var declared ir.IDDeclarations
		ir.WalkValues(doc, ir.DocumentPath, &declared)
		ids := make([]string, 0, len(declared.List()))
		for _, d := range declared.List() {
			ids = append(ids, d.ID)
		}
		facts := irfacts.New(doc)
		for _, id := range slices.Compact(slices.Sorted(slices.Values(ids))) {
			key, ok := facts.Correlation(id)
			if !ok {
				key = "-"
			}
			b.WriteString(name + "\t" + id + "\t" + key + "\n")
		}
	}
	return b.String()
}
